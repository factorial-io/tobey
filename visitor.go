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
	"tobey/internal/collector"
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

type Visitor struct {
	id       int
	runs     *RunManager
	queue    ctrlq.VisitWorkQueue
	progress ProgressReporter
	results  ResultReporter
	logger   *slog.Logger
}

// Start launches the worker in a new goroutine.
func (w *Visitor) Start(ctx context.Context, wg *sync.WaitGroup) {
	go func() {
		if err := w.run(ctx); err != nil {
			w.logger.Error("Visitor: Worker exited with error.", "error", err)
		} else {
			w.logger.Debug("Visitor: Worker exited cleanly.")
		}
		wg.Done()
	}()
}

// VisitWorker fetches a resource from a given URL, consumed from the work queue.
func (w *Visitor) run(ctx context.Context) error {
	w.logger.Debug("Visitor: Starting...")

	jobs, errs := w.queue.Consume(ctx)
	for {
		var job *ctrlq.VisitJob

		w.logger.Debug("Visitor: Waiting for job...")
		select {
		// This allows to stop a worker gracefully.
		case <-ctx.Done():
			w.logger.Debug("Visitor: Context cancelled, stopping worker.")
			return nil
		case err := <-errs:
			_, span := tracer.Start(ctx, "handle.visit.queue.worker.error")

			w.logger.Error("Visitor: Failed to consume from queue.", "error", err)
			span.RecordError(err)

			span.End()
			return err
		case j := <-jobs:
			job = j
		}

		if err := w.process(ctx, job); err != nil {
			return err
		}
	}
}

func (w *Visitor) process(ctx context.Context, job *ctrlq.VisitJob) error {
	jlogger := w.logger.With("run", job.Run, "url", job.URL, "job.id", job.ID)
	jctx, span := tracer.Start(job.Context, "process_visit_job")
	defer span.End()

	span.SetAttributes(attribute.String("Url", job.URL))
	t := trace.WithAttributes(attribute.String("Url", job.URL))

	jlogger.Debug("Visitor: Processing job.")

	if _, err := job.Validate(); err != nil {
		jlogger.Error(err.Error())
		return nil
	}

	// If this tobey instance is also the instance that received the run request,
	// we already have a Collector locally available. If this instance has retrieved
	// a VisitMessage that was put in the queue by another tobey instance, we don't
	// yet have a collector available via the Manager. Please note that Collectors
	// are not shared by the Manager across tobey instances.
	r, _ := w.runs.Get(ctx, job.Run)
	c := r.GetCollector(ctx, w.queue, w.progress, w.results)
	p := w.progress.With(r, job.URL)

	p.Update(jctx, ProgressStateCrawling)

	res, err := c.Visit(jctx, job.URL)

	if UseMetrics {
		PromVisitTxns.Inc()
	}
	if UsePulse {
		atomic.AddInt32(&PulseVisitTxns, 1)
	}

	if res != nil {
		w.queue.TakeRateLimitHeaders(jctx, job.URL, res.Headers)
		w.queue.TakeSample(jctx, job.URL, res.StatusCode, err, res.Took)
	} else {
		w.queue.TakeSample(jctx, job.URL, 0, err, 0)
	}

	if err == nil {
		p.Update(jctx, ProgressStateCrawled)
		jlogger.Info("Visitor: Visited URL.", "took.lifetime", time.Since(job.Created), "took.fetch", res.Took)
		span.AddEvent("Visitor: Visited URL.", t)

		switch rr := w.results.(type) {
		case *WebhookResultReporter:
			rr.Accept(jctx, r.WebhookConfig, r, res)
		case *DiskResultReporter:
			rr.Accept(jctx, nil, r, res)
		}
		return nil
	}

	if !w.handleError(jctx, span, job, res, err, p, jlogger) {
		return nil
	}

	p.Update(jctx, ProgressStateErrored)
	jlogger.Error("Visitor: Error visiting URL.", "error", err)
	span.RecordError(err)

	return nil
}

// handleError handles errors that occur during URL visits
func (w *Visitor) handleError(jctx context.Context, span trace.Span, job *ctrlq.VisitJob, res *collector.Response, err error, p *Progress, jlogger *slog.Logger) bool {
	t := trace.WithAttributes(attribute.String("Url", job.URL))

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
			return true
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
				w.queue.Pause(jctx, res.Request.URL.String(), d)
			}

			if job.Retries < MaxJobRetries {
				if err := w.queue.Republish(jctx, job); err != nil {
					jlogger.Warn("Visitor: Republish failed, stopping retrying.", "error", err)
				} else {
					// Leave job in "Crawling" state.
					span.End()
					return true
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
			if err := w.queue.Republish(jctx, job); err != nil {
				jlogger.Warn("Visitor: Republish failed, stopping retrying.", "error", err)
			} else {
				// Leave job in "Crawling" state.
				span.End()
				return true
			}
		} else {
			jlogger.Warn("Visitor: Maximum number of retries reached.")
		}
	}
	return true
}
