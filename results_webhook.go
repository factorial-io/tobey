// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"tobey/internal/collector"
)

var webhookHTTPClient = CreateRetryingHTTPClient(NoAuthFn, UserAgent)

type webhookResult struct {
	Run                string      `json:"run_uuid"`
	RunMetadata        interface{} `json:"run_metadata,omitempty"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
}

// WebhookResultReporterConfig defines the configuration for webhook endpoints
type WebhookResultReporterConfig struct {
	Endpoint string `json:"endpoint"`
}

func newWebhookResultReporterConfigFromDSN(dsn string) (WebhookResultReporterConfig, error) {
	config := WebhookResultReporterConfig{}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid webhook endpoint: %w", err)
	}

	config.Endpoint = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)

	return config, nil
}

func ReportResultToWebhook(ctx context.Context, config WebhookResultReporterConfig, run *Run, res *collector.Response) error {
	logger := slog.With("run", run.ID, "url", res.Request.URL)

	logger.Debug("Result Reporter: Forwarding result to webhook ...")
	ctx, span := tracer.Start(ctx, "output.webhook.send")
	defer span.End()

	result := &webhookResult{
		Run:                run.ID,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
	}

	payload := struct {
		Action string `json:"action"`
		*webhookResult
	}{
		Action:        "collector.response",
		webhookResult: result,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		return err
	}
	buf := bytes.NewBuffer(body)

	req, err := http.NewRequestWithContext(ctx, "POST", config.Endpoint, buf)
	if err != nil {
		span.RecordError(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	go func() {
		res, err := webhookHTTPClient.Do(req)
		defer res.Body.Close()

		if err != nil {
			logger.Error("Result Reporter: Failed to send webhook.", "error", err)
			span.RecordError(err)
			return
		}
		if res.StatusCode != http.StatusOK {
			logger.Error("Result Reporter: Webhook was not accepted.", "status", res.Status)
			span.RecordError(err)
			return
		}
	}()

	return nil
}
