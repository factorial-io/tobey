package main

import (
	"context"
	"log/slog"
	"tobey/internal/collector"

	"go.opentelemetry.io/otel/attribute"
)

// CollectorConfig is a serializable configuration that is passed to the
// Collector when enqueuing a URL. It can be used to initialize a new Collector
// later on.
type CollectorConfig struct {
	Run            string
	AllowedDomains []string
	SkipRobots     bool
}

// getEnqueueFn returns the enqueue function, that will enqueue a single URL to
// be crawled. The enqueue function is called whenever a new URL is discovered
// by that Collector, i.e. by looking at all links in a crawled page HTML.
func getEnqueueFn(hconf *WebhookConfig, q WorkQueue, runs MetaStore, progress Progress) collector.EnqueueFn {

	// The returned function takes the run context.
	return func(ctx context.Context, c *collector.Collector, url string, flags uint8) error {
		logger := slog.With("run", c.Run, "url", url)
		tctx, span := tracer.Start(ctx, "enqueue_element")
		defer span.End()

		span.SetAttributes(attribute.String("URL", url))
		// Ensure we never publish a URL twice for a single run. Not only does
		// this help us not put unnecessary load on the queue, it also helps with
		// ensuring there will only (mostly) be one result for a page. There is a slight
		// chance that two processes enter this function with the same run and url,
		// before one of them is finished.
		if ok, _ := c.IsVisitAllowed(url); !ok {
			// slog.Debug("Not enqueuing visit, domain not allowed.", "run", c.Run, "url", url)
			return nil
		}
		if runs.HasSeenURL(tctx, c.Run, url) {
			// Do not need to enqueue an URL that has already been crawled, and its response
			// can be served from cache.
			// slog.Debug("Not enqueuing visit, URL already seen.", "run", c.Run, "url", url)
			return nil
		}

		logger.Debug("Publishing URL...")
		err := q.PublishURL(
			context.WithoutCancel(tctx), // The captured crawl run context.
			// Passing the run ID to identify the crawl run, so when
			// consumed the URL is crawled by the matching
			// Collector.
			c.Run,
			url,
			// This provides all the information necessary to re-construct
			// a Collector by whoever receives this (might be another tobey instance).
			&CollectorConfig{
				Run:            c.Run,
				AllowedDomains: c.AllowedDomains,
			},
			hconf,
			flags,
		)

		if flags&collector.FlagInternal == 0 {
			progress.Update(ProgressUpdateMessagePackage{
				context.WithoutCancel(tctx),
				ProgressUpdateMessage{
					PROGRESS_STAGE_NAME,
					PROGRESS_STATE_QUEUED_FOR_CRAWLING,
					c.Run,
					url,
				},
			})
		}

		if err == nil {
			runs.SawURL(tctx, c.Run, url)
			logger.Debug("URL marked as seen.", "total", runs.CountSeenURLs(ctx, c.Run))
		} else {
			logger.Error("Error enqueuing visit.", "error", err)
		}
		return err
	}
}

// getCollectFn returns the collect function that is called once we have a
// result. Uses the information provided in the original crawl request, i.e. the
// WebhookConfig, that we have received via the queued message.
func getCollectFn(hconf *WebhookConfig, hooks *WebhookDispatcher) collector.CollectFn {

	// The returned function takes the run context.
	return func(ctx context.Context, c *collector.Collector, res *collector.Response, flags uint8) {
		slog.Debug(
			"Collect suceeded.",
			"run", c.Run,
			"url", res.Request.URL,
			"response.body.length", len(res.Body),
			"response.status", res.StatusCode,
		)
		if flags&collector.FlagInternal == 0 {
			if hconf != nil && hconf.Endpoint != "" {
				hooks.Send(ctx, hconf, c.Run, res)
			}
		}
	}
}
