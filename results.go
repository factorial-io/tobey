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
		// Only require host if dynamic config is not enabled
		if u.Host == "" && u.Query().Get("enable_dynamic_config") == "" {
			return nil, fmt.Errorf("webhook results store requires a valid host (e.g., webhook://example.com/results) unless dynamic configuration is enabled")
		}
		endpoint := fmt.Sprintf("%s://%s%s?%s", "https", u.Host, u.Path, u.RawQuery)
		return NewWebhookResultStore(context.Background(), endpoint), nil
	case "noop":
		return &NoopResultStore{}, nil
	default:
		return nil, fmt.Errorf("unsupported results store type: %s", u.Scheme)
	}
}

type ResultStore interface {
	Save(ctx context.Context, config any, run *Run, res *collector.Response) error
}
