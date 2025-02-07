// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"log/slog"
)

// ProgressStatus represents the valid states for progress updates
type ProgressStatus string

// Constants to be used for indicating what state the progress is in.
const (
	ProgressStateQueuedForCrawling ProgressStatus = "queued_for_crawling" // Used when an URL has been enqueued, see collector.Collector.EnqueueFn.
	ProgressStateCrawling          ProgressStatus = "crawling"            // Used when actively crawling an URL, i.e. right before collector.Collector.Visit.
	ProgressStateCrawled           ProgressStatus = "crawled"
	ProgressStateSucceeded         ProgressStatus = "succeeded" // When crawling has been successful.
	ProgressStateErrored           ProgressStatus = "errored"
	ProgressStateCancelled         ProgressStatus = "cancelled"
)

// CreateProgress creates a new progress dispatcher based on the provided DSN.
// If dsn is empty, it returns a NoopProgressDispatcher.
func CreateProgress(dsn string) ProgressDispatcher {
	if dsn != "" {
		slog.Info("Using progress service for updates.", "dsn", dsn)
		return &FactorialProgressServiceDispatcher{
			client: CreateRetryingHTTPClient(NoAuthFn),
		}
	}
	slog.Debug("Not sharing progress updates.")
	return &NoopProgressDispatcher{}
}

type ProgressDispatcher interface {
	With(run string, url string) *Progressor
	Call(ctx context.Context, msg ProgressUpdate) error // Usually only called by the Progressor.
}

type Progressor struct {
	dispatcher ProgressDispatcher

	stage string
	Run   string
	URL   string
}

type ProgressUpdate struct {
	Stage  string         `json:"stage"`
	Status ProgressStatus `json:"status"`
	Run    string         `json:"run_uuid"` // uuid of the run
	URL    string         `json:"url"`
}

// Update updates the progress with a new status
func (p *Progressor) Update(ctx context.Context, status ProgressStatus) error {
	return p.dispatcher.Call(ctx, ProgressUpdate{
		Stage:  p.stage,
		Run:    p.Run,
		URL:    p.URL,
		Status: status,
	})
}
