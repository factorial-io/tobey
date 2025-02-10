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

// WebhookResultStoreConfig defines the configuration for webhook endpoints
type WebhookResultStoreConfig struct {
	Endpoint string      `json:"endpoint"`
	Data     interface{} `json:"data"` // Accept arbitrary data here.
}

func (c *WebhookResultStoreConfig) Validate() error {
	return nil
}

func (c *WebhookResultStoreConfig) GetWebhook() *WebhookResultStoreConfig {
	return c
}

// WebhookResultStore implements ResultsStore by sending results to a webhook endpoint.
// It sends results in a non-blocking way, following a fire-and-forget approach.
type WebhookResultStore struct {
	client             *http.Client
	defaultEndpoint    string // Can be empty when only using dynamic config
	allowDynamicConfig bool
}

func NewWebhookResultStore(ctx context.Context, endpoint string) *WebhookResultStore {
	u, err := url.Parse(endpoint)
	if err != nil {
		return &WebhookResultStore{
			client:             CreateRetryingHTTPClient(NoAuthFn),
			defaultEndpoint:    endpoint,
			allowDynamicConfig: false,
		}
	}

	allowDynamic := u.Query().Get("enable_dynamic_config") != ""

	// If dynamic config is enabled, we don't require a default endpoint
	var cleanEndpoint string
	if u.Host != "" {
		u.RawQuery = ""
		cleanEndpoint = u.String()
	}

	return &WebhookResultStore{
		client:             CreateRetryingHTTPClient(NoAuthFn),
		defaultEndpoint:    cleanEndpoint,
		allowDynamicConfig: allowDynamic,
	}
}

// Save implements ResultsStore.Save by sending results to a webhook endpoint
func (wrs *WebhookResultStore) Save(ctx context.Context, config ResultStoreConfig, run *Run, res *collector.Response) error {
	var endpoint string
	var webhook *WebhookResultStoreConfig

	if config != nil {
		if whConfig, ok := config.(*WebhookResultStoreConfig); ok {
			webhook = whConfig
			if whConfig.Endpoint != "" && wrs.allowDynamicConfig {
				endpoint = whConfig.Endpoint
			} else if whConfig.Endpoint != "" && !wrs.allowDynamicConfig {
				slog.Warn("Dynamic webhook configuration is disabled. Ignoring custom endpoint.")
			}
		}
	}

	// If no dynamic endpoint, fall back to default
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
	result := NewResult(run, res, webhook.Data)

	payload := struct {
		Action string `json:"action"`
		*Result
	}{
		Action: "collector.response",
		Result: result,
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
