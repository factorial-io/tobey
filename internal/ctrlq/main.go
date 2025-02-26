// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ctrlq provides a work queue for accepting and distributing requests
// to visit a web resource to workers. It transparently handles rate limiting.
// By providing run time statistics to the queue it will adjust the rate
// limiters accordingly.
//
// In its most basic form the queue is backed by memory, but it can be backed by
// Redis if you wanted to have several instances coordinate over the queue.
package ctrlq

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

func CreateWorkQueue(redis *redis.Client) VisitWorkQueue {
	if redis != nil {
		slog.Debug("Work Queue: Using distributed work queue")
		// TODO: Add support for redis work queue.
		// return &RedisVisitWorkQueue{conn: redis}
		return NewMemoryVisitWorkQueue()
	} else {
		slog.Debug("Work Queue: Using in-memory work queue")
		return NewMemoryVisitWorkQueue()
	}
}

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
