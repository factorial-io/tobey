// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"net/url"
	"testing"
)

// Verify that passing an URL with a port to NewHostFromURL works as expected and
// that the port is correctly set.
func TestNewHostFromURL(t *testing.T) {
	u, err := url.Parse("http://example.com:8080")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHostFromURL(u)
	if h.Port != "8080" {
		t.Errorf("Expected port to be '8080', got '%s'", h.Port)
	}
}
