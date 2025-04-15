// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	sitemap "github.com/oxffaa/gopher-parse-sitemap"
)

func isProbablySitemap(url string) bool {
	return strings.HasSuffix(url, "/sitemap.xml")
}

func isProbablySiteindex(url string) bool {
	return strings.HasSuffix(url, "/sitemap_index.xml")
}

// NewSitemaps creates a new Sitemaps instance.
func NewSitemaps(robots *Robots) *Sitemaps {
	return &Sitemaps{
		robots: robots,
	}
}

type Sitemaps struct {
	sync.RWMutex

	robots *Robots

	// Per host fetched sitemap data, each host may have multiple sitemaps.
	data map[string][]byte
}

// Discover sitemaps for the hosts, if the robots.txt has no
// information about it, fall back to a well known location.
func (s *Sitemaps) Discover(ctx context.Context, getAuth GetAuthFn, ua string, urls []string) []string {
	bases := make([]string, 0, len(urls))
	for _, u := range urls {
		p, err := url.Parse(u)
		if err != nil {
			slog.Warn("Sitemaps: Failed to parse URL, skipping.", "url", u, "error", err)
			continue
		}
		base := fmt.Sprintf("%s://%s", p.Scheme, p.Host)

		if slices.Index(bases, base) == -1 { // Ensure unique.
			bases = append(bases, base)
		}
	}

	sitemaps := make([]string, 0)
	for _, base := range bases {
		urls, err := s.robots.Sitemaps(base, getAuth, ua) // This may block.

		if err != nil {
			slog.Error("Sitemaps: Failed to fetch sitemap URLs, taking a well known location.", "error", err)
			sitemaps = append(sitemaps, fmt.Sprintf("%s/sitemap.xml", base))
		} else if len(urls) > 0 {
			sitemaps = append(sitemaps, urls...)
		} else {
			slog.Debug("Sitemaps: No sitemap URLs found in robots.txt, taking well known location.")
			sitemaps = append(sitemaps, fmt.Sprintf("%s/sitemap.xml", base))
		}
	}
	return sitemaps
}

// Drain fetches the sitemap, parses it and yield the URLs to the yield function. This also recusively
// resolves siteindexes to sitemaps. Async function, returns immediately.
//
// FIXME: Implement this, might use FFI to use Stephan's Rust sitemap fetcher.
// FIXME: Implement this as a work process and go through the work queue.
func (s *Sitemaps) Drain(ctx context.Context, getAuth GetAuthFn, ua string, url string, yieldu func(context.Context, string) error) {
	client := CreateRetryingHTTPClient(getAuth, ua)

	var resolve func(context.Context, string) error
	resolve = func(ctx context.Context, url string) error {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		slog.Debug("Sitemaps: Resolving...", "url", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		res, err := client.Do(req)
		if err != nil {
			slog.Error("Sitemaps: Failed to fetch sitemap.", "url", url, "error", err)
			return err
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusNotFound {
			slog.Debug("Sitemaps: No sitemap found, skipping.", "url", url)
			return nil
		}

		slog.Debug("Sitemaps: Fetched sitemap/siteindex.", "url", url)

		if isProbablySitemap(url) {
			return sitemap.Parse(res.Body, func(e sitemap.Entry) error {
				slog.Debug("Sitemaps: Yield URL.", "url", e.GetLocation())
				return yieldu(ctx, e.GetLocation())
			})
		} else if isProbablySiteindex(url) {
			return sitemap.ParseIndex(res.Body, func(e sitemap.IndexEntry) error {
				slog.Debug("Sitemaps: Resolving siteindex...", "url", e.GetLocation())
				return resolve(ctx, e.GetLocation())
			})
		}
		return nil
	}
	go func(ctx context.Context) {
		if err := resolve(ctx, url); err != nil {
			slog.Error("Sitemaps: Failed to resolve sitemap/siteindex.", "url", url, "error", err)
		}
	}(ctx)
}
