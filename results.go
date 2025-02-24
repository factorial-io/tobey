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
	"tobey/internal/collector"
)

func CreateResultReporter(dsn string) (ResultReporter, error) {
	if dsn == "" {
		slog.Info("Result Reporter: Enabling, using disk reporter", "dsn", dsn)
		config := DiskResultReporterConfig{
			OutputDir: "results", // Relative to the current working directory.
		}
		return NewDiskResultReporter(config)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid results DSN: %w", err)
	}

	switch u.Scheme {
	case "disk":
		slog.Info("Result Reporter: Enabling, using disk reporter", "dsn", dsn)
		config, err := newDiskResultReporterConfigFromDSN(dsn)
		if err != nil {
			return nil, err
		}
		return NewDiskResultReporter(config)
	case "webhook":
		slog.Info("Result Reporter: Enabling, using webhook reporter", "dsn", dsn)

		config, err := newWebhookResultReporterConfigFromDSN(dsn)
		if err != nil {
			return nil, err
		}
		return NewWebhookResultReporter(context.Background(), config)
	case "noop":
		slog.Info("Result Reporter: Disabling, using noop reporter")
		return &NoopResultReporter{}, nil
	default:
		return nil, fmt.Errorf("unsupported results store type: %s", u.Scheme)
	}
}

type ResultReporter interface {
	Accept(ctx context.Context, config any, run *Run, res *collector.Response) error
}
