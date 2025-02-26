// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
	"tobey/internal/collector"
	"tobey/internal/ctrlq"
)

func createResponse(statusCode int, headers ...string) *collector.Response {
	var h *http.Header
	if len(headers) > 0 {
		if len(headers)%2 != 0 {
			panic("createResponse requires pairs of header key/value")
		}
		header := make(http.Header)
		for i := 0; i < len(headers); i += 2 {
			header.Set(headers[i], headers[i+1])
		}
		h = &header
	}
	return &collector.Response{
		StatusCode: statusCode,
		Headers:    h,
		Request: &collector.Request{
			URL: &url.URL{
				Scheme: "https",
				Host:   "example.com",
				Path:   "/",
			},
		},
	}
}

func TestHandleFailedVisit(t *testing.T) {
	testCases := []struct {
		name            string
		response        *collector.Response
		err             error
		retries         uint32
		expectedCode    Code
		expectedError   bool
		expectPause     bool
		expectRepublish bool
	}{
		{
			name:            "301 redirect",
			response:        createResponse(301),
			expectedCode:    CodeIgnore,
			expectedError:   false,
			expectPause:     false,
			expectRepublish: false,
		},
		{
			name:            "404 not found",
			response:        createResponse(404),
			expectedCode:    CodeIgnore,
			expectedError:   false,
			expectPause:     false,
			expectRepublish: false,
		},
		{
			name:            "500 internal server error - first retry",
			response:        createResponse(500),
			retries:         0,
			expectedCode:    CodeTemporary,
			expectedError:   false,
			expectPause:     false,
			expectRepublish: true,
		},
		{
			name:            "500 internal server error - max retries exceeded",
			response:        createResponse(500),
			retries:         MaxJobRetries,
			expectedCode:    CodePermanent,
			expectedError:   true,
			expectPause:     false,
			expectRepublish: false,
		},
		{
			name:            "503 service unavailable with retry-after",
			response:        createResponse(503, "Retry-After", "60"),
			expectedCode:    CodeTemporary,
			expectedError:   false,
			expectPause:     true,
			expectRepublish: true,
		},
		{
			name:            "context deadline exceeded",
			response:        nil,
			err:             context.DeadlineExceeded,
			expectedCode:    CodeTemporary,
			expectedError:   false,
			expectPause:     false,
			expectRepublish: true,
		},
		{
			name:            "unknown status code",
			response:        createResponse(418), // I'm a teapot
			expectedCode:    CodeUnknown,
			expectedError:   false,
			expectPause:     false,
			expectRepublish: false,
		},
		{
			name:            "unhandled error without response",
			response:        nil,
			err:             errors.New("random error"),
			expectedCode:    CodeUnhandled,
			expectedError:   true,
			expectPause:     false,
			expectRepublish: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var pauseCalled bool
			var republishCalled bool

			pauseFn := func(url string, d time.Duration) error {
				pauseCalled = true
				return nil
			}

			republishFn := func(job *ctrlq.VisitJob) error {
				republishCalled = true
				return nil
			}

			job := &ctrlq.VisitJob{
				VisitMessage: &ctrlq.VisitMessage{
					ID:      0,
					Run:     "test",
					URL:     "https://example.com",
					Created: time.Now(),
					Retries: tc.retries,
				},
				Context: context.Background(),
			}

			code, err := handleFailedVisit(
				context.Background(),
				pauseFn,
				republishFn,
				job,
				tc.response,
				tc.err,
			)

			if code != tc.expectedCode {
				t.Errorf("expected code %v, got %v", tc.expectedCode, code)
			}

			if tc.expectedError && err == nil {
				t.Error("expected error but got nil")
			}

			if !tc.expectedError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}

			if tc.expectPause != pauseCalled {
				t.Errorf("expected pause called to be %v, got %v", tc.expectPause, pauseCalled)
			}

			if tc.expectRepublish != republishCalled {
				t.Errorf("expected republish called to be %v, got %v", tc.expectRepublish, republishCalled)
			}
		})
	}
}
