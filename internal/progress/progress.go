// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package progress

import (
	"context"
	"log/slog"
	"time"
)

// Status represents the valid states for progress updates.
type Status int

// Constants to be used for indicating what state the progress is in.
const (
	StateQueuedForCrawling Status = iota // Used when an URL has been enqueued, see collector.Collector.EnqueueFn.
	StateCrawling                        // Used when actively crawling an URL, i.e. right before collector.Collector.Visit.
	StateCrawled                         // Used when a URL has been crawled.
	StateSucceeded                       // When crawling has been successful.
	StateErrored                         // When crawling has failed.
	StateCancelled                       // When crawling has been cancelled.
)

// Reporter is the interface for progress reporting implementations.
type Reporter interface {
	With(runID string, url string) *Progress
	Call(ctx context.Context, msg Update) error // Usually only called by the Progressor.
}

// Progress is a helper struct that is used to update the progress of a run.
// It is returned by the With method of the ProgressDispatcher. It allows for
// cleaner code when updating the progress of a run, multiple times in the same
// function.
type Progress struct {
	reporter Reporter

	RunID string
	URL   string
	Stage string
}

// Update represents a single progress update.
type Update struct {
	RunID   string
	URL     string
	Stage   string
	Status  Status
	Created time.Time
}

// Update updates the progress with a new status
func (p *Progress) Update(ctx context.Context, status Status) error {
	slog.Debug("Progress: Updating status", "run", p.RunID, "url", p.URL, "stage", p.Stage, "status", status)

	return p.reporter.Call(ctx, Update{
		RunID:   p.RunID,
		URL:     p.URL,
		Stage:   p.Stage,
		Status:  status,
		Created: time.Now(),
	})
}
