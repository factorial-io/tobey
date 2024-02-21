package main

import (
	"context"
	"log/slog"
	"tobey/internal/collector"
)

// getEnqueueFn returns the enqueue function, that will enqueue a single URL to
// be crawled. The enqueue function is called whenever a new URL is discovered
// by that Collector, i.e. by looking at all links in a crawled page HTML.
func getEnqueueFn(ctx context.Context, webhookConfig *WebhookConfig) collector.EnqueueFn {
	return func(c *collector.Collector, url string) error {
		// Ensure we never publish a URL twice for a single run. Not only does
		// this help us not put unnecessary load on the queue, it also helps with
		// ensuring there will only (mostly) be one result for a page. There is a slight
		// chance that two processes enter this function with the same run and url,
		// before one of them is finished.
		if !c.IsDomainAllowed(GetHostFromURL(url)) {
			// slog.Debug("Not enqueuing visit, domain not allowed.", "run", c.Run, "url", url)
			return nil
		}
		if runStore.HasSeen(ctx, c.Run, url) {
			// Do not need to enqueue an URL that has already been crawled, and its response
			// can be served from cache.
			// slog.Debug("Not enqueuing visit, URL already seen.", "run", c.Run, "url", url)
			return nil
		}

		slog.Debug("Publishing URL...", "run", c.Run, "url", url)
		err := workQueue.PublishURL(
			// Passing the crawl request ID, so when
			// consumed the URL is crawled by the matching
			// Collector.
			c.Run, // The collector's ID is the run ID.
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
			runStore.Seen(ctx, c.Run, url)
		} else {
			slog.Error("Error enqueuing visit.", "run", c.Run, "url", url, "error", err)
		}
		return err
	}
}

// getCollectFn returns the collect function that is called once we have a
// result. Uses the information provided in the original crawl request, i.e. the
// WebhookConfig, that we have received via the queued message.
func getCollectFn(ctx context.Context, webhookConfig *WebhookConfig) collector.CollectFn {
	return func(c *collector.Collector, res *collector.Response) {
		slog.Info(
			"Collect suceeded.",
			"run", c.Run,
			"url", res.Request.URL,
			"response.body.length", len(res.Body),
			"response.status", res.StatusCode,
		)
		if webhookConfig != nil && webhookConfig.Endpoint != "" {
			webhookDispatcher.Send(ctx, webhookConfig, res)
		}
	}
}
