// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

const (
	// The progress service has the concept of stages, which are used to group
	// progress updates. The default stage is "crawler".
	FactorialProgressServiceDefaultStage = "crawler"
	FactorialProgressEndpointUpdate      = "api/status/update"
	// FactorialProgressEndpointTransition  = "api/status/transition-to" // Not yet implemented.
)

// FactorialProgressServiceDispatcher is a dispatcher for the Factorial progress service.
type FactorialProgressServiceDispatcher struct {
	client *http.Client
}

func (p *FactorialProgressServiceDispatcher) With(run string, url string) *Progressor {
	return &Progressor{
		dispatcher: p,
		stage:      FactorialProgressServiceDefaultStage,
		Run:        run,
		URL:        url,
	}
}

// Call sends the progress update over the wire, it implements a fire and forget approach.
func (p *FactorialProgressServiceDispatcher) Call(ctx context.Context, pu ProgressUpdate) error {
	logger := slog.With("run", pu.Run, "url", pu.URL)
	logger.Debug("Progress Dispatcher: Sending update...")

	ctx, span := tracer.Start(ctx, "output.progress.send")
	defer span.End()

	payload := pu
	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		return err
	}
	buf := bytes.NewBuffer(body)

	req, err := http.NewRequestWithContext(ctx, "POST", FactorialProgressEndpointUpdate, buf)
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
