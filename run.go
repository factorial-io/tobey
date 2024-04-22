// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"log/slog"
	"net/http"
	"tobey/internal/collector"
)

// Run is a struct that represents a single run of the crawler. It contains
// all the configuration for the run, like the URLs to start with, the auth
// configurations, the allowed domains, etc. Runs are the main entities that
// group and scope a lot of the crawler's logic.
//
// A Run can be executed from multiple crawlers at the same time.
type Run struct {
	SerializableRun

	store  RunStore // Used to get a live list of seen URLs.
	robots *Robots
}

// SerializableRun is a serializable version of the Run struct. It is used to
// store the Run in the RunStore. It contains only static data. "Live" data,
// like seen URLs are not kept in this struct.
type SerializableRun struct {
	ID string

	URLs []string

	AuthConfigs []*AuthConfig

	AllowedDomains []string
	IgnorePaths    []string

	SkipRobots           bool
	SkipSitemapDiscovery bool

	WebhookConfig *WebhookConfig
}

// LiveRun is a live version of the Run struct. It contains data that should not
// be cached and accessed each time from store.
type LiveRun struct {
	Seen []string
}

// GetClient configures and returns the http.Client for the Run.
func (r *Run) GetClient() *http.Client {
	return CreateCrawlerHTTPClient(func(ctx context.Context, host string) (string, bool) {
		for _, auth := range r.AuthConfigs {
			if auth.Host == host {
				return auth.GetHeader()
			}
		}
		return "", false
	})
}

// ConfigureStore must be called before calling into one of the methods that
// access live data.
func (r *Run) ConfigureStore(s RunStore) {
	r.store = s
}

// ConfigureRobots configures a Robots instance for the Run, it ensures we retrieve
// the robots.txt file with the same http.Client as we use for crawling. The
// http.Client might be using custom headers for authentication. These are only
// available to the Run.
func (r *Run) ConfigureRobots() {
	r.robots = NewRobots(r.GetClient())
}

func (r *Run) GetCollector(ctx context.Context, q WorkQueue, p Progress, h *WebhookDispatcher) *collector.Collector {
	c := collector.NewCollector(
		ctx,
		// The collector.Collector will modify the http.Client passed to it, we
		// must ensure that this Client isn't shared with i.e. the Robots instance.
		r.GetClient(),
		func(a string, u string) (bool, error) {
			if r.SkipRobots {
				return true, nil
			}
			return r.robots.Check(a, u)
		},
		getEnqueueFn(r, q, p),
		getCollectFn(r, h),
	)

	c.UserAgent = UserAgent
	c.AllowedDomains = r.AllowedDomains
	c.IgnorePaths = r.IgnorePaths

	return c
}

// Start starts the crawl with the given URLs. It will discover sitemaps and
// enqueue the URLs. From there on more URLs will be discovered and enqueued.
func (r *Run) Start(ctx context.Context, q WorkQueue, p Progress, h *WebhookDispatcher, urls []string) {
	c := r.GetCollector(ctx, q, p, h)

	for _, u := range urls {
		if isProbablySitemap(u) {
			c.EnqueueWithFlags(context.WithoutCancel(ctx), u, collector.FlagInternal)
		} else {
			c.Enqueue(context.WithoutCancel(ctx), u)
		}
	}

	if !r.SkipSitemapDiscovery {
		for _, u := range r.DiscoverSitemaps(ctx, urls) {
			slog.Debug("Sitemaps: Enqueueing sitemap for crawling.", "url", u)
			c.EnqueueWithFlags(context.WithoutCancel(ctx), u, collector.FlagInternal)
		}
	}
}

func (r *Run) DiscoverSitemaps(ctx context.Context, urls []string) []string {
	return discoverSitemaps(ctx, urls, r.robots)
}

func (r *Run) SawURL(ctx context.Context, url string) {
	r.store.SawURL(ctx, r.ID, url)
}

func (r *Run) HasSeenURL(ctx context.Context, url string) bool {
	return r.store.HasSeenURL(ctx, r.ID, url)
}
