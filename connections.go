package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/kos-v/dsnparser"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func maybeRedis(ctx context.Context) *redis.Client {
	rawdsn, ok := os.LookupEnv("TOBEY_REDIS_DSN")
	if !ok {
		return nil
	}
	log.Printf("Connecting to Redis with DSN (%s)...", rawdsn)

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
			log.Printf("Retrying connection: %s", err)
		},
	)

	if err != nil {
		log.Printf("Ultimately failed retrying: %s", err)
		panic(err)
	}
	log.Print("Connection to Redis established :)")

	// Enable tracing instrumentation.
	if err := redisotel.InstrumentTracing(client); err != nil {
		panic(err)
	}

	// Enable metrics instrumentation.
	if err := redisotel.InstrumentMetrics(client); err != nil {
		panic(err)
	}
	return client
}

func maybeRabbitMQ(ctx context.Context) *amqp.Connection {
	dsn, ok := os.LookupEnv("TOBEY_RABBITMQ_DSN")
	if !ok {
		return nil
	}
	log.Printf("Connecting to RabbitMQ with DSN (%s)...", dsn)

	client, err := backoff.RetryNotifyWithData(
		func() (*amqp.Connection, error) {
			return amqp.Dial(dsn)
		},
		backoff.WithContext(backoff.NewExponentialBackOff(), ctx),
		func(err error, t time.Duration) {
			log.Printf("Retrying connection: %s", err)
		},
	)
	if err != nil {
		log.Printf("Ultimately failed retrying: %s", err)
		panic(err)
	}
	log.Print("Connection to RabbitMQ established :)")
	return client

}
