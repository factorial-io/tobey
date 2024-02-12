package main

import (
	"context"
	"log"
	"time"

	"tobey/internal/colly"
)

// VisitWorker fetches a resource from a given URL, consumed from the work queue.
func VisitWorker(ctx context.Context, id int) error {
	for {
		var msg *VisitMessage
		msgs, errs := workQueue.ConsumeVisit()

		select {
		case <-ctx.Done():
			log.Print("Worker context cancelled, stopping worker...")
			return nil
		case err := <-errs:
			log.Printf("Failed to consume from work queue: %s", err)
			return err
		case m := <-msgs:
			msg = m
		}

		// We're creating a Collector lazily once it is needed.
		//
		// This is not only for saving time to construct these, but
		// instances also store information about parsed robots.txt
		// control files and performed requests.
		//
		// Each Collector only does handle a single crawl request. This
		// allows us to pass along request specific WebhookConfig.
		cachedCollectorsLock.Lock()

		var c *colly.Collector
		if v, ok := cachedCollectors[msg.CrawlRequestID]; ok {
			c = v
		} else {
			c = CreateCollector(
				ctx,
				msg.CrawlRequestID,
				redisconn,
				msg.CollectorConfig.AllowedDomains,
			)
			CollectorAttachCallbacks(
				c,
				// enqueue function, that will enqueue a single URL to
				// be crawled. The enqueue function is called whenever
				// a new URL is discovered by that Collector, i.e. by
				// looking at all links in a crawled page HTML.
				func(url string) error {
					ok := IsDomainAllowed(GetHostFromURL(url), msg.CollectorConfig.AllowedDomains)
					if !ok {
						// log.Printf("Skipping enqueuing of crawl of URL (%s), domain not allowed...", msg.URL)
						return nil
					}
					if has, _ := c.HasVisited(url); has {
						return nil
					}
					return workQueue.PublishURL(
						// Passing the crawl request ID, so when
						// consumed the URL is crawled by the matching
						// Collector.
						msg.CrawlRequestID,
						url,
						msg.CollectorConfig,
						msg.WebhookConfig,
					)
				},
				// collect function that is called once we have a
				// result. Uses the information provided in the original
				// crawl request, i.e. the WebhookConfig, that we have
				// received via the queued message.
				func(res *colly.Response) {
					// log.Printf("Worker (%d, %d) scraped URL (%s) and got response", id, c.ID, res.Request.URL)

					if msg.WebhookConfig != nil && msg.WebhookConfig.Endpoint != "" {
						webhookDispatcher.Send(msg.WebhookConfig, res)
					}
				},
			)
			cachedCollectors[msg.CrawlRequestID] = c
		}
		cachedCollectorsLock.Unlock()

		ok := IsDomainAllowed(GetHostFromURL(msg.URL), msg.CollectorConfig.AllowedDomains)
		if !ok {
			// log.Printf("Skipping crawl of URL (%s), domain not allowed...", msg.URL)
			continue
		}
		if has, _ := c.HasVisited(msg.URL); has {
			continue
		}

		ok, retryAfter, err := limiter(msg.URL)
		if err != nil {
			log.Printf("Error while checking rate limiter for  message: %d", msg.CrawlRequestID)
			return err
			// TODO: Rollback, Nack and requeue msg, so it isn't lost.
		} else if !ok { // Hit rate limit, retryAfter is now > 0
			if err := workQueue.DelayVisit(retryAfter, msg); err != nil {
				log.Printf("Failed to schedule delayed message: %d", msg.CrawlRequestID)
				// TODO: Nack and requeue msg, so it isn't lost.
				return err
			}
		}
		log.Printf("Visiting URL (%s)...", msg.URL)

		// Sync crawl the URL.
		if err := c.Visit(msg.URL); err != nil {
			log.Printf("Error visiting URL (%s): %s", msg.URL, err)
			continue
		}
		log.Printf("Worker (%d) scraped URL (%s), took %d ms", id, msg.URL, time.Since(msg.Created).Milliseconds())
	}
}
