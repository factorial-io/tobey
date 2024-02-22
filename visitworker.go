package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"tobey/internal/collector"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// CreateVisitWorkersPool initizalizes a worker pool and fills it with a number
// of VisitWorker.
func CreateVisitWorkersPool(ctx context.Context, num int, cm *collector.Manager, httpClient *http.Client, limiter LimiterAllowFn) *sync.WaitGroup {
	var wg sync.WaitGroup

	slog.Debug("Starting visit workers...", "num", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id, cm, httpClient, limiter); err != nil {
				slog.Error("Visit worker exited with error.", "worker.id", id, "error", err)
			} else {
				slog.Debug("Visit worker exited cleanly.", "worker.id", id)
			}
			wg.Done()
		}(i)
	}
	return &wg
}

// VisitWorker fetches a resource from a given URL, consumed from the work queue.
func VisitWorker(ctx context.Context, id int, cm *collector.Manager, httpClient *http.Client, limiter LimiterAllowFn) error {
	wlogger := slog.With("worker.id", id)

	for {
		var job *VisitJob

		jobs, errs := workQueue.ConsumeVisit(ctx)

		select {
		// This allows to stop a worker gracefully.
		case <-ctx.Done():
			return nil
		case err := <-errs:
			_, span := tracer.Start(ctx, "handle.visit.queue.worker.error")
			wlogger.Error("Failed to consume from work queue.", "error", err)
			span.RecordError(err)
			span.End()

			return err
		case j := <-jobs:
			job = j
		}
		jlogger := wlogger.With("run", job.Run, "url", job.URL)

		jctx, span := tracer.Start(job.Context, "process_visit_job")
		span.SetAttributes(attribute.String("Url", job.URL))

		if _, err := job.Validate(); err != nil {
			jlogger.Error(err.Error())
			continue
		}

		// If this tobey instance is also the instance that received the run request,
		// we already have a Collector locally available. If this instance has retrieved
		// a VisitMessage that was put in the queue by another tobey instance, we don't
		// yet have a collector available via the Manager. Please note that Collectors
		// are not shared by the Manager across tobey instances.
		c, ok := cm.Get(job.Run)
		if !ok {
			c = collector.NewCollector(
				ctx, // Do not use the job's context here.
				httpClient,
				job.CollectorConfig.Run,
				job.CollectorConfig.AllowedDomains,
				getEnqueueFn(ctx, job.WebhookConfig),
				getCollectFn(ctx, job.WebhookConfig),
			)
		}

		if !job.HasReservation {
			retryAfter, err := limiter(job.URL)
			if err != nil {
				slog.Error("Error while checking rate limiter.", "error", err)
				return err
			}
			job.HasReservation = true // Skip limiter next time.

			if retryAfter > 0 {
				jlogger.Debug("Delaying visit...", "delay", retryAfter)

				if err := workQueue.DelayVisit(jctx, retryAfter, job.VisitMessage); err != nil {
					jlogger.Error("Failed to schedule delayed message.")
					span.AddEvent("Failed to schedule delayed message", trace.WithAttributes(
						attribute.String("Url", job.URL),
					))

					// TODO: Nack and requeue msg, so it isn't lost.
					span.End()
					return err
				}

				continue
			}
		}

		if err := c.Visit(job.URL); err != nil {
			jlogger.Error("Error visiting URL.", "error", err)

			progress.Update(ProgressUpdateMessagePackage{
				ctx,
				ProgressUpdateMessage{
					PROGRESS_STAGE_NAME,
					PROGRESS_STATE_Errored,
					job.Run,
					job.URL,
				},
			})
			span.AddEvent("Error visiting URL", trace.WithAttributes(
				attribute.String("Url", job.URL),
			))
			span.End()
			continue
		}

		jlogger.Info("Visited URL.", "took", time.Since(job.Created))
		span.AddEvent("Visited URL.",
			trace.WithAttributes(
				attribute.String("Url", job.URL),
			))
		span.End()

		progress.Update(ProgressUpdateMessagePackage{
			ctx,
			ProgressUpdateMessage{
				PROGRESS_STAGE_NAME,
				PROGRESS_STATE_CRAWLED,
				job.Run,
				job.URL,
			},
		})

	}
}
