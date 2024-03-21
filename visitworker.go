// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"tobey/internal/collector"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// CreateVisitWorkersPool initizalizes a worker pool and fills it with a number
// of VisitWorker.
func CreateVisitWorkersPool(
	ctx context.Context,
	num int,
	runs *RunManager,
	limiter LimiterAllowFn,
	q WorkQueue,
	progress Progress,
	hooks *WebhookDispatcher,
) *sync.WaitGroup {
	var wg sync.WaitGroup

	slog.Debug("Visitor: Starting workers...", "num", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id, runs, limiter, q, progress, hooks); err != nil {
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
	limiter LimiterAllowFn,
	q WorkQueue,
	progress Progress,
	hooks *WebhookDispatcher,
) error {
	wlogger := slog.With("worker.id", id)

	for {
		var job *VisitJob

		wlogger.Debug("Visitor: Waiting for job...")
		jobs, errs := q.ConsumeVisit(ctx)

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
		jlogger := wlogger.With("run", job.Run, "url", job.URL, "job.id", job.ID, "job.flags", job.Flags)
		jlogger.Debug("Visitor: Received job.")

		jctx, span := tracer.Start(job.Context, "process_visit_job")
		span.SetAttributes(attribute.String("Url", job.URL))

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
		c := r.GetCollector(ctx, q, progress, hooks)

		if !job.HasReservation {
			jlogger.Debug("Visitor: Job has no reservation.")

			nowReserved, retryAfter, err := limiter(job.URL)
			if err != nil {
				slog.Error("Visitor: Error while checking rate limiter.", "error", err)

				span.End()
				return err
			}
			// Some limiters will always perform a reservation, others will ask
			// you to retry and reserve again later.
			job.HasReservation = nowReserved // Skip limiter next time.

			if retryAfter > 0 {
				jlogger.Debug("Visitor: Delaying visit...", "delay", retryAfter)

				if err := q.DelayVisit(jctx, retryAfter, job.VisitMessage); err != nil {
					jlogger.Error("Visitor: Failed to schedule delayed message.")

					span.AddEvent("Failed to schedule delayed message", trace.WithAttributes(
						attribute.String("Url", job.URL),
					))

					span.End()
					continue
				}
				span.End()
				continue
			}
		}
		if job.Flags&collector.FlagInternal == 0 {
			progress.Update(ProgressUpdateMessagePackage{
				jctx,
				ProgressUpdateMessage{
					ProgressStage,
					ProgressStateCrawling,
					job.Run,
					job.URL,
				},
			})
		}

		if err := c.Visit(jctx, job.URL); err != nil {
			jlogger.Error("Visitor: Error visiting URL.", "error", err)

			if job.Flags&collector.FlagInternal == 0 {
				progress.Update(ProgressUpdateMessagePackage{
					jctx,
					ProgressUpdateMessage{
						ProgressStage,
						ProgressStateErrored,
						job.Run,
						job.URL,
					},
				})
			}
			span.AddEvent("Error visiting URL", trace.WithAttributes(
				attribute.String("Url", job.URL),
			))
			span.End()
			continue
		}
		jlogger.Info("Visitor: Visited URL.", "took", time.Since(job.Created))

		if job.Flags&collector.FlagInternal == 0 {
			// Until Laravel is properly connected, we setit to successful (PROGRESS_STATE_CRAWLED)
			progress.Update(ProgressUpdateMessagePackage{
				jctx,
				ProgressUpdateMessage{
					ProgressStage,
					ProgressStateSucceeded,
					job.Run,
					job.URL,
				},
			})
		}
		span.AddEvent("Visitor: Visited URL.",
			trace.WithAttributes(
				attribute.String("Url", job.URL),
			))
		span.End()

	}
}
