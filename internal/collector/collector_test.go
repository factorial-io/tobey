package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectorRedirects(t *testing.T) {
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

	// Create a simple enqueue function that just logs
	enqueueFn := func(ctx context.Context, c *Collector, u string) error {
		return nil
	}

	// Create a simple collect function that just logs
	collectFn := func(ctx context.Context, c *Collector, res *Response) {
		// This would normally report results
	}

	// Create a simple robots check function
	robotsFn := func(agent string, u string) (bool, error) {
		return true, nil
	}

	// Create HTTP client
	client := &http.Client{}

	// Create collector
	c := NewCollector(context.Background(), client, robotsFn, enqueueFn, collectFn)

	// Test that redirects work
	res, err := c.Visit(context.Background(), server.URL+"/redirect")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if res == nil {
		t.Fatal("Expected response, got nil")
	}

	// The response should be from the final destination
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}

	if string(res.Body) != "Final destination" {
		t.Errorf("Expected body 'Final destination', got '%s'", string(res.Body))
	}

	// The request URL should be the final redirect URL (this is how HTTP redirects work)
	if res.Request.URL.String() != server.URL+"/final" {
		t.Errorf("Expected request URL to be final redirect URL, got %s", res.Request.URL.String())
	}
}

func TestCollectorMaxRedirects(t *testing.T) {
	// Create a test server that redirects in a loop
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always redirect back to /redirect, creating an infinite loop
		http.Redirect(w, r, "/redirect", http.StatusMovedPermanently)
	}))
	defer server.Close()

	// Create a simple enqueue function that just logs
	enqueueFn := func(ctx context.Context, c *Collector, u string) error {
		return nil
	}

	// Create a simple collect function that just logs
	collectFn := func(ctx context.Context, c *Collector, res *Response) {
		// This would normally report results
	}

	// Create a simple robots check function
	robotsFn := func(agent string, u string) (bool, error) {
		return true, nil
	}

	// Create HTTP client
	client := &http.Client{}

	// Create collector with low redirect limit
	c := NewCollector(context.Background(), client, robotsFn, enqueueFn, collectFn)
	c.MaxRedirects = 3 // Override default for testing

	// Test that redirects are limited
	_, err := c.Visit(context.Background(), server.URL+"/redirect")
	if err == nil {
		t.Fatal("Expected error due to redirect limit, got nil")
	}

	// When redirect limit is exceeded, we should get an error
	if err.Error() != "Moved Permanently" {
		t.Errorf("Expected 'Moved Permanently' error, got %v", err)
	}
}
