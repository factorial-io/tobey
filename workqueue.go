// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// MinHostRPS specifies the minimum number of requests per
	// second that are executed against a single host.
	MinHostRPS float64 = 1

	// MaxHostRPS specifies the maximum number of requests per
	// second that are exectuted against a single host.
	MaxHostRPS float64 = 50
)

// VisitWorkQueue appears to produceres and consumers as a single queue. Each
// message in the work queue represents a job for a request to visit a single
// URL and process the response.
//
// While producers publish a new VisitMessage immediately to the work queue,
// consumers can only consume jobs at a certain rate. This rate is determined by
// a per-host rate limiter. These rate limiters can be updated dynamically.
type VisitWorkQueue interface {
	// Open opens the work queue for use. It must be called before any other method.
	Open(context.Context) error

	// Publish creates a new VisitMessage for the given URL and enqueues the job to
	// be retrieved later via Consume. The run ID must be specified in order to
	// allow the consumer to find the right Collector to visit the URL.
	Publish(ctx context.Context, run string, url string) error

	// Republish is used to reschedule a job for later processing. This is useful
	// if the job could not be processed due to a temporary error. The function
	// should keep a count on how often a job is rescheduled.
	Republish(ctx context.Context, job *VisitJob) error

	// Consume is used by workers to retrieve a new VisitJob to process, reading from the
	// returned channel will block until a job becomes available. Jobs are automatically acked
	// when retrieved from the channel.
	Consume(ctx context.Context) (<-chan *VisitJob, <-chan error)

	// Pause pauses the consumption of jobs for a given host. This is useful if
	// we see the host stopping to be available, for example when it is down
	// for maintenance.
	Pause(ctx context.Context, url string, d time.Duration) error

	// TakeSample allows to inform the rate limiter how long it took to process a job and adjust
	// accordingly. Seeing an increase in Latency might indicate we are overwhelming the
	// target.
	TakeSample(ctx context.Context, url string, statusCode int, err error, d time.Duration)

	// UseRateLimitHeaders allows the implementation to use the information provided
	// through rate limit headers to inform the rate limiter.
	// See https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api?apiVersion=2022-11-28#checking-the-status-of-your-rate-limit
	TakeRateLimitHeaders(ctx context.Context, url string, hdr *http.Header)

	// Close allows the implementation to release opened resources. After Close
	// the work queue must not be used anymore.
	Close() error
}

type VisitMessage struct {
	ID  uint32
	Run string

	URL string

	Created time.Time

	// The number of times this job has been retried to be enqueued.
	Retries uint32

	// The carrier is used to pass tracing information from the job publisher to
	// the job consumer. It is used to pass the TraceID and SpanID.
	Carrier propagation.MapCarrier
}

// VisitJob is similar to a http.Request, it exists only for a certain time. It
// carries its own Context. And although this violates the strict variant of the
// "do not store context on struct" it doe not violate the relaxed "do not store
// a context" rule, as a Job is transitive.
//
// We initially saw the requirement to pass a context here as we wanted to carry
// over TraceID and SpanID from the job publisher.
type VisitJob struct {
	*VisitMessage
	Context context.Context
}

// Validate ensures mandatory fields are non-zero.
func (j *VisitJob) Validate() (bool, error) {
	if j.Run == "" {
		return false, errors.New("job without run")
	}
	if j.URL == "" {
		return false, errors.New("job without URL")
	}
	return true, nil
}

func CreateWorkQueue(redis *redis.Client) VisitWorkQueue {
	if redis != nil {
		slog.Debug("Using distributed work queue...")
		// TODO: Add support for redis work queue.
		// return &RedisVisitWorkQueue{conn: redis}
		return NewMemoryVisitWorkQueue()
	} else {
		slog.Debug("Using in-memory work queue...")
		return NewMemoryVisitWorkQueue()
	}
}

// ControlledQueue is a wrapped queue that is flow controllable, by
// pausing and resuming. It also has a rate limiter attached to it.
type ControlledQueue struct {
	ID   uint32
	Name string // For debugging purposes only.

	Queue chan *VisitMessage

	Limiter Limiter

	// Holds an Unix timestamp, instead of a time.Time so
	// that we can operate on this without locks.
	pausedUntil atomic.Int64

	IsAdaptive bool
}

func (cq *ControlledQueue) String() string {
	_, until := cq.IsPaused()
	return fmt.Sprintf("ControlledQueue(%d, %s, %d, %s, %s)", cq.ID, cq.Name, len(cq.Queue), until, cq.Limiter.HoldsReservation())
}

func (cq *ControlledQueue) IsPaused() (bool, time.Time) {
	now := time.Now().Unix()
	until := cq.pausedUntil.Load()

	if until == 0 {
		return false, time.Time{}
	}
	return now < until, time.Unix(until, 0)
}

func (cq *ControlledQueue) Pause(d time.Duration) time.Time {
	v := time.Now().Add(d).Unix()
	o := cq.pausedUntil.Load()

	if o == 0 || v > o { // Pause can only increase.
		cq.pausedUntil.Store(v)
		return time.Unix(v, 0)
	}
	return time.Unix(o, 0)
}

func (cq *ControlledQueue) Unpause() {
	cq.pausedUntil.Store(0)
}

// guessHost heuristically identifies a host for the given URL. The function
// doesn't return the host name directly, as it might not exist, but an ID.
//
// It does by by ignoring a www. prefix, leading to www.example.org and
// example.org being considered the same host. It also ignores the port number,
// so example.org:8080 and example.org:9090 are considered the same host as
// well.
//
// Why FNV? https://softwareengineering.stackexchange.com/questions/49550
func guessHost(u string) uint32 {
	p, err := url.Parse(u)
	if err != nil {
		return 0
	}
	h := fnv.New32a()

	h.Write([]byte(strings.TrimLeft(p.Hostname(), "www.")))
	return h.Sum32()
}
