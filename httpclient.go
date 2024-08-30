// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/peterbourgon/diskv"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// https://www.mattcutts.com/blog/crawl-caching-proxy/

var (
	// HTTPCachePath holds a diskcache.Cache - unless SkipCache is true -
	// that is used by the HTTP client to cache responses. It is exposed as a
	// variable to allow for invalidation of the cache.
	HTTPCacheDisk *diskcache.Cache
)

func init() {
	if !SkipCache {
		cachedir, _ := filepath.Abs(HTTPCachePath)
		tempdir, _ := filepath.Abs(os.TempDir())

		HTTPCacheDisk = diskcache.NewWithDiskv(diskv.New(diskv.Options{
			BasePath:     cachedir,
			TempDir:      tempdir,
			CacheSizeMax: 1000 * 1024 * 1024, // 1GB
		}))
	}
}

// GetAuthFn returns the AuthConfig - if any - for the given Host. The second
// return value indicates if the host was found in the configuration. If the
// host was not found the caller should not add the Authorization header to the
// request, as none is needed.
type GetAuthFn func(*Host) (*AuthConfig, bool)

// NoAuthFn is a GetAuthFn that always returns nil, false. It can be used
// when no authentication is required, i.e. in testing.
func NoAuthFn(*Host) (*AuthConfig, bool) {
	return nil, false
}

// getAuthHeaderFn returns the Authorization header for the given URL.
type getAuthHeaderFn func(context.Context, *url.URL) (string, bool)

// CreateCrawlerHTTPClient creates a new HTTP client configured and optimized for use
// in crawling actions. It adds caching, tracing, metrics, and authentication support.
func CreateCrawlerHTTPClient(getAuth GetAuthFn) *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: withMiddlewares(http.DefaultTransport, getAuth),
	}
}

func CreateRetryingHTTPClient(getAuth GetAuthFn) *http.Client {
	rc := retryablehttp.NewClient()

	// Fail a little quicker, as the caller might block until
	// the request is done.
	rc.RetryMax = 2

	// Pass the last response we got back to the caller, otherwise
	// would get a nil response. This allows the surrounding code to
	// react on the status code.
	rc.ErrorHandler = retryablehttp.PassthroughErrorHandler

	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: withMiddlewares(&retryablehttp.RoundTripper{
			Client: rc,
		}, getAuth),
	}
}

// withMiddlewares adds additional transports to the provided transport, usually this is http.DefaultTransport.
//
// The order in which the transports are layered on top of each other is important:
//
// [request initiated by client]
// -> OtelTransport
// -> AuthTransport
// -> UserAgentTransport
// -> CachingTransport
// -> t (usually http.DefaultTransport)
// [endpoint]
func withMiddlewares(t http.RoundTripper, getAuth GetAuthFn) http.RoundTripper {
	if !SkipCache {
		// Adds caching support to the client. Please note that the cache is a
		// private cache and will store responses that required authentication
		// as well.
		//
		// TODO: This is currently treated as a public cache, although it is a private one. Runs that don't provide
		//       authentication my still access cached responses that required authentication.
		//
		//       We should either never cache responses that required authentication or include the Authorization
		//       headers' contents in the cache key. This would require a custom cache implementation.
		//
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Cache-Control
		t = &httpcache.Transport{
			Transport:           t,
			Cache:               HTTPCacheDisk,
			MarkCachedResponses: true,
		}
	}

	t = &AuthTransport{
		Transport: t,
		getHeaderFn: func(ctx context.Context, u *url.URL) (string, bool) {
			h := NewHostFromURL(u)

			ac, ok := getAuth(h)
			if !ok {
				return "", false
			}
			return ac.GetHeader()
		},
	}

	// Add User-Agent to the transport, these headers should be added
	// before going through the caching transport.
	t = &UserAgentTransport{Transport: t}

	// Any request independent if cached or not should be traced
	// and have metrics collected.
	if UseMetrics || UseTracing {
		t = otelhttp.NewTransport(t)
	}
	return t
}

type AuthTransport struct {
	Transport   http.RoundTripper
	getHeaderFn getAuthHeaderFn
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header, ok := t.getHeaderFn(req.Context(), req.URL)
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
