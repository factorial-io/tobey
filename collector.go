package main

import (
	"context"
	"log/slog"
	"tobey/internal/collector"
)

// CollectorConfig is a serializable configuration that is passed to the
// Collector when enqueuing a URL. It can be used to initialize a new Collector
// later on.
type CollectorConfig struct {
	Run            uint32
	AllowedDomains []string
	SkipRobots     bool
}

// getEnqueueFn returns the enqueue function, that will enqueue a single URL to
// be crawled. The enqueue function is called whenever a new URL is discovered
// by that Collector, i.e. by looking at all links in a crawled page HTML.
func getEnqueueFn(ctx context.Context, webhookConfig *WebhookConfig) collector.EnqueueFn {
	return func(c *collector.Collector, url string) error {
		logger := slog.With("run", c.Run, "url", url)

		// Ensure we never publish a URL twice for a single run. Not only does
		// this help us not put unnecessary load on the queue, it also helps with
		// ensuring there will only (mostly) be one result for a page. There is a slight
		// chance that two processes enter this function with the same run and url,
		// before one of them is finished.
		if ok, _ := c.IsVisitAllowed(url); !ok {
			// slog.Debug("Not enqueuing visit, domain not allowed.", "run", c.Run, "url", url)
			return nil
		}
		if runStore.HasSeen(ctx, c.Run, url) {
			// Do not need to enqueue an URL that has already been crawled, and its response
			// can be served from cache.
			// slog.Debug("Not enqueuing visit, URL already seen.", "run", c.Run, "url", url)
			return nil
		}

		logger.Debug("Publishing URL...")
		err := workQueue.PublishURL(
			ctx, // The captured crawl run context.
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
			webhookConfig,
		)
		if err == nil {
			runStore.MarkSeen(ctx, c.Run, url)
			logger.Debug("URL marked as seen.", "total", runStore.CountSeen(ctx, c.Run))
		} else {
			logger.Error("Error enqueuing visit.", "error", err)
		}
		return err
	}
}

// getCollectFn returns the collect function that is called once we have a
// result. Uses the information provided in the original crawl request, i.e. the
// WebhookConfig, that we have received via the queued message.
func getCollectFn(ctx context.Context, webhookConfig *WebhookConfig) collector.CollectFn {
	return func(c *collector.Collector, res *collector.Response) {
		slog.Debug(
			"Collect suceeded.",
			"run", c.Run,
			"url", res.Request.URL,
			"response.body.length", len(res.Body),
			"response.status", res.StatusCode,
		)
		if webhookConfig != nil && webhookConfig.Endpoint != "" {
			// Use the captured craw run context to send the webhook.
			webhookDispatcher.Send(ctx, webhookConfig, res)
		}
	}
}
