// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"tobey/internal/ctrlq"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	// The maximum number of retries for a job. Jobs are retried if they fail
	// with an error that indicates a temporary issue, i.e. a 503, when a host
	// is down for maintenance.
	MaxJobRetries = 3
)

// CreateVisitWorkersPool initizalizes a worker pool and fills it with a number
// of VisitWorker.
func CreateVisitWorkersPool(
	ctx context.Context,
	num int,
	runs *RunManager,
	q ctrlq.VisitWorkQueue,
	progress ProgressDispatcher,
	rs ResultStore,
) *sync.WaitGroup {
	var wg sync.WaitGroup

	slog.Debug("Visitor: Starting workers...", "num", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id, runs, q, progress, rs); err != nil {
				slog.Error("Visitor: Worker exited with error.", "worker.id", id, "error", err)
			} else {
				slog.Debug("Visitor: Worker exited cleanly.", "worker.id", id)
			}
			wg.Done()
		}(i)
	}
	return &wg
}

// VisitWorker fetches a resource from a given URL, consumed from the work queue.
func VisitWorker(
	ctx context.Context,
	id int,
	runs *RunManager,
	q ctrlq.VisitWorkQueue,
	progress ProgressDispatcher,
	rs ResultStore,
) error {
	wlogger := slog.With("worker.id", id)
	wlogger.Debug("Visitor: Starting...")

	jobs, errs := q.Consume(ctx)
	for {
		var job *ctrlq.VisitJob

		wlogger.Debug("Visitor: Waiting for job...")
		select {
		// This allows to stop a worker gracefully.
		case <-ctx.Done():
			wlogger.Debug("Visitor: Context cancelled, stopping worker.")
			return nil
		case err := <-errs:
			_, span := tracer.Start(ctx, "handle.visit.queue.worker.error")

			wlogger.Error("Visitor: Failed to consume from queue.", "error", err)
			span.RecordError(err)

			span.End()
			return err
		case j := <-jobs:
			job = j
		}
		jlogger := wlogger.With("run", job.Run, "url", job.URL, "job.id", job.ID)

		p := progress.With(job.Run, job.URL)

		jctx, span := tracer.Start(job.Context, "process_visit_job")
		span.SetAttributes(attribute.String("Url", job.URL))
		t := trace.WithAttributes(attribute.String("Url", job.URL))

		jlogger.Debug("Visitor: Received job.")

		if _, err := job.Validate(); err != nil {
			jlogger.Error(err.Error())
			span.End()
			continue
		}

		// If this tobey instance is also the instance that received the run request,
		// we already have a Collector locally available. If this instance has retrieved
		// a VisitMessage that was put in the queue by another tobey instance, we don't
		// yet have a collector available via the Manager. Please note that Collectors
		// are not shared by the Manager across tobey instances.
		r, _ := runs.Get(ctx, job.Run)
		c := r.GetCollector(ctx, q, progress, rs)

		p.Update(jctx, ProgressStateCrawling)

		res, err := c.Visit(jctx, job.URL)

		if UseMetrics {
			PromVisitTxns.Inc()
		}
		if UsePulse {
			atomic.AddInt32(&PulseVisitTxns, 1)
		}

		if res != nil {
			q.TakeRateLimitHeaders(jctx, job.URL, res.Headers)
			q.TakeSample(jctx, job.URL, res.StatusCode, err, res.Took)
		} else {
			q.TakeSample(jctx, job.URL, 0, err, 0)
		}

		if err == nil {
			p.Update(jctx, ProgressStateCrawled)

			jlogger.Info("Visitor: Visited URL.", "took.lifetime", time.Since(job.Created), "took.fetch", res.Took)
			span.AddEvent("Visitor: Visited URL.", t)

			if r.WebhookConfig != nil {
				rs.Save(jctx, r.WebhookConfig, r, res)
			}

			span.End()
			continue
		}

		if res != nil {
			// We have response information, use it to determine the correct error handling in detail.

			switch res.StatusCode {
			case 302: // Redirect
				// When a redirect is encountered, the visit errors out. This is in fact
				// no an actual error, but just a skip.
				p.Update(jctx, ProgressStateCancelled)

				jlogger.Info("Visitor: Skipped URL, got redirected.")
				span.AddEvent("Cancelled visiting URL", t)

				span.End()
				continue
			case 404:
				// FIXME: Probably want to lower the log level to Info for 404s here.
			case 429: // Too Many Requests
				fallthrough
			case 503: // Service Unavailable
				// Additionally want to retrieve the Retry-After header here and wait for that amount of time, if
				// we just have to wait than don't error out but reschedule the job and wait. In order to not do
				// that infinitely, we should have a maximum number of retries.

				// Handling of Retry-After header is optional, so errors here
				// are not critical.
				if v := res.Headers.Get("Retry-After"); v != "" {
					d, _ := time.ParseDuration(v + "s")
					q.Pause(jctx, res.Request.URL.String(), d)
				}

				if job.Retries < MaxJobRetries {
					if err := q.Republish(jctx, job); err != nil {
						jlogger.Warn("Visitor: Republish failed, stopping retrying.", "error", err)
					} else {
						// Leave job in "Crawling" state.
						span.End()
						continue
					}
				} else {
					jlogger.Warn("Visitor: Maximum number of retries reached.")
				}
			default:
				// Noop, fallthrough to generic error handling.
			}
		} else if errors.Is(err, context.DeadlineExceeded) {
			// We react to timeouts as a temporary issue and retry the job, similary
			// to 429 and 503 errors.
			if job.Retries < MaxJobRetries {
				if err := q.Republish(jctx, job); err != nil {
					jlogger.Warn("Visitor: Republish failed, stopping retrying.", "error", err)
				} else {
					// Leave job in "Crawling" state.
					span.End()
					continue
				}
			} else {
				jlogger.Warn("Visitor: Maximum number of retries reached.")
			}
		}

		p.Update(jctx, ProgressStateErrored)

		jlogger.Error("Visitor: Error visiting URL.", "error", err)
		span.RecordError(err)

		span.End()
		continue
	}
}
