// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"net/http"
	"testing"
)

func TestAuthTransportAddsAuthorizationHeader(t *testing.T) {
	transport := &AuthTransport{
		Transport: http.DefaultTransport,
		getHeaderFn: func(ctx context.Context, host string) (string, bool) {
			return "<header>", true
		},
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	transport.RoundTrip(req)

	if h := req.Header.Get("Authorization"); h != "<header>" {
		t.Errorf("got: %q", h)
	}
}
