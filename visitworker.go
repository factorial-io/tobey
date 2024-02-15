package main

import (
	"context"
	"sync"
	"time"

	"tobey/internal/colly"
	logger "tobey/logger"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	collectors *lru.Cache[string, *colly.Collector] // Cannot grow unbound.
)

// CreateVisitWorkersPool initizalizes a worker pool and fills it with a number
// of VisitWorker.
func CreateVisitWorkersPool(ctx context.Context, num int) sync.WaitGroup {
	log := logger.GetBaseLogger()
	l, _ := lru.New[string, *colly.Collector](128)
	collectors = l

	var wg sync.WaitGroup

	log.Printf("Starting %d visit workers...", num)
	for i := 0; i < num; i++ {
		wg.Add(1)

		go func(id int) {
			if err := VisitWorker(ctx, id); err != nil {
				log.Printf("Visit worker (%d) exited with error: %s", id, err)
			} else {
				log.Printf("Visit worker (%d) exited cleanly.", id)
			}
			wg.Done()
		}(i)
	}

	return wg
}

// VisitWorker fetches a resource from a given URL, consumed from the work queue.
func VisitWorker(ctx context.Context, id int) error {
	log := logger.GetBaseLogger()
	for {
		var package_visit *VisitMessagePackage
		var msg *VisitMessage
		msgs, errs := workQueue.ConsumeVisit()

		select {
		// This allows to stop a worker gracefully.
		case <-ctx.Done():
			log.Print("Worker context cancelled, stopping worker...")
			return nil
		case err := <-errs:
			_, span := tracer.Start(ctx, "handle.visit.queue.worker.error")
			log.Printf("Failed to consume from work queue: %s", err)
			span.RecordError(err)
			span.End()
			return err
		case m := <-msgs:
			package_visit = m
			msg = package_visit.payload
		}

		ctx_new, span := tracer.Start(*package_visit.context, "handle.visit.queue.worker")
		span.SetAttributes(attribute.String("Url", msg.URL))
		// We're creating a Collector lazily once it is needed.
		//
		// This is not only for saving time to construct these, but
		// instances also store information about parsed robots.txt
		// control files and performed requests.
		//
		// Each Collector only does handle a single crawl request. This
		// allows us to pass along request specific WebhookConfig.

		var c *colly.Collector
		if v, ok := collectors.Get(msg.CrawlRequestID); ok {
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
						log.Debug("Domain is not allowed: ", url)
						// log.Printf("Skipping enqueuing of crawl of URL (%s), domain not allowed...", msg.URL)
						return nil
					}
					if has, _ := c.HasVisited(url); has {
						log.Debug("Item ", url, " already visited.")
						return nil
					}

					progress.Update(ProgressUpdateMessagePackage{
						*package_visit.context,
						ProgressUpdateMessage{
							PROGRESS_STAGE_NAME,
							PROGRESS_STATE_QUEUED_FOR_CRAWLING,
							msg.CrawlRequestID,
							url,
						},
					})
					return workQueue.PublishURL(
						*package_visit.context, //todo find the righr
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
						webhookDispatcher.Send(ctx_new, msg.WebhookConfig, res)
					} else {
						//TODO handle message differently
						log.Error("Missing Webhook")
					}
				},
			)
			collectors.Add(msg.CrawlRequestID, c)
		}

		ok := IsDomainAllowed(GetHostFromURL(msg.URL), msg.CollectorConfig.AllowedDomains)
		if !ok {
			log.Debug("Skipping crawl of URL (", msg.URL, "), domain not allowed...")
			span.AddEvent("Domain is not Allowed",
				trace.WithAttributes(
					attribute.String("Url", msg.URL),
				))
			span.End()
			continue
		}
		if has, _ := c.HasVisited(msg.URL); has {
			log.Debug("Skipping crawl of URL (", msg.URL, "), is already visited...")
			span.AddEvent("Url is already visited",
				trace.WithAttributes(
					attribute.String("Url", msg.URL),
				))
			span.End()
			continue
		}

		ok, retryAfter, err := limiter(msg.URL)
		if err != nil {
			log.Printf("Error while checking rate limiter for  message: %v", msg.CrawlRequestID)
			span.AddEvent("Url has reached rate limit",
				trace.WithAttributes(
					attribute.String("Url", msg.URL),
				))
			span.End()
			return err
			// TODO: Rollback, Nack and requeue msg, so it isn't lost.
		} else if !ok { // Hit rate limit, retryAfter is now > 0
			if err := workQueue.DelayVisit(retryAfter, package_visit); err != nil {
				log.Printf("Failed to schedule delayed message: %v", msg.CrawlRequestID)

				span.AddEvent("Failed to schedule delayed message",
					trace.WithAttributes(
						attribute.String("Url", msg.URL),
					))
				// TODO: Nack and requeue msg, so it isn't lost.
				span.End()
				return err
			}
		}
		log.Info("Visiting URL ", msg.URL)

		// Sync crawl the URL.
		if err := c.Visit(msg.URL); err != nil {
			log.Error("Error visiting URL ", msg.URL, ":", err)
			progress.Update(ProgressUpdateMessagePackage{
				*package_visit.context,
				ProgressUpdateMessage{
					PROGRESS_STAGE_NAME,
					PROGRESS_STATE_Errored,
					msg.CrawlRequestID,
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

		progress.Update(ProgressUpdateMessagePackage{
			*package_visit.context,
			ProgressUpdateMessage{
				PROGRESS_STAGE_NAME,
				PROGRESS_STATE_CRAWLED,
				msg.CrawlRequestID,
				msg.URL,
			},
		})
		log.Infof("Worker (%d) scraped URL (%s), took %d ms", id, msg.URL, time.Since(msg.Created).Milliseconds())
		span.AddEvent("Webpage is scraped",
			trace.WithAttributes(
				attribute.String("Url", msg.URL),
			))
		span.End()
	}
}
