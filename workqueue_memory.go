// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	xrate "golang.org/x/time/rate"
)

// The maximum number of messages that can exists in the in-memory work queue.
const MemoryWorkQueueBufferSize = 1_000_000

// visitMemoryPackage is used by the in-memory implementation of the WorkQueue.
// Implementations that have a built-in mechanism to transport headers do not
// need to use this.
type visitMemoryPackage struct {
	Carrier propagation.MapCarrier
	Message *VisitMessage
}

type hostMemoryVisitWorkQueue struct {
	ID   uint32
	Name string // For debugging purposes only.

	Queue chan *visitMemoryPackage

	Limiter        *xrate.Limiter
	HasReservation bool
	IsAdaptive     bool
	PausedUntil    time.Time
}

func NewMemoryVisitWorkQueue() *MemoryVisitWorkQueue {
	return &MemoryVisitWorkQueue{
		dqueue:       make(chan *visitMemoryPackage, MemoryWorkQueueBufferSize),
		hqueues:      make(map[uint32]*hostMemoryVisitWorkQueue),
		shoudlRecalc: make(chan bool),
	}
}

type MemoryVisitWorkQueue struct {
	mu sync.RWMutex

	// This where consumers read from.
	dqueue chan *visitMemoryPackage

	// This is where message get published too. Key ist the hostname including
	// the port. It's okay to mix enqueue visits with and without authentication
	// for the same host.
	hqueues map[uint32]*hostMemoryVisitWorkQueue

	// shoudlRecalc is checked by the promoter to see if it should recalculate.
	// It is an unbuffered channel. If sending is blocked, this means there is
	// a pending notification. As one notification is enough, to trigger the
	// recalculation, a failed send can be ignored.
	shoudlRecalc chan bool
}

func (wq *MemoryVisitWorkQueue) Open(ctx context.Context) error {
	wq.startPromoter(ctx)
	return nil
}

func (wq *MemoryVisitWorkQueue) lazyHostQueue(u string) *hostMemoryVisitWorkQueue {
	p, _ := url.Parse(u)
	key := guessHost(u)

	if _, ok := wq.hqueues[key]; !ok {
		wq.mu.Lock()
		wq.hqueues[key] = &hostMemoryVisitWorkQueue{
			ID:             key,
			Name:           strings.TrimLeft(p.Hostname(), "www."),
			PausedUntil:    time.Time{},
			HasReservation: false,
			IsAdaptive:     true,
			Queue:          make(chan *visitMemoryPackage, MemoryWorkQueueBufferSize),
			Limiter:        xrate.NewLimiter(xrate.Limit(MinHostRPS), 1),
		}
		wq.mu.Unlock()
	}

	wq.mu.RLock()
	defer wq.mu.RUnlock()
	return wq.hqueues[key]
}

func (wq *MemoryVisitWorkQueue) Publish(ctx context.Context, run string, url string) error {
	defer wq.shouldRecalc() // Notify promoter that a new message is available.

	hq := wq.lazyHostQueue(url)

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)

	pkg := &visitMemoryPackage{
		Carrier: carrier,
		Message: &VisitMessage{
			ID:      uuid.New().ID(),
			Run:     run,
			URL:     url,
			Created: time.Now(),
		},
	}

	select {
	case hq.Queue <- pkg:
		slog.Debug("Work Queue: Message successfully published.", "msg.id", pkg.Message.ID)
	default:
		slog.Warn("Work Queue: full, not publishing message!", "msg", pkg.Message)
	}
	return nil
}

func (wq *MemoryVisitWorkQueue) Republish(ctx context.Context, job *VisitJob) error {
	defer wq.shouldRecalc()

	hq := wq.lazyHostQueue(job.URL)

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(job.Context, carrier)

	pkg := &visitMemoryPackage{
		Carrier: carrier,
		Message: &VisitMessage{
			ID:      job.ID,
			Run:     job.Run,
			URL:     job.URL,
			Created: job.Created,
			Retries: job.Retries + 1,
		},
	}

	select {
	case hq.Queue <- pkg:
		slog.Debug("Work Queue: Message successfully rescheduled.", "msg.id", pkg.Message.ID)
	default:
		slog.Warn("Work Queue: full, not rescheduling message!", "msg", pkg.Message)
	}
	return nil
}

// Consume returns the next available VisitJob from the default queue.
func (wq *MemoryVisitWorkQueue) Consume(ctx context.Context) (<-chan *VisitJob, <-chan error) {
	// We are unwrapping the VisitJob from the visitMemoryPackage.
	reschan := make(chan *VisitJob)
	errchan := make(chan error)

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Debug("Work Queue: Consume context cancelled, closing channels.")

				close(reschan)
				close(errchan)
				return
			case p := <-wq.dqueue:
				// slog.Debug("Work Queue: Received message, forwarding to results channel.", "msg.id", p.Message.ID)

				// Initializes the context for the job. Than extract the tracing
				// information from the carrier into the job's context.
				jctx := context.Background()
				jctx = otel.GetTextMapPropagator().Extract(jctx, p.Carrier)

				reschan <- &VisitJob{
					VisitMessage: p.Message,
					Context:      jctx,
				}
				// slog.Debug("Work Queue: Forwarded message to results channel.", "msg.id", p.Message.ID)
			}
		}
	}()

	return reschan, errchan
}

func (wq *MemoryVisitWorkQueue) Pause(ctx context.Context, url string, d time.Duration) error {
	t := time.Now().Add(d)
	hq := wq.lazyHostQueue(url)

	if hq.PausedUntil.IsZero() || !hq.PausedUntil.After(t) { // Pause can only increase.
		hq.PausedUntil = t
	}
	return nil
}

// shoudlRecalc is checked by the promoter to see if it should recalculate.
// It is an unbuffered channel. If sending is blocked, this means there is
// a pending notification. As one notification is enough, to trigger the
// recalculation, a failed send can be ignored.
func (wq *MemoryVisitWorkQueue) shouldRecalc() {
	select {
	case wq.shoudlRecalc <- true:
	default:
		// A notification is already pending, no need to send another.
	}
}

func (wq *MemoryVisitWorkQueue) Close() error {
	close(wq.dqueue)

	for _, hq := range wq.hqueues {
		close(hq.Queue)
	}
	return nil
}
