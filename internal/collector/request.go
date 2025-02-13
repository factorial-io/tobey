// Copyright 2018 Adam Tauber. All rights reserved.
// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Based on the colly HTTP scraping framework by Adam Tauber, originally
// licensed under the Apache License 2.0 modified by Factorial GmbH.

package collector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Request is the representation of a HTTP request made by a Collector
type Request struct {
	// URL is the parsed URL of the HTTP request
	URL *url.URL
	// Headers contains the Request's HTTP headers
	Headers *http.Header
	// the Host header
	Host string
	// Depth is the number of the parents of the request
	Depth int
	// Method is the HTTP method of the request
	Method string
	// Body is the request body which is used on POST/PUT requests
	Body io.Reader
	// ResponseCharacterencoding is the character encoding of the response body.
	// Leave it blank to allow automatic character encoding of the response body.
	// It is empty by default and it can be set in OnRequest callback.
	ResponseCharacterEncoding string
	// ID is the Unique identifier of the request
	ID        uint32
	collector *Collector
	abort     bool
	baseURL   *url.URL
	// ProxyURL is the proxy address that handles the request
	ProxyURL string
}

type serializableRequest struct {
	URL     string
	Method  string
	Depth   int
	Body    []byte
	ID      uint32
	Ctx     map[string]interface{}
	Headers http.Header
	Host    string
}

// New creates a new request with the context of the original request
func (r *Request) New(method, URL string, body io.Reader) (*Request, error) {
	u, err := urlParser.Parse(URL)
	if err != nil {
		return nil, err
	}
	u2, err := url.Parse(u.Href(false))
	if err != nil {
		return nil, err
	}
	return &Request{
		Method:    method,
		URL:       u2,
		Body:      body,
		Headers:   &http.Header{},
		Host:      r.Host,
		collector: r.collector,
	}, nil
}

// Abort cancels the HTTP request when called in an OnRequest callback
func (r *Request) Abort() {
	r.abort = true
}

// AbsoluteURL returns with the resolved absolute URL of an URL chunk.
// AbsoluteURL returns empty string if the URL chunk is a fragment or
// could not be parsed
func (r *Request) AbsoluteURL(u string) string {
	if strings.HasPrefix(u, "#") {
		return ""
	}
	var base *url.URL
	if r.baseURL != nil {
		base = r.baseURL
	} else {
		base = r.URL
	}

	absURL, err := urlParser.ParseRef(base.String(), u)
	if err != nil {
		return ""
	}
	return absURL.Href(false)
}

// Visit continues Collector's collecting job by creating a
// request and preserves the Context of the previous request.
// Visit also calls the previously provided callbacks
func (r *Request) Visit(rctx context.Context, URL string) (*Response, error) {
	return r.collector.scrape(rctx, r.AbsoluteURL(URL), "GET", r.Depth+1, nil, nil)
}

// Do submits the request
func (r *Request) Do() (*Response, error) {
	return r.collector.scrape(context.TODO(), r.URL.String(), r.Method, r.Depth, r.Body, *r.Headers)
}

// Marshal serializes the Request
func (r *Request) Marshal() ([]byte, error) {
	var err error
	var body []byte
	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
	}
	sr := &serializableRequest{
		URL:    r.URL.String(),
		Host:   r.Host,
		Method: r.Method,
		Depth:  r.Depth,
		Body:   body,
		ID:     r.ID,
	}
	if r.Headers != nil {
		sr.Headers = *r.Headers
	}
	return json.Marshal(sr)
}
