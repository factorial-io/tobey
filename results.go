// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"net/url"
	"runtime"
	"tobey/internal/collector"
)

// Result represents a crawl result that can be stored by any ResultsStore implementation
type Result struct {
	Run                string      `json:"run_uuid"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when marshalled
	ResponseStatusCode int         `json:"response_status_code"`
	Data               interface{} `json:"data,omitempty"` // Optional additional data
}

// NewResult creates a Result from a collector.Response and optional data
func NewResult(run string, res *collector.Response, data interface{}) *Result {
	return &Result{
		Run:                run,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
		Data:               data,
	}
}

// ResultStore defines how crawl results are stored
type ResultStore interface {
	Save(ctx context.Context, config ResultStoreConfig, run string, res *collector.Response) error
}

// ResultStoreConfig is the base configuration interface that all result store configs must implement
type ResultStoreConfig interface {
	Validate() error
}

// CreateResultStore creates a ResultsStore based on the provided DSN
func CreateResultStore(dsn string) (ResultStore, error) {
	if dsn == "" {
		return &NoopResultStore{}, nil
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid results DSN: %w", err)
	}

	switch u.Scheme {
	case "disk":
		path := u.Path
		if runtime.GOOS == "windows" && len(path) > 0 && path[0] == '/' {
			path = path[1:] // Remove leading slash on Windows
		}
		config := DiskStoreConfig{
			OutputDir: path,
		}
		store, err := NewDiskResultStore(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create disk store: %w", err)
		}
		return store, nil
	case "webhook":
		if u.Host == "" {
			return nil, fmt.Errorf("webhook results store requires a valid host (e.g., webhook://example.com/results)")
		}
		endpoint := fmt.Sprintf("%s://%s%s", "https", u.Host, u.Path)
		return NewWebhookResultStore(context.Background(), endpoint), nil
	case "noop":
		return &NoopResultStore{}, nil
	default:
		return nil, fmt.Errorf("unsupported results store type: %s", u.Scheme)
	}
}
