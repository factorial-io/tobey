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
	"tobey/internal/collector"
)

type WebhookConfig struct {
	Endpoint string      `json:"endpoint"`
	Data     interface{} `json:"data"` // Accept arbitrary data here.
}

// The messages that should go over the wire.
type WebhookPayload struct {
	Action string `json:"action"`
	Run    string `json:"run_uuid"`
	// TODO: Figure out if we want to use "Standard Webhook" and/or if
	// we than want to nest all results data under Data as to prevent
	// collisions with Action and other fields.
	// TODO Talk about the interface variation
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded once marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
	Data               interface{} `json:"data"` // Pass through arbitrary data here.
}

func NewWebhookDispatcher(ctx context.Context) *WebhookDispatcher {
	return &WebhookDispatcher{
		client: CreateRetryingHTTPClient(NoAuthFn),
	}
}

type WebhookDispatcher struct {
	client *http.Client
}

// Send sends a webhook to the given endpoint. It returns immediately, and is not blocking. It implements a fire and forget approach.
func (wd *WebhookDispatcher) Send(ctx context.Context, webhook *WebhookConfig, run string, res *collector.Response) error {
	logger := slog.With("endpoint", webhook.Endpoint, "run", run)
	logger.Debug("Webhook Dispatcher: Sending webhook...")

	ctx, span := tracer.Start(ctx, "output.webhook.send")
	defer span.End()

	payload := WebhookPayload{
		Action: "collector.response",
		Run:    run,

		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,

		// We pass through the data we received taking in the
		// initial crawl request, verbatim.
		Data: webhook.Data,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		return err
	}
	buf := bytes.NewBuffer(body)

	req, err := http.NewRequestWithContext(ctx, "POST", webhook.Endpoint, buf)
	if err != nil {
		span.RecordError(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	go func() {
		res, err := wd.client.Do(req)
		defer res.Body.Close()

		if err != nil {
			logger.Error("Webhook Dispatcher: Failed to send webhook.", "error", err)
			span.RecordError(err)
			return
		}
		if res.StatusCode != http.StatusOK {
			logger.Error("Webhook Dispatcher: Webhook was not accepted.", "status", res.Status)
			span.RecordError(err)
			return
		}
	}()

	return nil
}
