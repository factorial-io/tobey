// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
)

// ProgressStatus represents the valid states for progress updates.
type ProgressStatus int

// Constants to be used for indicating what state the progress is in.
const (
	ProgressStateQueuedForCrawling ProgressStatus = iota // Used when an URL has been enqueued, see collector.Collector.EnqueueFn.
	ProgressStateCrawling                                // Used when actively crawling an URL, i.e. right before collector.Collector.Visit.
	ProgressStateCrawled                                 // Used when a URL has been crawled.
	ProgressStateSucceeded                               // When crawling has been successful.
	ProgressStateErrored                                 // When crawling has failed.
	ProgressStateCancelled                               // When crawling has been cancelled.
)

// CreateProgressReporter creates a new progress dispatcher based on the provided DSN.
// If dsn is empty, it returns a NoopProgressDispatcher.
func CreateProgressReporter(dsn string) (ProgressReporter, error) {
	if dsn == "" {
		slog.Info("Progress Reporting: Disabled, not sharing progress updates.")
		return &NoopProgressReporter{}, nil
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid progress DSN: %w", err)
	}

	switch u.Scheme {
	case "console":
		slog.Info("Progress Reporting: Using Console for progress updates.")
		return &ConsoleProgressReporter{}, nil
	case "factorial":
		slog.Info("Progress Reporting: Enabled, using Factorial progress service for updates.", "dsn", dsn)
		return &FactorialProgressReporter{
			client: CreateRetryingHTTPClient(NoAuthFn, UserAgent),
		}, nil
	case "noop":
		slog.Info("Progress Reporting: Disabled, not sharing progress updates.")
		return &NoopProgressReporter{}, nil
	default:
		return nil, fmt.Errorf("unsupported progress dispatcher type: %s", u.Scheme)
	}
}

type ProgressReporter interface {
	With(run *Run, url string) *Progress
	Call(ctx context.Context, msg ProgressUpdate) error // Usually only called by the Progressor.
}

// Progress is a helper struct that is used to update the progress of a run.
// It is returned by the With method of the ProgressDispatcher. It allows for
// cleaner code when updating the progress of a run, multiple times in the same
// function.
type Progress struct {
	reporter ProgressReporter

	Run   *Run
	URL   string
	Stage string
}

type ProgressUpdate struct {
	Run    *Run
	URL    string
	Stage  string
	Status ProgressStatus
}

// Update updates the progress with a new status
func (p *Progress) Update(ctx context.Context, status ProgressStatus) error {
	return p.reporter.Call(ctx, ProgressUpdate{
		Run:    p.Run,
		URL:    p.URL,
		Stage:  p.Stage,
		Status: status,
	})
}
