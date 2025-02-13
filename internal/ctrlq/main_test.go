package ctrlq

import (
	"context"
	"testing"
	"time"
)

// TestVisitWorkQueueLifecycle tests the lifecycle of a VisitWorkQueue, by first
// publishing a URL and then consuming it.
func TestVisitWorkQueueLifecycle(t *testing.T) {
	queue := NewMemoryVisitWorkQueue()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Open the queue
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()

	// Give the queue and promoter more time to initialize
	time.Sleep(500 * time.Millisecond)

	testURL := "https://example.com"
	testRun := "test-run-1"

	// First publish a URL
	if err := queue.Publish(ctx, testRun, testURL); err != nil {
		t.Fatal(err)
	}

	jobs, errs := queue.Consume(ctx)
	timeout := time.After(4 * time.Second)

	select {
	case job := <-jobs:
		if job == nil {
			t.Fatal("received nil job")
		}
		// Verify the job details
		if job.URL != testURL {
			t.Errorf("expected URL %q, got %q", testURL, job.URL)
		}
		if job.Run != testRun {
			t.Errorf("expected run %q, got %q", testRun, job.Run)
		}

	case err := <-errs:
		t.Error(err)

	case <-timeout:
		// Add debugging information
		t.Error("test timed out waiting for job")

	case <-ctx.Done():
		t.Error("context deadline exceeded")
	}
}
