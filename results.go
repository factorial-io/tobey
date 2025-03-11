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

type DiscoverySource string

const (
	DiscoverySourceSitemap DiscoverySource = "sitemap"
	DiscoverySourceRobots  DiscoverySource = "robots"
	DiscoverySourceLink    DiscoverySource = "link"
)

// ResultReporter is a function type that can be used to report the result of a crawl. It comes
// with a preconfigured config.
type ResultReporter func(ctx context.Context, run *Run, res *collector.Response) error

// CreateResultReporter creates a ResultReporter from a DSN.
func CreateResultReporter(ctx context.Context, dsn string, run *Run, res *collector.Response) (ResultReporter, error) {
	if dsn == "" {
		slog.Debug("Result Reporter: Using disk reporter", "dsn", dsn)
		config, err := newDiskResultReporterConfigFromDSN(dsn)

		return func(ctx context.Context, run *Run, res *collector.Response) error {
			return ReportResultToDisk(ctx, config, run, res)
		}, err
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid result reporter DSN: %w", err)
	}

	switch u.Scheme {
	case "disk":
		slog.Debug("Result Reporter: Using disk reporter", "dsn", dsn)
		config, err := newDiskResultReporterConfigFromDSN(dsn)

		return func(ctx context.Context, run *Run, res *collector.Response) error {
			return ReportResultToDisk(ctx, config, run, res)
		}, err
	case "webhook":
		slog.Debug("Result Reporter: Using webhook reporter", "dsn", dsn)
		config, err := newWebhookResultReporterConfigFromDSN(dsn)

		return func(ctx context.Context, run *Run, res *collector.Response) error {
			return ReportResultToWebhook(ctx, config, run, res)
		}, err
	case "s3":
		slog.Debug("Result Reporter: Using s3 reporter", "dsn", dsn)
		config, err := newS3ResultReporterConfigFromDSN(dsn)

		return func(ctx context.Context, run *Run, res *collector.Response) error {
			return ReportResultToS3(ctx, config, run, res)
		}, err
	case "noop":
		slog.Debug("Result Reporter: Disabled, not reporting results")
		return func(ctx context.Context, run *Run, res *collector.Response) error {
			return ReportResultToNoop(ctx, nil, run, res)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported result reporter type: %s", u.Scheme)
	}
}
