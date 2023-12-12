package main

import (
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

type WorkQueue interface {
	Open() error
	PublishURL(s *Site, url string) error
	Consume() (*WorkQueueMessage, error)
	Close() error
}

type WorkQueueMessage struct {
	Site *Site  `json:"site"`
	URL  string `json:"url"`
}

func CreateWorkQueue(rabbitmq *amqp.Connection) WorkQueue {
	if rabbitmq != nil {
		return &RabbitMQWorkQueue{conn: rabbitmq}
	} else {
		return &MemoryWorkQueue{}
	}
}

type RabbitMQWorkQueue struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
	receive <-chan amqp.Delivery
}

func (wq *RabbitMQWorkQueue) Open() error {
	ch, err := wq.conn.Channel()
	if err != nil {
		return err
	}
	wq.channel = ch

	q, err := ch.QueueDeclare(
		"tobey", // name
		true,    // durable TODO: check meaning
		false,   // delete when unused
		false,   // exclusive TODO: check meaning
		false,   // no-wait TODO: check meaning
		nil,     // arguments
	)
	if err != nil {
		return err
	}
	wq.queue = q

	receive, err := wq.channel.Consume(
		wq.queue.Name, // queue
		"",            // consumer
		true,          // auto-ack
		false,         // exclusive
		false,         // no-local
		false,         // no-wait
		nil,           // args
	)
	if err != nil {
		return err
	}
	wq.receive = receive

	return nil
}

func (wq *RabbitMQWorkQueue) PublishURL(s *Site, url string) error {
	msg := &WorkQueueMessage{
		Site: s,
		URL:  url,
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return wq.channel.Publish(
		"",            // exchange
		wq.queue.Name, // routing key
		false,         // mandatory TODO: check meaning
		false,         // immediate TODO: check meaning
		amqp.Publishing{
			DeliveryMode: amqp.Persistent, // TODO: check meaning
			ContentType:  "application/json",
			Body:         b,
		},
	)
}

func (wq *RabbitMQWorkQueue) Consume() (*WorkQueueMessage, error) {
	var msg *WorkQueueMessage

	rawmsg := <-wq.receive // Blocks until we have at least one message.

	if err := json.Unmarshal(rawmsg.Body, &msg); err != nil {
		return msg, err
	}
	return msg, nil
}

func (wq *RabbitMQWorkQueue) Close() error {
	var lasterr error

	if err := wq.channel.Close(); err != nil {
		lasterr = err
	}
	if err := wq.conn.Close(); err != nil {
		lasterr = err
	}
	return lasterr
}

type MemoryWorkQueue struct {
	// TODO: MaxSize
	msgs chan *WorkQueueMessage
}

func (wq *MemoryWorkQueue) Open() error {
	wq.msgs = make(chan *WorkQueueMessage, 10000) // make sends non-blocking
	return nil
}

func (wq *MemoryWorkQueue) PublishURL(s *Site, url string) error {
	// TODO: Use select in case we don't have a receiver yet (than this is blocking).
	wq.msgs <- &WorkQueueMessage{
		Site: s,
		URL:  url,
	}
	return nil
}

func (wq *MemoryWorkQueue) Consume() (*WorkQueueMessage, error) {
	return <-wq.msgs, nil
}

func (wq *MemoryWorkQueue) Close() error {
	close(wq.msgs)
	return nil
}
