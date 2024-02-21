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
func CreateVisitWorkersPool(ctx context.Context, num int, cm *collector.Manager, httpClient *http.Client) *sync.WaitGroup {
	var wg sync.WaitGroup

	slog.Debug("Starting visit workers...", "num", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id, cm, httpClient); err != nil {
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
func VisitWorker(ctx context.Context, id int, cm *collector.Manager, httpClient *http.Client) error {
	wlogger := slog.With("worker.id", id)

	for {
		var msg *VisitMessage

		msgs, errs := workQueue.ConsumeVisit()

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
		case m := <-msgs:
			msg = m
		}
		rlogger := wlogger.With("run", msg.Run, "url", msg.URL)

		// TODO: The active span and trace IDs from the publisher should get here without passing the context on a struct.
		ctx, span := tracer.Start(context.TODO(), "handle.visit.queue.worker")
		span.SetAttributes(attribute.String("Url", msg.URL))

		if msg.Run == 0 {
			rlogger.Error("Message without run arrived.")
			continue
		}
		if msg.URL == "" {
			rlogger.Error("Message without URL arrived.")
			continue
		}

		// If this tobey instance is also the instance that received the run request,
		// we already have a Collector locally available. If this instance has retrieved
		// a VisitMessage that was put in the queue by another tobey instance, we don't
		// yet have a collector available via the Manager. Please note that Collectors
		// are not shared by the Manager across tobey instances.
		c, ok := cm.Get(msg.Run)
		if !ok {
			c = collector.NewCollector(
				ctx,
				httpClient,
				msg.CollectorConfig.Run,
				msg.CollectorConfig.AllowedDomains,
				getEnqueueFn(ctx, msg.WebhookConfig),
				getCollectFn(ctx, msg.WebhookConfig),
			)

		}

		if !msg.HasReservation {
			retryAfter, err := limiter(msg.URL)
			if err != nil {
				slog.Error("Error while checking rate limiter.", "error", err)
				return err
			}
			msg.HasReservation = true // Skip limiter next time.

			if retryAfter > 0 {
				rlogger.Debug("Delaying visit...", "delay", retryAfter)

				if err := workQueue.DelayVisit(retryAfter, msg); err != nil {
					rlogger.Error("Failed to schedule delayed message.")
					span.AddEvent("Failed to schedule delayed message", trace.WithAttributes(
						attribute.String("Url", msg.URL),
					))

					// TODO: Nack and requeue msg, so it isn't lost.
					span.End()
					return err
				}

				continue
			}
		}

		if err := c.Visit(msg.URL); err != nil {
			rlogger.Error("Error visiting URL.", "error", err)

			progress.Update(ProgressUpdateMessagePackage{
				ctx,
				ProgressUpdateMessage{
					PROGRESS_STAGE_NAME,
					PROGRESS_STATE_Errored,
					msg.Run,
					msg.URL,
				},
			})
			span.AddEvent("Error visiting URL", trace.WithAttributes(
				attribute.String("Url", msg.URL),
			))
			span.End()
			continue
		}

		rlogger.Info("Visited URL.", "took", time.Since(msg.Created))
		span.AddEvent("Visited URL.",
			trace.WithAttributes(
				attribute.String("Url", msg.URL),
			))
		span.End()

		progress.Update(ProgressUpdateMessagePackage{
			ctx,
			ProgressUpdateMessage{
				PROGRESS_STAGE_NAME,
				PROGRESS_STATE_CRAWLED,
				msg.Run,
				msg.URL,
			},
		})

	}
}
