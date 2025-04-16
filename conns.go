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
	"os"
	"strconv"
	"time"
	"tobey/internal/collector"
	"tobey/internal/progress"
	"tobey/internal/result"

	"github.com/cenkalti/backoff/v4"
	"github.com/kos-v/dsnparser"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func maybeRedis(ctx context.Context) (*redis.Client, error) {
	rawdsn, ok := os.LookupEnv("TOBEY_REDIS_DSN")
	if !ok || rawdsn == "" {
		return nil, nil
	}
	slog.Debug("Connecting to Redis...", "dsn", rawdsn)

	dsn := dsnparser.Parse(rawdsn)
	database, _ := strconv.Atoi(dsn.GetPath())

	client, err := backoff.RetryNotifyWithData(
		func() (*redis.Client, error) {
			client := redis.NewClient(&redis.Options{
				Addr:     fmt.Sprintf("%s:%s", dsn.GetHost(), dsn.GetPort()),
				Password: dsn.GetPassword(),
				DB:       database,
			})
			_, err := client.Ping(ctx).Result()
			return client, err
		},
		backoff.WithContext(backoff.NewExponentialBackOff(), ctx),
		func(err error, t time.Duration) {
			slog.Info("Retrying redis connection.", "error", err)
		},
	)

	if err != nil {
		return nil, fmt.Errorf("ultimately failed retrying redis connection: %w", err)
	}
	slog.Debug("Connection to Redis established :)")

	if UseTracing {
		if err := redisotel.InstrumentTracing(client); err != nil {
			return client, err
		}
	}
	if UseMetrics {
		if err := redisotel.InstrumentMetrics(client); err != nil {
			return client, err
		}
	}
	return client, nil
}

var webhookHTTPClient = CreateRetryingHTTPClient(NoAuthFn, UserAgent)

// CreateResultReporter creates a result.Reporter from a DSN.
func CreateResultReporter(ctx context.Context, dsn string, run *Run, res *collector.Response) (result.Reporter, error) {
	if dsn == "" {
		config, err := result.NewDiskConfigFromDSN("disk://results")
		slog.Debug("Result Reporter: Using disk reporter", "config", config, "err", err)

		return func(ctx context.Context, runID string, res *collector.Response) error {
			return result.ReportToDisk(ctx, config, runID, res)
		}, err
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid result reporter DSN: %w", err)
	}

	switch u.Scheme {
	case "disk":
		config, err := result.NewDiskConfigFromDSN(dsn)
		slog.Debug("Result Reporter: Using disk reporter", "config", config)

		return func(ctx context.Context, runID string, res *collector.Response) error {
			return result.ReportToDisk(ctx, config, runID, res)
		}, err
	case "webhook":
		config, err := result.NewWebhookConfigFromDSN(dsn, webhookHTTPClient)
		slog.Debug("Result Reporter: Using webhook reporter", "config", config)

		return func(ctx context.Context, runID string, res *collector.Response) error {
			return result.ReportToWebhook(ctx, config, runID, res)
		}, err
	case "s3", "s3s":
		config, err := result.NewS3ConfigFromDSN(dsn)
		slog.Debug("Result Reporter: Using s3 reporter", "config", config)

		return func(ctx context.Context, runID string, res *collector.Response) error {
			return result.ReportToS3(ctx, config, runID, res)
		}, err
	case "noop":
		slog.Debug("Result Reporter: Disabled, not reporting results")
		return func(ctx context.Context, runID string, res *collector.Response) error {
			return result.ReportToNoop(ctx, nil, runID, res)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported result reporter type: %s", u.Scheme)
	}
}

// CreateProgressReporter creates a new progress dispatcher based on the provided DSN.
// If dsn is empty, it returns a NoopProgressDispatcher.
func CreateProgressReporter(dsn string) (progress.Reporter, error) {
	if dsn == "" {
		slog.Info("Progress Reporter: Disabled, not sharing progress updates.")
		return &progress.NoopReporter{}, nil
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid progress DSN: %w", err)
	}

	switch u.Scheme {
	case "console":
		slog.Info("Progress Reporter: Using Console for progress updates.")
		return &progress.ConsoleReporter{}, nil
	case "factorial", "factorials":
		slog.Info("Progress Reporter: Enabled, using Factorial progress service for updates.", "dsn", dsn)

		var scheme string
		if u.Scheme == "factorials" {
			scheme = "https"
		} else {
			scheme = "http"
		}
		return progress.NewFactorialReporter(
			CreateRetryingHTTPClient(NoAuthFn, UserAgent),
			scheme,
			u.Host,
			tracer,
		), nil
	case "memory":
		slog.Info("Progress Reporter: Using Memory for progress updates.")
		return progress.NewMemoryReporter(), nil
	case "noop":
		slog.Info("Progress Reporter: Disabled, not sharing progress updates.")
		return &progress.NoopReporter{}, nil
	default:
		return nil, fmt.Errorf("unsupported progress dispatcher type: %s", u.Scheme)
	}
}
