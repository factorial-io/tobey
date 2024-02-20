// Based on colly HTTP scraping framework, Copyright 2018 Adam Tauber,
// originally licensed under the Apache License 2.0

package collector

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	whatwgUrl "github.com/nlnwa/whatwg-url/url"
)

type EnqueueFn func(*Collector, string) error                      // Enqueues a scrape.
type VisitFn func(*Collector, string) (bool, time.Duration, error) // Performs a scrape.
type CollectFn func(*Collector, *Response)                         // Collects the result of a scrape.

var urlParser = whatwgUrl.NewParser(whatwgUrl.WithPercentEncodeSinglePercentSign())

// The key type is unexported to prevent collisions with context keys defined in
// other packages.
type key int

// ProxyURLKey is the context key for the request proxy address.
const ProxyURLKey key = iota

func NewCollector(ctx context.Context, client *http.Client, run uint32, domains []string, enqueue EnqueueFn, visit VisitFn, collect CollectFn) *Collector {
	backend := &HTTPBackend{
		Client: client,
	}

	c := &Collector{
		AllowedDomains: domains,
		Run:            run,
		UserAgent:      fmt.Sprintf("Website Standards Bot/2.0"),

		enqueueFn: enqueue,
		visitFn:   visit,
		collectFn: collect,

		MaxBodySize:     10 * 1024 * 1024,
		backend:         backend,
		IgnoreRobotsTxt: true,
		Context:         context.Background(),
	}

	// Unelegant, but this can be improved later.
	backend.WithCheckRedirect(c.CheckRedirectFunc())

	// c.robotsMap = make(map[string]*robotstxt.RobotsData)

	c.OnHTML("a[href]", func(e *HTMLElement) {
		enqueue(c, e.Request.AbsoluteURL(e.Attr("href")))
	})

	c.OnScraped(func(res *Response) {
		collect(c, res)
	})

	// Resolve linked sitemaps.
	c.OnXML("//sitemap/loc", func(e *XMLElement) {
		enqueue(c, e.Text)
	})

	c.OnXML("//urlset/url/loc", func(e *XMLElement) {
		enqueue(c, e.Text)
	})

	c.OnError(func(res *Response, err error) {
		slog.Info("Error while visiting URL.", "url", res.Request.URL.String(), "error", err, "response.status", res.StatusCode)
	})

	return c
}

type Collector struct {
	// Run is the unique identifier of a collector.
	Run uint32

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

	// IgnoreRobotsTxt allows the Collector to ignore any restrictions set by
	// the target host's robots.txt file.  See http://www.robotstxt.org/ for more
	// information.
	IgnoreRobotsTxt bool

	// ParseHTTPErrorResponse allows parsing HTTP responses with non 2xx status codes.
	// By default, Colly parses only successful HTTP responses. Set ParseHTTPErrorResponse
	// to true to enable it.
	ParseHTTPErrorResponse bool

	backend *HTTPBackend

	enqueueFn EnqueueFn
	visitFn   VisitFn
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

func (c *Collector) EnqueueVisit(URL string) error {
	return c.enqueueFn(c, URL)
}

func (c *Collector) Visit(URL string) (bool, time.Duration, error) {
	return c.visitFn(c, URL)
}

func (c *Collector) Scrape(URL string) error {
	if check := c.scrape(URL, "HEAD", 1, nil, nil, nil); check != nil {
		return check
	}
	return c.scrape(URL, "GET", 1, nil, nil, nil)
}

func (c *Collector) scrape(u, method string, depth int, requestData io.Reader, ctx *Context, hdr http.Header) error {
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
	req = req.WithContext(c.Context)
	if err := c.requestCheck(parsedURL, method, req.GetBody, depth); err != nil {
		return err
	}
	u = parsedURL.String()
	return c.fetch(u, method, depth, requestData, ctx, hdr, req)
}

func (c *Collector) fetch(u, method string, depth int, requestData io.Reader, ctx *Context, hdr http.Header, req *http.Request) error {
	if ctx == nil {
		ctx = NewContext()
	}
	request := &Request{
		URL:       req.URL,
		Headers:   &req.Header,
		Host:      req.Host,
		Ctx:       ctx,
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
		c.handleOnResponseHeaders(&Response{Ctx: ctx, Request: request, StatusCode: statusCode, Headers: &headers})
		return !request.abort
	}

	response, err := c.backend.Do(req, c.MaxBodySize, checkHeadersFunc)
	if proxyURL, ok := req.Context().Value(ProxyURLKey).(string); ok {
		request.ProxyURL = proxyURL
	}
	if err := c.handleOnError(response, err, request, ctx); err != nil {
		return err
	}

	response.Ctx = ctx
	response.Request = request

	err = response.fixCharset(c.DetectCharset, request.ResponseCharacterEncoding)
	if err != nil {
		return err
	}

	c.handleOnResponse(response)

	err = c.handleOnHTML(response)
	if err != nil {
		c.handleOnError(response, err, request, ctx)
	}

	err = c.handleOnXML(response)
	if err != nil {
		c.handleOnError(response, err, request, ctx)
	}

	c.handleOnScraped(response)

	return err
}

func (c *Collector) requestCheck(parsedURL *url.URL, method string, getBody func() (io.ReadCloser, error), depth int) error {
	if c.MaxDepth > 0 && c.MaxDepth < depth {
		return ErrMaxDepth
	}
	if ok := c.IsDomainAllowed(parsedURL.Hostname()); !ok {
		return ErrForbiddenDomain
	}

	// TODO: Use again, once we implement robots support.
	// if method != "HEAD" && !c.IgnoreRobotsTxt {
	// 	if err := c.checkRobots(parsedURL); err != nil {
	// 		return err
	// 	}
	// }

	return nil
}

func (c *Collector) CheckRedirectFunc() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if ok := c.IsDomainAllowed(req.URL.Hostname()); !ok {
			return fmt.Errorf("Not following redirect to %q: %w", req.URL, ErrForbiddenDomain)
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
func (c *Collector) IsDomainAllowed(in string) bool {
	if c.AllowedDomains == nil || len(c.AllowedDomains) == 0 {
		return false
	}

	naked := strings.TrimPrefix(in, "www.")
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
