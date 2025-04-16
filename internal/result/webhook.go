// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

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

type webhookResult struct {
	Run         string      `json:"run_uuid"`
	RunMetadata interface{} `json:"run_metadata,omitempty"`
	// DiscoveredBy       []DiscoverySource `json:"discovered_by"` // TODO: Implement.
	RequestURL         string `json:"request_url"`
	ResponseBody       []byte `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int    `json:"response_status_code"`
}

// WebhookConfig defines the configuration for webhook endpoints
type WebhookConfig struct {
	Endpoint string `json:"endpoint"`
	Client   *http.Client
}

func NewWebhookConfigFromDSN(dsn string, client *http.Client) (WebhookConfig, error) {
	config := WebhookConfig{
		Client: client,
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid webhook endpoint: %w", err)
	}

	config.Endpoint = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)

	return config, nil
}

func ReportToWebhook(ctx context.Context, config WebhookConfig, runID string, res *collector.Response) error {
	logger := slog.With("run", runID, "url", res.Request.URL)

	logger.Debug("Result Reporter: Forwarding result to webhook ...")

	result := &webhookResult{
		Run:                runID,
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
		return err
	}
	buf := bytes.NewBuffer(body)

	req, err := http.NewRequestWithContext(ctx, "POST", config.Endpoint, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	go func() {
		res, err := config.Client.Do(req)
		if err != nil {
			logger.Error("Result Reporter: Failed to send webhook.", "error", err)
			return
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			logger.Error("Result Reporter: Webhook was not accepted.", "status", res.Status)
			return
		}
	}()

	return nil
}
