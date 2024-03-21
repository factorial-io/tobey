// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// CreateCrawlerHTTPClient creates a new HTTP client configured and optimized for use
// in crawling actions.
func CreateCrawlerHTTPClient() *http.Client {
	var t http.RoundTripper

	//
	// -> OtelTransport -> UserAgentTransport -> CachingTransport -> DefaultTransport
	//

	if SkipCache {
		t = http.DefaultTransport
	} else {
		cachedir, _ := filepath.Abs(CachePath)

		tempdir := os.TempDir()
		slog.Debug("Using temporary directory for atomic file operations.", "dir", tempdir)

		cachedisk := diskv.New(diskv.Options{
			BasePath:     cachedir,
			TempDir:      tempdir,
			CacheSizeMax: 1000 * 1024 * 1024, // 1GB
		})
		slog.Debug(
			"Initializing caching HTTP client...",
			"cache.dir", cachedir,
			"cache.size", cachedisk.CacheSizeMax,
		)
		t = httpcache.NewTransport(diskcache.NewWithDiskv(cachedisk))
	}

	// Add User-Agent to the transport, these headers should be added
	// before going through the caching transport.
	t = &UserAgentTransport{Transport: t}

	// Any request independent if cached or not should be traced
	// and have metrics collected.
	if UseMetrics || UseTracing {
		t = otelhttp.NewTransport(t)
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: t,
	}
}

// UserAgentTransport is a simple http.RoundTripper that adds a User-Agent
// header to each request.
type UserAgentTransport struct {
	Transport http.RoundTripper
}

func (t *UserAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("User-Agent", UserAgent)
	return t.Transport.RoundTrip(req)
}
