// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"log/slog"
	"net/http"
	"tobey/internal/collector"
	"tobey/internal/ctrlq"
	"tobey/internal/progress"
	"tobey/internal/result"

	"go.opentelemetry.io/otel/attribute"
)

// Run is a struct that represents a single run of the crawler. It contains
// all the configuration for the run, like the URLs to start with, the auth
// configurations, the allowed domains, etc. Runs are the main entities that
// group and scope a lot of the crawler's logic.
//
// A Run can be executed from multiple crawlers at the same time.
type Run struct {
	SerializableRun

	store RunStore // Used to get a live list of seen URLs.

	robots   *Robots
	sitemaps *Sitemaps
}

// SerializableRun is a serializable version of the Run struct. It is used to
// store the Run in the RunStore. It contains only static data. "Live" data,
// like seen URLs are not kept in this struct.
type SerializableRun struct {
	ID       string
	Metadata interface{}

	URLs []string

	AuthConfigs []*AuthConfig

	AllowDomains []string
	IgnorePaths  []string

	UserAgent string

	ResultReporterDSN string
}

// LiveRun is a live version of the Run struct. It contains data that should not
// be cached and accessed each time from store.
type LiveRun struct {
	Seen []string
}

func (r *Run) Configure(s RunStore, ro *Robots, si *Sitemaps) {
	r.store = s
	r.robots = ro
	r.sitemaps = si
}

func (r *Run) LogValue() slog.Value {
	return slog.StringValue(r.ShortID())
}

// ShortID returns a human-readable version of the run's ID.
func (r *Run) ShortID() string {
	// Here we can implement a simple transformation, for example, taking the first 8 characters.
	if len(r.ID) > 8 {
		return r.ID[:8] + "..." // Return the first 8 characters of the ID
	}
	return r.ID // Return the full ID if it's shorter than 8 characters
}

// GetClient configures and returns the http.Client for the Run.
func (r *Run) GetClient() *http.Client {
	return CreateCrawlerHTTPClient(r.getAuthFn(), r.UserAgent)
}

// getAuthFn returns a GetAuthFn that can be used to get the auth configuration.
func (r *Run) getAuthFn() GetAuthFn {
	return func(h *Host) (*AuthConfig, bool) {
		for _, ac := range r.AuthConfigs {
			if ac.Matches(h) {
				return ac, true
			}
		}
		return nil, false
	}
}

