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
	amqp "github.com/rabbitmq/amqp091-go"
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

func maybeRabbitMQ(ctx context.Context) (*amqp.Connection, error) {
	dsn, ok := os.LookupEnv("TOBEY_RABBITMQ_DSN")
	if !ok || dsn == "" {
		return nil, nil
	}
	slog.Debug("Connecting to RabbitMQ...", "dsn", dsn)

	client, err := backoff.RetryNotifyWithData(
		func() (*amqp.Connection, error) {
			return amqp.Dial(dsn)
		},
		backoff.WithContext(backoff.NewExponentialBackOff(), ctx),
		func(err error, t time.Duration) {
			slog.Info("Retrying RabbitMQ connection...", "error", err)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("ultimately failed retrying RabitMQ connection: %w", err)
	}

	slog.Debug("Connection to RabbitMQ established :)")
	return client, nil

}
