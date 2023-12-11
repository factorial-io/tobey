package main

import (
	"log"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/streadway/amqp"
)

type WorkQueue interface {
	Add() error
	Get() error
	Close() error
}

func NewRabbitMQWorkQueue(dsn string) (*RabbitMQWorkQueue, error) {
	return backoff.RetryNotifyWithData(func() (*RabbitMQWorkQueue, error) {
		conn, err := amqp.Dial(dsn)

		return &RabbitMQWorkQueue{conn}, err
	}, backoff.NewExponentialBackOff(), func(err error, t time.Duration) {
		log.Print(err)
	})
}

type RabbitMQWorkQueue struct {
	conn *amqp.Connection
}

func (q *RabbitMQWorkQueue) Add() error {
	return nil
}

func (q *RabbitMQWorkQueue) Get() error {
	return nil
}

func (q *RabbitMQWorkQueue) Close() error {
	return q.conn.Close()
}

func NewMemoryWorkQueue() (*MemoryWorkQueue, error) {
	return &MemoryWorkQueue{}, nil
}

type MemoryWorkQueue struct{}

func (q *MemoryWorkQueue) Add() error {
	return nil
}

func (q *MemoryWorkQueue) Get() error {
	return nil
}

func (q *MemoryWorkQueue) Close() error {
	return nil
}
