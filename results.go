// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"tobey/internal/collector"
)

func CreateResultReporter(dsn string) (ResultReporter, error) {
	if dsn == "" {
		slog.Debug("Result Reporter: Disabled, using noop reporter")
		return &NoopResultReporter{}, nil
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
		config := DiskResultReporterConfig{
			OutputDir: path,
		}
		store, err := NewDiskResultReporter(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create disk store: %w", err)
		}

		slog.Debug("Result Reporter: Enabled, using disk store", "dsn", dsn)
		return store, nil
	case "webhook":
		// Only require host if dynamic config is not enabled
		if u.Host == "" && u.Query().Get("enable_dynamic_config") == "" {
			return nil, fmt.Errorf("webhook results store requires a valid host (e.g., webhook://example.com/results) unless dynamic configuration is enabled")
		}
		endpoint := fmt.Sprintf("%s://%s%s?%s", "https", u.Host, u.Path, u.RawQuery)

		slog.Debug("Result Reporter: Enabled, using webhook reporter", "dsn", dsn)
		return NewWebhookResultReporter(context.Background(), endpoint), nil
	case "noop":

		slog.Debug("Result Reporter: Disabled, using noop reporter")
		return &NoopResultReporter{}, nil
	default:
		return nil, fmt.Errorf("unsupported results store type: %s", u.Scheme)
	}
}

type ResultReporter interface {
	Accept(ctx context.Context, config any, run *Run, res *collector.Response) error
}
