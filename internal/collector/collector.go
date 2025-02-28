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
	ignore "github.com/sabhiram/go-gitignore"
)

type EnqueueFn func(ctx context.Context, c *Collector, u string) error // Enqueues a scrape.
type CollectFn func(ctx context.Context, c *Collector, res *Response)  // Collects the result of a scrape.
type RobotCheckFn func(agent string, u string) (ok bool, err error)    // Checks if a URL is allowed by robots.txt. ok is true if allowed.

var WellKnownFiles = []string{"robots.txt", "sitemap.xml", "sitemap_index.xml"}

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

	// Callback that discovers link in HTMl documents and feeds the links
	// back into the collector.
	c.OnHTML("a[href]", func(ctx context.Context, e *HTMLElement) {
		u, err := url.Parse(e.Request.AbsoluteURL(e.Attr("href")))
		if err != nil {
			return // Skip invalid URLs.
		}
		if !strings.HasPrefix(u.Scheme, "http") {
			return
		}
		enqueue(ctx, c, u.String())
	})

	c.OnScraped(func(ctx context.Context, res *Response) {
		collect(ctx, c, res)
	})

	c.OnError(func(res *Response, err error) {
		slog.Debug("Collector: OnError callback triggered while visiting URL.", "url", res.Request.URL.String(), "error", err, "response.status", res.StatusCode)
	})

	return c
}

type Collector struct {
	AllowDomains []string // AllowedDomains is a domain allowlist.
	IgnorePaths  []string

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
	return c.enqueueFn(rctx, c, u)
}

func (c *Collector) Visit(rctx context.Context, URL string) (*Response, error) {
	return c.scrape(rctx, URL, "GET", 1, nil, nil)
}

func (c *Collector) scrape(rctx context.Context, u, method string, depth int, requestData io.Reader, hdr http.Header) (*Response, error) {
	parsedWhatwgURL, err := urlParser.Parse(u)
	if err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(parsedWhatwgURL.Href(false))
	if err != nil {
		return nil, err
	}
	if hdr == nil {
		hdr = http.Header{}
	}
	if _, ok := hdr["User-Agent"]; !ok {
		hdr.Set("User-Agent", c.UserAgent)
	}
	req, err := http.NewRequestWithContext(rctx, method, parsedURL.String(), requestData)
	if err != nil {
		return nil, err
	}
	req.Header = hdr
	// The Go HTTP API ignores "Host" in the headers, preferring the client
	// to use the Host field on Request.
	if hostHeader := hdr.Get("Host"); hostHeader != "" {
		req.Host = hostHeader
	}
	if err := c.requestCheck(req.URL, req.Method, req.GetBody, depth); err != nil {
		return nil, err
	}
	return c.fetch(rctx, req.URL.String(), req.Method, depth, requestData, hdr, req)
}

func (c *Collector) fetch(rctx context.Context, u, method string, depth int, requestData io.Reader, hdr http.Header, req *http.Request) (*Response, error) {
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
		return nil, nil
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
		return response, err
	}

	response.Request = request

	err = response.fixCharset(c.DetectCharset, request.ResponseCharacterEncoding)
	if err != nil {
		return response, err
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

	return response, err
}

func (c *Collector) requestCheck(parsedURL *url.URL, method string, getBody func() (io.ReadCloser, error), depth int) error {
	if c.MaxDepth > 0 && c.MaxDepth < depth {
		return ErrMaxDepth
	}
	return nil
}

// We do not allow redirects as this would lead to problems with tracking in the Progress Service.
// As the request URL does not match the response URL in a redirect case.
func (c *Collector) CheckRedirectFunc() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		lastRequest := via[len(via)-1]
		slog.Debug("Found redirect", "to", req.URL, "from", lastRequest.URL)

		// Enqueue the new redirect target URL, ensure when the HTTP request is
		// cancelled, the queuing is not also cancelled.
		c.Enqueue(context.WithoutCancel(req.Context()), req.URL.String())

		// This URL was not processed, so we do not want to follow the redirect.
		return http.ErrUseLastResponse
	}
}

func (c *Collector) IsVisitAllowed(in string) (bool, error) {
	p, err := url.Parse(in)
	if err != nil {
		slog.Error("url parse error", in, "")
		return false, ErrCheckInternal
	}

	// Treats www.domain.tld and domain.tld as equivalent.
	checkDomain := func(u *url.URL) bool {
		// Ensure there is at least one domain in the allowlist. Do not treat an
		// empty allowlist as a wildcard.
		if len(c.AllowDomains) == 0 {
			slog.Error("No domains have been added to the allowlist.")
			return false
		}

		naked := strings.TrimPrefix(u.Hostname(), "www.")
		www := fmt.Sprintf("www.%s", naked)

		for _, allowed := range c.AllowDomains {
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

	// Order matters: checkPath must come after checkDomain, as we would
	// otherwise allow URLs with non-allowed domains if they have a well known
	// file in the path.
	checkPath := func(u *url.URL) bool {
		for _, wellKnown := range WellKnownFiles {
			if strings.HasSuffix(u.Path, wellKnown) {
				return true
			}
		}
		return !ignore.CompileIgnoreLines(c.IgnorePaths...).MatchesPath(u.Path)
	}
	if !checkPath(p) {
		return false, ErrForbiddenPath
	}

	ok, err := c.robotsCheckFn(c.UserAgent, p.String())
	if err != nil {
		slog.Error("robots txt error", in, "")
		return false, ErrCheckInternal
	}
	if !ok {
		return false, ErrRobotsTxtBlocked
	}
	return true, nil
}
