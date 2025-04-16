// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package progress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

const (
	// The progress service has the concept of stages, which are used to group
	// progress updates. The default stage is "crawler".
	FactorialProgressServiceDefaultStage = "crawler"
	FactorialProgressEndpointUpdate      = "api/status/update"
	// FactorialProgressEndpointTransition  = "api/status/transition-to" // Not yet implemented.
)

type FactorialProgressUpdatePayload struct {
	Stage  string `json:"stage"`
	Status string `json:"status"` // Changed to string since we're using string representations
	Run    string `json:"run_uuid"`
	URL    string `json:"url"`
}

// factorialProgressStatus maps internal ProgressStatus to Factorial API string representations.
func factorialProgressStatus(status Status) string {
	switch status {
	case StateQueuedForCrawling:
		return "queued_for_crawling"
	case StateCrawling:
		return "crawling"
	case StateCrawled:
		return "crawled"
	case StateSucceeded:
		return "succeeded"
	case StateErrored:
		return "errored"
	case StateCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// FactorialReporter is a reporter for the Factorial progress service.
type FactorialReporter struct {
	client *http.Client
	scheme string
	host   string
	tracer trace.Tracer
}

// NewFactorialReporter creates a new Factorial progress reporter.
func NewFactorialReporter(client *http.Client, scheme string, host string, tracer trace.Tracer) *FactorialReporter {
	return &FactorialReporter{
		client: client,
		scheme: scheme,
		host:   host,
		tracer: tracer,
	}
}

func (p *FactorialReporter) With(runID string, url string) *Progress {
	return &Progress{
		reporter: p,
		Stage:    FactorialProgressServiceDefaultStage,
		RunID:    runID,
		URL:      url,
	}
}

// Call sends the progress update over the wire, it implements a fire and forget approach.
func (p *FactorialReporter) Call(ctx context.Context, pu Update) error {
	logger := slog.With("run", pu.RunID, "url", pu.URL)
	logger.Debug("Progress Dispatcher: Sending update...")

	ctx, span := p.tracer.Start(ctx, "output.progress.send")
	defer span.End()

	// Convert generic ProgressUpdate to Factorial-specific payload
	payload := FactorialProgressUpdatePayload{
		Stage:  pu.Stage,
		Status: factorialProgressStatus(pu.Status),
		Run:    pu.RunID,
		URL:    pu.URL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		return err
	}
	buf := bytes.NewBuffer(body)

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("%s://%s/%s", p.scheme, p.host, FactorialProgressEndpointUpdate),
		buf,
	)
	if err != nil {
		span.RecordError(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	go func() {
		res, err := p.client.Do(req)
		defer res.Body.Close()

		if err != nil {
			logger.Error("Progress Dispatcher: Failed to send progress.", "error", err)
			span.RecordError(err)
			return
		}
		if res.StatusCode != http.StatusOK {
			logger.Error("Progress Dispatcher: Progress was not accepted.", "status", res.Status)
			span.RecordError(err)
			return
		}
	}()

	return nil
}
