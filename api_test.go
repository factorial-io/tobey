// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "testing"

func TestAuthGenerateBasicHeader(t *testing.T) {
	auth := AuthConfig{
		Method:   AuthMethodBasic,
		Username: "user",
		Password: "pass",
	}

	header, _ := auth.GetHeader()
	if header != "Basic dXNlcjpwYXNz" {
		t.Errorf("got: %q", header)
	}
}
