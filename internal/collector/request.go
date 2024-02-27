// Copyright 2018 Adam Tauber
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	// Ctx is a context between a Request and a Response
	Ctx *Context
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
		Ctx:       r.Ctx,
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
func (r *Request) Visit(rctx context.Context, URL string) error {
	return r.collector.scrape(rctx, r.AbsoluteURL(URL), "GET", r.Depth+1, nil, r.Ctx, nil)
}

// Retry submits HTTP request again with the same parameters
func (r *Request) Retry() error {
	r.Headers.Del("Cookie")
	return r.collector.scrape(context.TODO(), r.URL.String(), r.Method, r.Depth, r.Body, r.Ctx, *r.Headers)
}

// Do submits the request
func (r *Request) Do() error {
	return r.collector.scrape(context.TODO(), r.URL.String(), r.Method, r.Depth, r.Body, r.Ctx, *r.Headers)
}

// Marshal serializes the Request
func (r *Request) Marshal() ([]byte, error) {
	ctx := make(map[string]interface{})
	if r.Ctx != nil {
		r.Ctx.ForEach(func(k string, v interface{}) interface{} {
			ctx[k] = v
			return nil
		})
	}
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
		Ctx:    ctx,
	}
	if r.Headers != nil {
		sr.Headers = *r.Headers
	}
	return json.Marshal(sr)
}
