// Copyright 2018 Adam Tauber. All rights reserved.
// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Based on the colly HTTP scraping framework by Adam Tauber, originally
// licensed under the Apache License 2.0 modified by Factorial GmbH.

package collector

import (
	"io"
	"net/http"
	"strings"
	"time"

	"compress/gzip"
)

type HTTPBackend struct {
	Client *http.Client
}

type checkHeadersFunc func(req *http.Request, statusCode int, header http.Header) bool
type checkRedirectFunc func(req *http.Request, via []*http.Request) error

func (h *HTTPBackend) WithCheckRedirect(fn checkRedirectFunc) {
	h.Client.CheckRedirect = fn
}

func (h *HTTPBackend) Do(request *http.Request, bodySize int, checkHeadersFunc checkHeadersFunc) (*Response, error) {
	start := time.Now()

	res, err := h.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	finalRequest := request
	if res.Request != nil {
		finalRequest = res.Request
	}
	if !checkHeadersFunc(finalRequest, res.StatusCode, res.Header) {
		// closing res.Body (see defer above) without reading it aborts
		// the download
		return nil, ErrAbortedAfterHeaders
	}

	var bodyReader io.Reader = res.Body
	if bodySize > 0 {
		bodyReader = io.LimitReader(bodyReader, int64(bodySize))
	}
	contentEncoding := strings.ToLower(res.Header.Get("Content-Encoding"))
	if !res.Uncompressed && (strings.Contains(contentEncoding, "gzip") || (contentEncoding == "" && strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "gzip")) || strings.HasSuffix(strings.ToLower(finalRequest.URL.Path), ".xml.gz")) {
		bodyReader, err = gzip.NewReader(bodyReader)
		if err != nil {
			return nil, err
		}
		defer bodyReader.(*gzip.Reader).Close()
	}
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: res.StatusCode,
		Body:       body,
		Headers:    &res.Header,
		Took:       time.Since(start),
	}, nil
}
