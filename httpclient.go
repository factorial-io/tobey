// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type GetAuthHeaderFn func(ctx context.Context, host string) (string, bool)

// CreateCrawlerHTTPClient creates a new HTTP client configured and optimized for use
// in crawling actions.
func CreateCrawlerHTTPClient(getAuthHeader GetAuthHeaderFn) *http.Client {
	var t http.RoundTripper

	//
	// -> OtelTransport -> AuthTransport -> UserAgentTransport -> CachingTransport -> DefaultTransport
	//

	if SkipCache {
		t = http.DefaultTransport
	} else {
		cachedir, _ := filepath.Abs(CachePath)
		tempdir, _ := filepath.Abs(CacheTempPath)

		cachedisk := diskv.New(diskv.Options{
			BasePath:     cachedir,
			TempDir:      tempdir,
			CacheSizeMax: 1000 * 1024 * 1024, // 1GB
		})
		t = httpcache.NewTransport(diskcache.NewWithDiskv(cachedisk))
	}

	t = &AuthTransport{Transport: t, getHeaderFn: getAuthHeader}

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

type AuthTransport struct {
	Transport   http.RoundTripper
	getHeaderFn GetAuthHeaderFn
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header, ok := t.getHeaderFn(req.Context(), req.Host)
	if ok {
		slog.Debug("Client: Adding Authorization header to request.", "host", req.Host)
		req.Header.Add("Authorization", header)
	}
	return t.Transport.RoundTrip(req)
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
