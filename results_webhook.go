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

// WebhookResultReporterConfig defines the configuration for webhook endpoints
type WebhookResultReporterConfig struct {
	Endpoint           string `json:"endpoint"`
	AllowDynamicConfig bool   `json:"allow_dynamic_config"`
}

func newWebhookResultReporterConfigFromDSN(dsn string) (WebhookResultReporterConfig, error) {
	config := WebhookResultReporterConfig{}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid webhook endpoint: %w", err)
	}

	config.AllowDynamicConfig = u.Query().Get("enable_dynamic_config") != ""
	config.Endpoint = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)

	// Only require host if dynamic config is not enabled.
	if u.Host == "" && !config.AllowDynamicConfig {
		return config, fmt.Errorf("webhook results store requires a valid host (e.g., https://example.com/results) unless dynamic configuration is enabled")
	}
	return config, nil
}

// WebhookResultReporter implements ResultsStore by sending results to a webhook endpoint.
// It sends results in a non-blocking way, following a fire-and-forget approach.
type WebhookResultReporter struct {
	client *http.Client
	// defaultEndppoint may be empty when always using dynamic config. It may be overriden
	// on a per-call basis when allowDynamicConfig is true.
	defaultEndpoint    string
	allowDynamicConfig bool
}

type WebhookResult struct {
	Run                string      `json:"run_uuid"`
	RunMetadata        interface{} `json:"run_metadata,omitempty"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
}

func NewWebhookResultReporter(ctx context.Context, config WebhookResultReporterConfig) (*WebhookResultReporter, error) {
	return &WebhookResultReporter{
		client:             CreateRetryingHTTPClient(NoAuthFn, UserAgent),
		defaultEndpoint:    config.Endpoint,
		allowDynamicConfig: config.AllowDynamicConfig,
	}, nil
}

// Accept implements ResultsStore.Accept by sending results to a webhook endpoint
func (wrs *WebhookResultReporter) Accept(ctx context.Context, config any, run *Run, res *collector.Response) error {
	var endpoint string

	slog.Debug("Result reporter: Forwarding result...")

	var webhook *WebhookResultReporterConfig
	if config != nil {
		var ok bool
		webhook, ok = config.(*WebhookResultReporterConfig)
		if !ok {
			return fmt.Errorf("invalid webhook configuration: %T", config)
		}
	}
	if webhook != nil {
		if webhook.Endpoint != "" && wrs.allowDynamicConfig {
			endpoint = webhook.Endpoint
		} else if webhook.Endpoint != "" && !wrs.allowDynamicConfig {
			slog.Warn("Dynamic webhook configuration is disabled. Ignoring custom endpoint.")
		}
	}
	// If no dynamic endpoint, fall back to default.
	if endpoint == "" {
		endpoint = wrs.defaultEndpoint
	}
	if endpoint == "" {
		return fmt.Errorf("no webhook endpoint configured - must provide either default endpoint or dynamic configuration")
	}

	logger := slog.With("endpoint", endpoint, "run", run.ID)
	logger.Debug("WebhookResultStore: Sending webhook...")

	ctx, span := tracer.Start(ctx, "output.webhook.send")
	defer span.End()

	// Create result using run metadata
	result := &WebhookResult{
		Run:                run.ID,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
	}

	payload := struct {
		Action string `json:"action"`
		*WebhookResult
	}{
		Action:        "collector.response",
		WebhookResult: result,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		return err
	}
	buf := bytes.NewBuffer(body)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, buf)
	if err != nil {
		span.RecordError(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	go func() {
		res, err := wrs.client.Do(req)
		defer res.Body.Close()

		if err != nil {
			logger.Error("WebhookResultStore: Failed to send webhook.", "error", err)
			span.RecordError(err)
			return
		}
		if res.StatusCode != http.StatusOK {
			logger.Error("WebhookResultStore: Webhook was not accepted.", "status", res.Status)
			span.RecordError(err)
			return
		}
	}()

	return nil
}
