// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

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
