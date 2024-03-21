// Copyright 2018 Adam Tauber. All rights reserved.
// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Based on the colly HTTP scraping framework by Adam Tauber, originally
// licensed under the Apache License 2.0 modified by Factorial GmbH.

package collector

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	whatwgUrl "github.com/nlnwa/whatwg-url/url"
)

const (
	FlagNone uint8 = 1 << iota
	FlagInternal
)

type EnqueueFn func(ctx context.Context, c *Collector, u string, flags uint8) error // Enqueues a scrape.
type CollectFn func(ctx context.Context, c *Collector, res *Response, flags uint8)  // Collects the result of a scrape.

type RobotCheckFn func(agent string, u string) (bool, error)

var urlParser = whatwgUrl.NewParser(whatwgUrl.WithPercentEncodeSinglePercentSign())

// The key type is unexported to prevent collisions with context keys defined in
// other packages.
type key int

// ProxyURLKey is the context key for the request proxy address.
const ProxyURLKey key = iota

func NewCollector(
	ctx context.Context,
	client *http.Client,
	robots RobotCheckFn,
	enqueue EnqueueFn,
	collect CollectFn,
) *Collector {

	backend := &HTTPBackend{
		Client: client,
	}

	c := &Collector{
		enqueueFn: enqueue,
		collectFn: collect,

		MaxBodySize: 10 * 1024 * 1024,
		backend:     backend,

		robotsCheckFn: robots,

		Context: context.Background(),
	}

	// Unelegant, but this can be improved later.
	backend.WithCheckRedirect(c.CheckRedirectFunc())

	c.OnHTML("a[href]", func(ctx context.Context, e *HTMLElement) {
		enqueue(ctx, c, e.Request.AbsoluteURL(e.Attr("href")), FlagNone)
	})

	c.OnScraped(func(ctx context.Context, res *Response) {
		collect(ctx, c, res, FlagNone)
	})

	// Resolve linked sitemaps.
	c.OnXML("//sitemap/loc", func(ctx context.Context, e *XMLElement) {
		slog.Info("Sitemaps: Found linked sitemap.", "url", e.Text)
		enqueue(ctx, c, e.Text, FlagInternal)
	})
	c.OnXML("//urlset/url/loc", func(ctx context.Context, e *XMLElement) {
		slog.Info("Sitemaps: Found URL in sitemap.", "url", e.Text)
		enqueue(ctx, c, e.Text, FlagNone)
	})

	c.OnError(func(res *Response, err error) {
		slog.Info("Collector: Error while visiting URL.", "url", res.Request.URL.String(), "error", err, "response.status", res.StatusCode)
	})

	return c
}

type Collector struct {
	// AllowedDomains is a domain allowlist.
	AllowedDomains []string

	// UserAgent is the User-Agent string used by HTTP requests
	UserAgent string

	// DetectCharset can enable character encoding detection for non-utf8 response bodies
	// without explicit charset declaration. This feature uses https://github.com/saintfish/chardet
	DetectCharset bool

	// MaxDepth limits the recursion depth of visited URLs.
	// Set it to 0 for infinite recursion (default).
	MaxDepth int

	// MaxBodySize is the limit of the retrieved response body in bytes.
	// 0 means unlimited.
	// The default value for MaxBodySize is 10MB (10 * 1024 * 1024 bytes).
	MaxBodySize int

	// RobotsFn is the function to check if a request is allowed by robots.txt.
	robotsCheckFn RobotCheckFn

	// ParseHTTPErrorResponse allows parsing HTTP responses with non 2xx status codes.
	// By default, Colly parses only successful HTTP responses. Set ParseHTTPErrorResponse
	// to true to enable it.
	ParseHTTPErrorResponse bool

	backend *HTTPBackend

	enqueueFn EnqueueFn
	collectFn CollectFn

	// Context is the context that will be used for HTTP requests. You can set this
	// to support clean cancellation of scraping.
	Context context.Context

	// Callbacks:
	htmlCallbacks            []*htmlCallbackContainer
	xmlCallbacks             []*xmlCallbackContainer
	requestCallbacks         []RequestCallback
	responseCallbacks        []ResponseCallback
	responseHeadersCallbacks []ResponseHeadersCallback
	errorCallbacks           []ErrorCallback
	scrapedCallbacks         []ScrapedCallback
}

func (c *Collector) Enqueue(rctx context.Context, u string) error {
	return c.enqueueFn(rctx, c, u, 0)
}

func (c *Collector) EnqueueWithFlags(rctx context.Context, u string, flags uint8) error {
	return c.enqueueFn(rctx, c, u, flags)
}

func (c *Collector) Visit(rctx context.Context, URL string) error {
	return c.scrape(rctx, URL, "GET", 1, nil, nil)
}

