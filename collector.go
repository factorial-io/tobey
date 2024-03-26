// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"log/slog"
	"tobey/internal/collector"

	"go.opentelemetry.io/otel/attribute"
)

// CollectorConfig is the configuration for a collector.Collector.
type CollectorConfig struct {
}

// getEnqueueFn returns the enqueue function, that will enqueue a single URL to
// be crawled. The enqueue function is called whenever a new URL is discovered
// by that Collector, i.e. by looking at all links in a crawled page HTML.
func getEnqueueFn(run *Run, q WorkQueue, progress Progress) collector.EnqueueFn {

	// The returned function takes the run context.
	return func(ctx context.Context, c *collector.Collector, url string, flags uint8) error {
		logger := slog.With("run", run.ID, "url", url)
		tctx, span := tracer.Start(ctx, "enqueue_element")
		defer span.End()

		span.SetAttributes(attribute.String("URL", url))
		// Ensure we never publish a URL twice for a single run. Not only does
		// this help us not put unnecessary load on the queue, it also helps with
		// ensuring there will only (mostly) be one result for a page. There is a slight
		// chance that two processes enter this function with the same run and url,
		// before one of them is finished.
		if ok, err := c.IsVisitAllowed(url); !ok {
			if err == collector.ErrCheckInternal {
				slog.Warn("Error checking if visit is allowed, not allowing visit.", "error", err)
			}
			// slog.Debug("Not enqueuing visit, domain not allowed.", "run", c.Run, "url", url)
			return nil
		}
		if run.HasSeenURL(tctx, url) {
			// Do not need to enqueue an URL that has already been crawled, and its response
			// can be served from cache.
			// slog.Debug("Not enqueuing visit, URL already seen.", "run", c.Run, "url", url)
			return nil
		}

		logger.Debug("Publishing URL...")
		err := q.PublishURL(
			context.WithoutCancel(tctx), // The captured crawl run context.
			// Passing the run ID to identify the crawl run, so when
			// consumed the URL the run can be reconstructed by the RunManager.
			run.ID,
			url,
			flags,
		)

		if flags&collector.FlagInternal == 0 {
			progress.Update(ProgressUpdateMessagePackage{
				context.WithoutCancel(tctx),
				ProgressUpdateMessage{
					ProgressStage,
					ProgressStateQueuedForCrawling,
					run.ID,
					url,
				},
			})
		}

		if err == nil {
			run.SawURL(tctx, url)
			logger.Debug("URL marked as seen.")
		} else {
			logger.Error("Error enqueuing visit.", "error", err)
		}
		return err
	}
}

// getCollectFn returns the collect function that is called once we have a
// result. Uses the information provided in the original crawl request, i.e. the
// WebhookConfig, that we have received via the queued message.
func getCollectFn(run *Run, hooks *WebhookDispatcher) collector.CollectFn {

	// The returned function takes the run context.
	return func(ctx context.Context, c *collector.Collector, res *collector.Response, flags uint8) {
		slog.Debug(
			"Collect suceeded.",
			"run", run.ID,
			"url", res.Request.URL,
			"response.body.length", len(res.Body),
			"response.status", res.StatusCode,
		)
		if flags&collector.FlagInternal == 0 {
			if run.WebhookConfig != nil && run.WebhookConfig.Endpoint != "" {
				hooks.Send(ctx, run.WebhookConfig, run.ID, res)
			}
		}
	}
}
