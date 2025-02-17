// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAuthTransportAddsAuthorizationHeader(t *testing.T) {
	transport := &AuthTransport{
		Transport: http.DefaultTransport,
		getHeaderFn: func(ctx context.Context, u *url.URL) (string, bool) {
			return "<header>", true
		},
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	transport.RoundTrip(req)

	if h := req.Header.Get("Authorization"); h != "<header>" {
		t.Errorf("got: %q", h)
	}
}

func TestRetryingHTTPClientReturnsResponseOn503(t *testing.T) {
	// Start a test HTTP server that only returns 503.
	errserver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(503)
		// Write something to avoid empty body.
		rw.Write([]byte("Service Unavailable"))
	}))
	defer errserver.Close()

	client := CreateRetryingHTTPClient(NoAuthFn, "test")

	resp, err := client.Get(errserver.URL)
	if err != nil {
		t.Log(err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != 503 {
		t.Errorf("got: %d", resp.StatusCode)
	}
}
