// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
)

func isProbablySitemap(url string) bool {
	return strings.HasSuffix(url, "/sitemap.xml") || strings.HasSuffix(url, "/sitemap_index.xml")
}

// Discover sitemaps for the hosts, if the robots.txt has no
// information about it, fall back to a well known location.
func discoverSitemaps(ctx context.Context, urls []string, robots *Robots) []string {

	bases := make([]string, 0, len(urls))
	for _, u := range urls {
		p, err := url.Parse(u)
		if err != nil {
			slog.Warn("Sitemaps: Failed to parse URL, skipping.", "url", u, "error", err)
			continue
		}
		base := fmt.Sprintf("%s://%s", p.Scheme, p.Hostname())

		if slices.Index(bases, base) == -1 { // Ensure unique.
			bases = append(bases, base)
		}
	}

	sitemaps := make([]string, 0)
	for _, base := range bases {
		urls, err := robots.Sitemaps(base)

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
