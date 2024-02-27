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
	if SkipCache {
		return &http.Client{
			Timeout:   10 * time.Second,
			Transport: &UserAgentTransport{},
		}
	}
	tempdir := os.TempDir()
	slog.Debug("Using temporary directory for atomic file operations.", "dir", tempdir)

	cachedir, _ := filepath.Abs(CachePath)

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

	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &UserAgentTransport{
			Transport: otelhttp.NewTransport(httpcache.NewTransport(diskcache.NewWithDiskv(cachedisk))),
		},
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