func (r *Run) GetCollector(ctx context.Context, q ctrlq.VisitWorkQueue, rr result.Reporter, p progress.Reporter) *collector.Collector {
	// getEnqueueFn returns the enqueue function, that will enqueue a single URL to
	// be crawled. The enqueue function is called whenever a new URL is discovered
	// by that Collector, i.e. by looking at all links in a crawled page HTML.
	getEnqueueFn := func(run *Run, q ctrlq.VisitWorkQueue, pr progress.Reporter) collector.EnqueueFn {

		// The returned function takes the run context.
		return func(ctx context.Context, c *collector.Collector, url string) error {
			logger := slog.With("run", run.LogValue(), "url", url)
			tctx, span := tracer.Start(ctx, "enqueue_element")
			defer span.End()

			p := pr.With(run.ID, url)

			span.SetAttributes(attribute.String("URL", url))
			// Ensure we never publish a URL twice for a single run. Not only does
			// this help us not put unnecessary load on the queue, it also helps with
			// ensuring there will only (mostly) be one result for a page. There is a slight
			// chance that two processes enter this function with the same run and url,
			// before one of them is finished.
			if ok, err := c.IsVisitAllowed(url); !ok {
				if err == collector.ErrCheckInternal {
					logger.Warn("Collector: Error checking if visit is allowed, not allowing visit.", "error", err)
				}
				logger.Debug("Collector: Not enqueuing visit, visit not allowed.", "error", err)
				return nil
			}
			if run.HasSeenURL(tctx, url) {
				// Do not need to enqueue an URL that has already been crawled, and its response
				// can be served from cache.
				// slog.Debug("Not enqueuing visit, URL already seen.", "run", c.Run, "url", url)
				return nil
			}

			logger.Debug("Collector: Publishing URL...")
			err := q.Publish(
				context.WithoutCancel(tctx), // The captured crawl run context.
				// Passing the run ID to identify the crawl run, so when
				// consumed the URL the run can be reconstructed by the RunManager.
				run.ID,
				url,
			)
			if err != nil {
				logger.Error("Collector: Error enqueuing visit.", "error", err)
				return err
			}

			run.SawURL(tctx, url)
			logger.Debug("Collector: URL marked as seen.")

			p.Update(tctx, progress.StateQueuedForCrawling)
			return nil
		}
	}

	// getCollectFn returns the collect function that is called once we have a
	// result. Uses the information provided in the original crawl request, i.e. the
	// WebhookConfig, that we have received via the queued message.
	getCollectFn := func(run *Run) collector.CollectFn {
		// The returned function takes the run context.
		return func(ctx context.Context, c *collector.Collector, res *collector.Response) {
			logger := slog.With("run", run.LogValue(), "url", res.Request.URL)

			logger.Debug(
				"Collect succeeded.",

				"response.body.length", len(res.Body),
				"response.status", res.StatusCode,
			)

			if DynamicConfig && run.ResultReporterDSN != "" {
				rr, err := CreateResultReporter(ctx, run.ResultReporterDSN, run, res)
				if err != nil {
					logger.Error("Failed to create report result function.", "error", err)
				} else {
					if err := rr(ctx, run.ID, res); err != nil {
						logger.Error("Failed to report result, using dynamic reporter.", "error", err)
					}
				}
			} else {
				if err := rr(ctx, run.ID, res); err != nil {
					logger.Error("Failed to report result, using default reporter.", "error", err)
				}
			}
		}
	}

	c := collector.NewCollector(
		ctx,
		// The collector.Collector will modify the http.Client passed to it, we
		// must ensure that this Client isn't shared with i.e. the Robots instance.
		r.GetClient(),
		func(a string, u string) (bool, error) {
			return r.robots.Check(u, r.getAuthFn(), a)
		},
		getEnqueueFn(r, q, p),
		getCollectFn(r),
	)

	c.UserAgent = r.UserAgent
	c.AllowDomains = r.AllowDomains
	c.IgnorePaths = r.IgnorePaths

	return c
}

// Start starts the crawl with the given URLs. It will discover sitemaps and
// enqueue the URLs. From there on more URLs will be discovered and enqueued.
func (r *Run) Start(ctx context.Context, q ctrlq.VisitWorkQueue, rr result.Reporter, pr progress.Reporter, urls []string) {
	c := r.GetCollector(ctx, q, rr, pr)

	// Decide where the initial URLs should go, users may provide sitemaps and
	// just URLs to web pages.
	//
	// FIXME: This doesn't yet support providing an alternative robots.txt.
	for _, u := range urls {
		if isProbablySitemap(u) || isProbablySiteindex(u) {
			r.sitemaps.Drain(context.WithoutCancel(ctx), r.getAuthFn(), r.UserAgent, u, c.Enqueue)
		} else {
			c.Enqueue(context.WithoutCancel(ctx), u)
		}
	}

	for _, u := range r.sitemaps.Discover(ctx, r.getAuthFn(), r.UserAgent, urls) {
		r.sitemaps.Drain(context.WithoutCancel(ctx), r.getAuthFn(), r.UserAgent, u, c.Enqueue)
	}
}

func (r *Run) SawURL(ctx context.Context, url string) {
	r.store.SawURL(ctx, r.ID, url)
}

func (r *Run) HasSeenURL(ctx context.Context, url string) bool {
	return r.store.HasSeenURL(ctx, r.ID, url)
}
