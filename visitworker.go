package main

import (
	"context"
	"log"
	"sync"
	"time"

	"tobey/internal/collector"
)

// CreateVisitWorkersPool initizalizes a worker pool and fills it with a number
// of VisitWorker.
func CreateVisitWorkersPool(ctx context.Context, num int, cm *collector.Manager) *sync.WaitGroup {
	var wg sync.WaitGroup

	log.Printf("Starting %d visit workers...", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id, cm); err != nil {
				log.Printf("Visit worker (%d) exited with error: %s", id, err)
			} else {
				log.Printf("Visit worker (%d) exited cleanly.", id)
			}
			wg.Done()
		}(i)
	}
	return &wg
}

// VisitWorker fetches a resource from a given URL, consumed from the work queue.
func VisitWorker(ctx context.Context, id int, cm *collector.Manager) error {
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

		c, _ := cm.Get(msg.RunID)
		// log.Printf("Visiting URL (%s)...", msg.URL)
		ok, retryAfter, err := c.Visit(msg.URL)
		if ok {
			log.Printf("Worker (%d) scraped URL (%s), took %d ms", id, msg.URL, time.Since(msg.Created).Milliseconds())
			continue
		}
		if err != nil {
			log.Printf("Error visiting URL (%s): %s", msg.URL, err)
			continue
		}
		if err := workQueue.DelayVisit(retryAfter, msg); err != nil {
			log.Printf("Failed to schedule delayed message: %d", msg.RunID)
			// TODO: Nack and requeue msg, so it isn't lost.
			return err
		}
	}
}
