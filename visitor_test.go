// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
	"tobey/internal/collector"
	"tobey/internal/ctrlq"
	"tobey/internal/progress"
)

func createTestResponse(statusCode int, headers ...string) *collector.Response {
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
			name:         "301 redirect",
			response:     createTestResponse(301),
			expectedCode: CodeIgnore,
		},
		{
			name:         "302 redirect",
			response:     createTestResponse(302),
			expectedCode: CodeIgnore,
		},
		{
			name:         "404 not found",
			response:     createTestResponse(404),
			expectedCode: CodeIgnore,
		},
		{
			name:            "500 internal server error - first retry",
			response:        createTestResponse(500),
			retries:         0,
			expectedCode:    CodeTemporary,
			expectRepublish: true,
		},
		{
			name:          "500 internal server error - max retries exceeded",
			response:      createTestResponse(500),
			retries:       MaxJobRetries,
			expectedCode:  CodePermanent,
			expectedError: true,
		},
		{
			name:            "503 service unavailable with retry-after",
			response:        createTestResponse(503, "Retry-After", "60"),
			expectedCode:    CodeTemporary,
			expectPause:     true,
			expectRepublish: true,
		},
		{
			name:            "context deadline exceeded",
			err:             context.DeadlineExceeded,
			expectedCode:    CodeTemporary,
			expectRepublish: true,
		},
		{
			name:         "unknown status code",
			response:     createTestResponse(418), // I'm a teapot
			expectedCode: CodeUnknown,
		},
		{
			name:          "unhandled error without response",
			err:           errors.New("random error"),
			expectedCode:  CodeUnhandled,
			expectedError: true,
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

func TestVisitorProgressTrackingWithRedirects(t *testing.T) {
	// Create a test server that redirects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
			return
		}
		if r.URL.Path == "/final" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Final destination"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create a memory progress reporter to track progress updates
	progressReporter := progress.NewMemoryReporter()

	// Create a simple result reporter
	resultReporter := func(ctx context.Context, runID string, res *collector.Response) error {
		return nil
	}

	// Create a memory queue
	queue := ctrlq.NewMemoryVisitWorkQueue()
	if err := queue.Open(context.Background()); err != nil {
		t.Fatalf("Failed to open queue: %v", err)
	}
	defer queue.Close()

	// Create a run manager
	robots := NewRobots()
	sitemaps := NewSitemaps(robots)
	runs := NewRunManager(nil, robots, sitemaps)

	// Create a visitor
	visitor := &Visitor{
		id:       1,
		runs:     runs,
		queue:    queue,
		result:   resultReporter,
		progress: progressReporter,
		logger:   slog.Default(),
	}

	// Create a test run
	run := &Run{
		SerializableRun: SerializableRun{
			ID:   "test-run",
			URLs: []string{server.URL + "/redirect"},
		},
	}
	runs.Add(context.Background(), run)

	// Create a test job
	job := &ctrlq.VisitJob{
		VisitMessage: &ctrlq.VisitMessage{
			ID:      1,
			Run:     "test-run",
			URL:     server.URL + "/redirect",
			Created: time.Now(),
		},
		Context: context.Background(),
	}

	// Process the job
	err := visitor.process(context.Background(), job)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Wait a bit for progress updates to be processed
	time.Sleep(100 * time.Millisecond)

	// Check that progress was tracked for the original URL
	updates := progressReporter.GetUpdates("test-run")
	if len(updates) == 0 {
		t.Fatal("Expected progress updates, got none")
	}

	// Find the progress update for the original URL
	var originalURLUpdate *progress.Update
	for _, update := range updates {
		if update.URL == server.URL+"/redirect" {
			originalURLUpdate = &update
			break
		}
	}

	if originalURLUpdate == nil {
		t.Fatal("Expected progress update for original URL, got none")
	}

	// Check that the progress shows success (even though there was a redirect)
	if originalURLUpdate.Status != progress.StateSucceeded {
		t.Errorf("Expected status StateSucceeded, got %v", originalURLUpdate.Status)
	}

	// Verify that no progress was tracked for the redirect destination
	for _, update := range updates {
		if update.URL == server.URL+"/final" {
			t.Error("Expected no progress tracking for redirect destination")
		}
	}
}