func (c *Collector) scrape(rctx context.Context, u, method string, depth int, requestData io.Reader, hdr http.Header) error {
	parsedWhatwgURL, err := urlParser.Parse(u)
	if err != nil {
		return err
	}
	parsedURL, err := url.Parse(parsedWhatwgURL.Href(false))
	if err != nil {
		return err
	}
	if hdr == nil {
		hdr = http.Header{}
	}
	if _, ok := hdr["User-Agent"]; !ok {
		hdr.Set("User-Agent", c.UserAgent)
	}
	req, err := http.NewRequest(method, parsedURL.String(), requestData)
	if err != nil {
		return err
	}
	req.Header = hdr
	// The Go HTTP API ignores "Host" in the headers, preferring the client
	// to use the Host field on Request.
	if hostHeader := hdr.Get("Host"); hostHeader != "" {
		req.Host = hostHeader
	}
	// note: once 1.13 is minimum supported Go version,
	// replace this with http.NewRequestWithContext
	req = req.WithContext(rctx)
	if err := c.requestCheck(parsedURL, method, req.GetBody, depth); err != nil {
		return err
	}
	u = parsedURL.String()
	return c.fetch(rctx, u, method, depth, requestData, hdr, req)
}

func (c *Collector) fetch(rctx context.Context, u, method string, depth int, requestData io.Reader, hdr http.Header, req *http.Request) error {
	request := &Request{
		URL:       req.URL,
		Headers:   &req.Header,
		Host:      req.Host,
		Depth:     depth,
		Method:    method,
		Body:      requestData,
		collector: c,
	}

	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}

	c.handleOnRequest(request)

	if request.abort {
		return nil
	}

	if method == "POST" && req.Header.Get("Content-Type") == "" {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	}

	origURL := req.URL
	checkHeadersFunc := func(req *http.Request, statusCode int, headers http.Header) bool {
		if req.URL != origURL {
			request.URL = req.URL
			request.Headers = &req.Header
		}
		c.handleOnResponseHeaders(&Response{Request: request, StatusCode: statusCode, Headers: &headers})
		return !request.abort
	}

	response, err := c.backend.Do(req, c.MaxBodySize, checkHeadersFunc)
	if proxyURL, ok := req.Context().Value(ProxyURLKey).(string); ok {
		request.ProxyURL = proxyURL
	}
	if err := c.handleOnError(response, err, request); err != nil {
		return err
	}

	response.Request = request

	err = response.fixCharset(c.DetectCharset, request.ResponseCharacterEncoding)
	if err != nil {
		return err
	}

	c.handleOnResponse(response)

	err = c.handleOnHTML(rctx, response)
	if err != nil {
		if err := c.handleOnError(response, err, request); err != nil {
			slog.Error(err.Error())
		}
	}

	err = c.handleOnXML(rctx, response)
	if err != nil {
		if err := c.handleOnError(response, err, request); err != nil {
			slog.Error(err.Error())
		}
	}

	c.handleOnScraped(rctx, response)

	return err
}

func (c *Collector) requestCheck(parsedURL *url.URL, method string, getBody func() (io.ReadCloser, error), depth int) error {
	if c.MaxDepth > 0 && c.MaxDepth < depth {
		return ErrMaxDepth
	}
	if ok, err := c.IsVisitAllowed(parsedURL.String()); !ok || err != nil {
		return err
	}
	return nil
}

func (c *Collector) CheckRedirectFunc() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if ok, err := c.IsVisitAllowed(req.URL.String()); !ok {
			return fmt.Errorf("not following redirect to %q: %w", req.URL, err)
		}

		// Honor golangs default of maximum of 10 redirects
		if len(via) >= 10 {
			return http.ErrUseLastResponse
		}

		lastRequest := via[len(via)-1]

		// If domain has changed, remove the Authorization-header if it exists
		if req.URL.Host != lastRequest.URL.Host {
			req.Header.Del("Authorization")
		}

		return nil
	}
}
func (c *Collector) IsVisitAllowed(in string) (bool, error) {
	p, _ := url.Parse(in)

	checkDomain := func(u *url.URL) bool {
		// Ensure there is at least one domain in the allowlist. Do not treat an
		// empty allowlist as a wildcard.
		if c.AllowedDomains == nil || len(c.AllowedDomains) == 0 {
			slog.Error("No domains have been added to the allowlist.")
			return false
		}

		naked := strings.TrimPrefix(p.Hostname(), "www.")
		www := fmt.Sprintf("www.%s", naked)

		for _, allowed := range c.AllowedDomains {
			if allowed == naked {
				return true
			}
			if allowed == www {
				return true
			}
		}
		return false
	}

	if !checkDomain(p) {
		return false, ErrForbiddenDomain
	}

	ok, err := c.robotsCheckFn(c.UserAgent, p.String())
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrRobotsTxtBlocked
	}
	return true, nil
}
