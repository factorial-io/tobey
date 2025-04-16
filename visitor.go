// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
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

type PauseFn func(url string, d time.Duration) error
type RepublishFn func(job *ctrlq.VisitJob) error

// Visitor is the main "work horse" of the crawler. It consumes jobs from the
// work queue and processes them with the help of the Collector.
type Visitor struct {
	id    int
	runs  *RunManager
	queue ctrlq.VisitWorkQueue

	result   ResultReporter
	progress ProgressReporter

	logger *slog.Logger
}

// Code is used when handling failed visits.
type Code uint32

const (
	CodeUnknown Code = iota
	CodeUnhandled
	CodePermanent
	CodeTemporary
	CodeIgnore
)

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

		if _, err := job.Validate(); err != nil {
			w.logger.Error(err.Error())
			return nil // Skip this job, but continue
		}
		if err := w.process(ctx, job); err != nil {
			return err
		}
	}
}

func (w *Visitor) process(ctx context.Context, job *ctrlq.VisitJob) error {

	// If this tobey instance is also the instance that received the run request,
	// we already have a Collector locally available. If this instance has retrieved
	// a VisitMessage that was put in the queue by another tobey instance, we don't
	// yet have a collector available via the Manager. Please note that Collectors
	// are not shared by the Manager across tobey instances.
	r, _ := w.runs.Get(ctx, job.Run)
	c := r.GetCollector(ctx, w.queue, w.result, w.progress)
	p := w.progress.With(r, job.URL)

	jlogger := w.logger.With("run", r.LogValue(), "job", job.LogValue())
	jlogger.Debug("Visitor: Processing job...")

	jctx, span := tracer.Start(job.Context, "process_visit_job")
	span.SetAttributes(attribute.String("Url", job.URL))
	t := trace.WithAttributes(attribute.String("Url", job.URL))
	defer span.End()

	p.Update(jctx, ProgressStateCrawling)

	// This will also call the ResultReporter.Accept method, via the collector.CollectFn.
	res, err := c.Visit(jctx, job.URL)

	// Take samples and metrics for the visit, that should always be done,
	// regardless of whether the visit was successful or not.
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
	p.Update(jctx, ProgressStateCrawled)

	// The happy path.
	if err == nil {
		p.Update(jctx, ProgressStateSucceeded)
		jlogger.Info("Visitor: Successfully visited URL.",
			"took.lifetime", time.Since(job.Created).Round(time.Millisecond).Milliseconds(),
			"took.fetch", res.Took.Round(time.Millisecond).Milliseconds())
		span.AddEvent("Visitor: Successfully visited URL.", t)
		return nil
	}

	// The sad path, where the previous error is now interpreted as a fail.
	span.AddEvent("Visitor: Failed visiting URL.", t)
	jlogger.Debug("Visitor: Error encountered visiting URL, handling...", "error", err)

	code, herr := handleFailedVisit(
		func(url string, d time.Duration) error {
			return w.queue.Pause(jctx, url, d)
		},
		func(job *ctrlq.VisitJob) error {
			return w.queue.Republish(jctx, job)
		},
		job,
		res,
		err,
	)

	codelogger := jlogger.With("code", code, "fail", err, "error", herr)

	switch code {
	case CodeIgnore:
		p.Update(jctx, ProgressStateCancelled)
		span.End()
		return nil
	case CodeTemporary:
		// Leave job in "Crawling" state, and process it later. It should've
		// been republished already.
		span.End()
		return nil
	case CodePermanent:
		codelogger.Error("Visitor: Handling the failed visit error'ed.")
		p.Update(jctx, ProgressStateErrored)
		span.End()
		return nil
	case CodeUnknown:
		codelogger.Error("Visitor: Handling the failed visit error'ed.")
		p.Update(jctx, ProgressStateErrored)
		span.End()
		return nil
	default: // covers CodeUnhandled
		codelogger.Error("Visitor: Handling the failed visit error'ed.")
		p.Update(jctx, ProgressStateErrored)
		span.RecordError(herr)
		span.End()
		return herr // Exit the worker.
	}
}

// handleFailedVisit handles failed URL visits. This handler has two return values.
// One returns the status code indicating *how* and if the fail was handled. The other return value
// is an error, if one occurred during handling. An error means the fail was not handled
// properly.
func handleFailedVisit(pause PauseFn, republish RepublishFn, job *ctrlq.VisitJob, res *collector.Response, err error) (Code, error) {
	attemptRepublish := func() (Code, error) {
		if job.Retries >= MaxJobRetries {
			return CodePermanent, fmt.Errorf("maximum retries reached")
		}
		if err := republish(job); err != nil {
			return CodeUnhandled, fmt.Errorf("failed to republish job: %w", err)
		}
		return CodeTemporary, nil
	}

	if res != nil {
		// We have response information, use it to determine the correct error handling in detail.
		switch res.StatusCode {
		case 301, 302: // Redirect
			// When a redirect is encountered, a new URL is enqueued already by the collector. We
			// don't want to retry this job, so we return CodeIgnore.
			return CodeIgnore, nil
		case 404:
			return CodeIgnore, nil
		case 500, 504: // Internal Server Error
			return attemptRepublish()
		case 429, 503: // Too Many Requests, Service Unavailable
			// Additionally want to retrieve the Retry-After header here and wait for that amount of time, if
			// we just have to wait than don't error out but reschedule the job and wait. In order to not do
			// that infinitely, we should have a maximum number of retries. Handling of Retry-After header is optional, so errors here
			// are not critical.
			if v := res.Headers.Get("Retry-After"); v != "" {
				d, _ := time.ParseDuration(v + "s")
				pause(res.Request.URL.String(), d)
			}
			return attemptRepublish()
		default:
			return CodeUnknown, nil
		}
	} else if errors.Is(err, context.DeadlineExceeded) {
		// We react to timeouts as a temporary issue and retry the job, similary
		// to 429 and 503 errors.
		return attemptRepublish()
	}
	return CodeUnhandled, fmt.Errorf("unhandled failed visit")
}
