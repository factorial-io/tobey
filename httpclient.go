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
)

// CreateHTTPClient creates a new HTTP client configured and optimized for use
// in crawling actions.
func CreateHTTPClient() *http.Client {
	if SkipCache {
		return &http.Client{
			Timeout: 10 * time.Second,
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

	t := httpcache.NewTransport(diskcache.NewWithDiskv(cachedisk))

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: t,
	}
}
