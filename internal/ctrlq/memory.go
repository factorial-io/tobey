// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// The maximum number of messages that can exists in the in-memory work queue.
const MemoryWorkQueueBufferSize = 1_000_000

func NewMemoryVisitWorkQueue() *MemoryVisitWorkQueue {
	return &MemoryVisitWorkQueue{
		dqueue:       make(chan *VisitMessage, MemoryWorkQueueBufferSize),
		shouldRecalc: make(chan bool),
	}
}

type MemoryVisitWorkQueue struct {
	// This where consumers read from.
	dqueue chan *VisitMessage

	// This is where message get published too. Key ist the hostname including
	// the port. It's okay to mix enqueue visits with and without authentication
	// for the same host.
	hqueues sync.Map // map[uint32]*ControlledQueue

	// shouldRecalc is checked by the promoter to see if it should recalculate.
	// It is an unbuffered channel. If sending is blocked, this means there is
	// a pending notification. As one notification is enough, to trigger the
	// recalculation, a failed send can be ignored.
	shouldRecalc chan bool
}

func (wq *MemoryVisitWorkQueue) Open(ctx context.Context) error {

	go func() {
		promoteLoop(ctx, wq.dqueue, &wq.hqueues, wq.shouldRecalc)
	}()
	return nil
}

func (wq *MemoryVisitWorkQueue) lazyHostQueue(u string) *ControlledQueue {
	p, _ := url.Parse(u)
	key := guessHost(u)

	if _, ok := wq.hqueues.Load(key); !ok {
		wq.hqueues.Store(key, &ControlledQueue{
			ID:         key,
			Name:       strings.TrimLeft(p.Hostname(), "www."),
			IsAdaptive: true,
			Queue:      make(chan *VisitMessage, MemoryWorkQueueBufferSize),
			Limiter:    NewMemoryLimiter(),
		})
	}

	v, _ := wq.hqueues.Load(key)
	return v.(*ControlledQueue)
}

func (wq *MemoryVisitWorkQueue) Publish(ctx context.Context, run string, url string) error {
	defer wq.triggerRecalc() // Notify promoter that a new message is available.

	hq := wq.lazyHostQueue(url)

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)

	msg := &VisitMessage{
		ID:      uuid.New().ID(),
		Run:     run,
		URL:     url,
		Created: time.Now(),
		Carrier: carrier,
	}

	select {
	case hq.Queue <- msg:
		slog.Debug("Work Queue: Message successfully published.", "msg.id", msg.ID)
	default:
		slog.Warn("Work Queue: full, not publishing message!", "msg", msg)
	}
	return nil
}

func (wq *MemoryVisitWorkQueue) Republish(ctx context.Context, job *VisitJob) error {
	defer wq.triggerRecalc()
	hq := wq.lazyHostQueue(job.URL)

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(job.Context, carrier)

	msg := &VisitMessage{
		ID:      job.ID,
		Run:     job.Run,
		URL:     job.URL,
		Created: job.Created,
		Retries: job.Retries + 1,
		Carrier: carrier,
	}

	select {
	case hq.Queue <- msg:
		slog.Debug("Work Queue: Message successfully rescheduled.", "msg.id", msg.ID)
	default:
		slog.Warn("Work Queue: full, not rescheduling message!", "msg", msg)
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
			case msg := <-wq.dqueue:
				// slog.Debug("Work Queue: Received message, forwarding to results channel.", "msg.id", p.Message.ID)

				if msg == nil {
					slog.Debug("Work Queue: Detected closed channel, closing channels.")

					close(reschan)
					close(errchan)
					return
				}

				// Initializes the context for the job. Than extract the tracing
				// information from the carrier into the job's context.
				jctx := context.Background()
				jctx = otel.GetTextMapPropagator().Extract(jctx, msg.Carrier)

				reschan <- &VisitJob{
					VisitMessage: msg,
					Context:      jctx,
				}
				// slog.Debug("Work Queue: Forwarded message to results channel.", "msg.id", p.Message.ID)
			}
		}
	}()

	return reschan, errchan
}

func (wq *MemoryVisitWorkQueue) Pause(ctx context.Context, url string, d time.Duration) error {
	wq.lazyHostQueue(url).Pause(d)
	return nil
}

// shouldRecalc is checked by the promoter to see if it should recalculate.
// It is an unbuffered channel. If sending is blocked, this means there is
// a pending notification. As one notification is enough, to trigger the
// recalculation, a failed send can be ignored.
func (wq *MemoryVisitWorkQueue) triggerRecalc() {
	select {
	case wq.shouldRecalc <- true:
	default:
		// A notification is already pending, no need to send another.
	}
}

func (wq *MemoryVisitWorkQueue) Close() error {
	close(wq.dqueue)

	wq.hqueues.Range(func(k any, v any) bool {
		close(v.(*ControlledQueue).Queue)
		return true
	})
	return nil
}

func (wq *MemoryVisitWorkQueue) TakeRateLimitHeaders(ctx context.Context, url string, hdr *http.Header) {
	hq := wq.lazyHostQueue(url)

	if !hq.IsAdaptive {
		return
	}
	changed, desired := newRateLimitByHeaders(hq.Limiter.GetLimit(), hdr)
	if !changed {
		return
	}

	hq.Limiter.SetLimit(desired)
	slog.Debug("Work Queue: Rate limit fine tuned.", "host", hq.Name, "desired", desired)
}

// TakeSample implements an algorithm to adjust the rate limiter, to ensure maximum througput within the bounds
// set by the rate limiter, while not overwhelming the target. The algorithm will adjust the rate limiter
// but not go below MinRequestsPerSecondPerHost and not above MaxRequestsPerSecondPerHost.
func (wq *MemoryVisitWorkQueue) TakeSample(ctx context.Context, url string, statusCode int, err error, d time.Duration) {
	hq := wq.lazyHostQueue(url)

	if !hq.IsAdaptive {
		return
	}
	changed, desired := newRateLimitByLatency(hq.Limiter.GetLimit(), statusCode, err, d)
	if !changed {
		return
	}

	hq.Limiter.SetLimit(desired)
	slog.Debug("Work Queue: Rate limit fine tuned.", "host", hq.Name, "desired", desired)
}
