package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"tobey/internal/collector"
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
		var msg *VisitMessage
		msgs, errs := workQueue.ConsumeVisit()

		select {
		// This allows to stop a worker gracefully.
		case <-ctx.Done():
			return nil
		case err := <-errs:
			return err
		case m := <-msgs:
			msg = m
		}
		rlogger := wlogger.With("run.id", msg.RunID)

		c, _ := cm.Get(msg.RunID)
		ok, retryAfter, err := c.Visit(msg.URL)
		if ok {
			rlogger.Debug("Scraped URL.", "url", msg.URL, "took", time.Since(msg.Created).Milliseconds())
			continue
		}
		if err != nil {
			rlogger.Error("Error scraping URL.", "url", msg.URL, "error", err)
			continue
		}
		if err := workQueue.DelayVisit(retryAfter, msg); err != nil {
			rlogger.Error("Failed to schedule delayed message.", "run.id", msg.RunID)
			// TODO: Nack and requeue msg, so it isn't lost.
			return err
		}
	}
}
