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
func CreateVisitWorkersPool(ctx context.Context, num int, cm *collector.Manager) *sync.WaitGroup {
	var wg sync.WaitGroup

	slog.Debug("Starting visit workers...", "num", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id, cm); err != nil {
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
func VisitWorker(ctx context.Context, id int, cm *collector.Manager) error {
	wlogger := slog.With("worker.id", id)

	for {

		var package_visit *VisitMessagePackage
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
		rlogger := wlogger.With("run.id", msg.RunID, "url", msg.URL)

		ctx_new, span := tracer.Start(*package_visit.context, "handle.visit.queue.worker")
		span.SetAttributes(attribute.String("Url", msg.URL))

		if msg.RunID == 0 {
			rlogger.Error("Message without run ID arrived.")
			continue
		}
		if msg.URL == "" {
			rlogger.Error("Message without URL arrived.")
			continue
		}

		c, _ := cm.Get(msg.RunID)
		ok, retryAfter, err := c.Visit(msg.URL)
		if ok {
			rlogger.Debug("Scraped URL.", "url", msg.URL, "took", time.Since(msg.Created).Milliseconds())
			continue
		}
		if err != nil {
			rlogger.Error("Error scraping URL.", "url", msg.URL, "error", err)
			progress.Update(ProgressUpdateMessagePackage{
				ctx_new,
				ProgressUpdateMessage{
					PROGRESS_STAGE_NAME,
					PROGRESS_STATE_Errored,
					msg.RunID,
					msg.URL,
				},
			})
			span.AddEvent("Error Visiting Url",
				trace.WithAttributes(
					attribute.String("Url", msg.URL),
				))
			span.End()
			continue
		}
		if err := workQueue.DelayVisit(retryAfter, msg); err != nil {
			rlogger.Error("Failed to schedule delayed message.", "run.id", msg.RunID)
			span.AddEvent("Failed to schedule delayed message", trace.WithAttributes(
				attribute.String("Url", msg.URL),
			))
			// TODO: Nack and requeue msg, so it isn't lost.
			span.End()
			return err
		}

		rlogger.Info("Scraped URL.", "took", time.Since(msg.Created).Milliseconds())
		span.AddEvent("URL has been scraped",
			trace.WithAttributes(
				attribute.String("Url", msg.URL),
			))
		span.End()

		progress.Update(ProgressUpdateMessagePackage{
			ctx_new,
			ProgressUpdateMessage{
				PROGRESS_STAGE_NAME,
				PROGRESS_STATE_CRAWLED,
				msg.RunID,
				msg.URL,
			},
		})

	}
}
