package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"tobey/internal/ctrlq"
)

// setupTestServer creates and configures a test server with all necessary dependencies
func setupTestServer(ctx context.Context, t *testing.T) *httptest.Server {
	robots := NewRobots()
	sitemaps := NewSitemaps(robots)
	runs := NewRunManager(nil, robots, sitemaps)

	queue := ctrlq.CreateWorkQueue(nil)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}

	defaultrr, err := CreateResultReporter(ctx, "noop://", nil, nil)
	if err != nil {
		panic(err)
	}

	progress, err := CreateProgressReporter("noop://")
	if err != nil {
		t.Fatal(err)
	}

	NewVisitorPool(
		ctx,
		1,
		runs,
		queue,
		defaultrr,
		progress,
	)

	return httptest.NewServer(setupRoutes(runs, queue, defaultrr, progress))
}

// TestCrawlRequestSubmission tries to perform a full integration test. On one
// hand side we have an instance of tobey on the other hand we have a crawl
// target. We then start the crawl and check if the results are as expected.
func TestCrawlRequestSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hits := make(chan string, 100)
	defer close(hits)

	// Create a test target server that simulates a website to crawl.
	targets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path

		switch r.URL.Path {
		case "/503":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service Unavailable"))
		case "/slow":
			// Simulate a slow response
			time.Sleep(120 * time.Millisecond) // Using shorter time for tests
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		case "/":
			w.Write([]byte(`
				<html>
					<body>
						<a href="/page1">Page 1</a>
						<a href="/page2">Page 2</a>
					</body>
				</html>
			`))
		case "/page1":
			w.Write([]byte(`
				<html>
					<body>
						<h1>Page 1</h1>
						<a href="/page2">Go to Page 2</a>
					</body>
				</html>
			`))
		case "/page2":
			w.Write([]byte(`
				<html>
					<body>
						<h1>Page 2</h1>
						<a href="/page1">Back to Page 1</a>
					</body>
				</html>
			`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer targets.Close()

	tobeys := setupTestServer(ctx, t)
	defer tobeys.Close()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "POST", tobeys.URL, bytes.NewBufferString(targets.URL))
	if err != nil {
		t.Fatal(err)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", res.Status)
	}

	var apires APIResponse
	if err := json.NewDecoder(res.Body).Decode(&apires); err != nil {
		t.Fatal(err)
	}

	if apires.Run == "" {
		t.Error("Expected non-empty run ID")
	}

	// Wait a moment for the crawl to be performed.
	time.Sleep(100 * time.Millisecond)

	// We now want to check whether exactly three 5 were made. We need to make sure we don't block on reading from the channel.
	// 3 page request + 1 robots.txt request + 1 sitemap request = 5 requests
	for i := 0; i < 5; i++ {
		select {
		case hit := <-hits:
			if hit == "/" || hit == "/page1" || hit == "/page2" || hit == "/robots.txt" || hit == "/sitemap.xml" {
				continue
			} else {
				t.Errorf("Unexpected hit: %s", hit)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("Expected 5 hits, got %d", i)
		}
	}
}

/*
func TestCrawlErrorConditions(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a test target server that simulates error conditions
	targets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/503":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service Unavailable"))
		case "/slow":
			time.Sleep(120 * time.Millisecond) // Using shorter time for tests
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer targets.Close()

	tobeys := setupTestServer(ctx, t)
	defer tobeys.Close()

	// Test 503 response
	t.Run("503 Response", func(t *testing.T) {
		client := &http.Client{}
		req, err := http.NewRequestWithContext(ctx, "POST", tobeys.URL, bytes.NewBufferString(targets.URL+"/503"))
		if err != nil {
			t.Fatal(err)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status OK for crawl submission, got %v", res.Status)
		}

		var apires APIResponse
		if err := json.NewDecoder(res.Body).Decode(&apires); err != nil {
			t.Fatal(err)
		}

		// Wait a moment for the crawl to be processed
		time.Sleep(200 * time.Millisecond)

		// TODO: Add specific assertions about how Tobey should handle 503 responses
		// For example, check if it respects the Retry-After header
		// or if it marks the run appropriately in the status
	})

	// Test slow response
	t.Run("Slow Response", func(t *testing.T) {
		client := &http.Client{}
		req, err := http.NewRequestWithContext(ctx, "POST", tobeys.URL, bytes.NewBufferString(targets.URL+"/slow"))
		if err != nil {
			t.Fatal(err)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status OK for crawl submission, got %v", res.Status)
		}

		var apires APIResponse
		if err := json.NewDecoder(res.Body).Decode(&apires); err != nil {
			t.Fatal(err)
		}

		// Wait a moment for the crawl to be processed
		time.Sleep(200 * time.Millisecond)

		// TODO: Add specific assertions about how Tobey should handle slow responses
		// For example, check if it respects timeout settings
		// or if it marks the run appropriately in the status
	})
}
*/
