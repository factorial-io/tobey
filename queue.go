package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type WorkQueue interface {
	Open() error
	PublishURL(reqID uint32, url string, cconf *CollectorConfig, whconf *WebhookConfig) error
	Consume() (*WorkQueueMessage, error)
	Delay(delay time.Duration, msg *WorkQueueMessage) error
	Close() error
}

type WorkQueueMessage struct {
	ID              uint32
	Created         time.Time
	CrawlRequestID  uint32
	URL             string
	CollectorConfig *CollectorConfig
	WebhookConfig   *WebhookConfig
}

func CreateWorkQueue(rabbitmq *amqp.Connection) WorkQueue {
	if rabbitmq != nil {
		log.Print("Using distributed work queue...")
		return &RabbitMQWorkQueue{conn: rabbitmq}
	} else {
		log.Print("Using in-memory work queue...")
		return &MemoryWorkQueue{}
	}
}

type RabbitMQWorkQueue struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
	receive <-chan amqp.Delivery
}

// Open declares both sides (producer and consumer) of the work queue.
func (wq *RabbitMQWorkQueue) Open() error {
	ch, err := wq.conn.Channel()
	if err != nil {
		return err
	}
	wq.channel = ch

	q, err := ch.QueueDeclare(
		"tobey.urls", // name
		true,         // durable TODO: check meaning
		false,        // delete when unused
		false,        // exclusive TODO: check meaning
		false,        // no-wait TODO: check meaning
		nil,          // arguments
	)
	if err != nil {
		return err
	}
	wq.queue = q

	// This utilizes the delayed_message plugin.
	ch.ExchangeDeclare("tobey.default", "x-delayed-message", true, false, false, false, amqp.Table{
		"x-delayed-type": "direct",
	})

	// Bind queue to delayed exchange.
	err = ch.QueueBind(wq.queue.Name, wq.queue.Name, "tobey.default", false, nil)
	if err != nil {
		return err
	}

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

// Delay republishes a message with given delay.
// Relies on: https://blog.rabbitmq.com/posts/2015/04/scheduling-messages-with-rabbitmq/
func (wq *RabbitMQWorkQueue) Delay(delay time.Duration, msg *WorkQueueMessage) error {
	log.Printf("Delaying message (%d) by %.2f s", msg.ID, delay.Seconds())

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return wq.channel.Publish(
		"tobey.default", // exchange
		wq.queue.Name,   // routing key
		false,           // mandatory TODO: check meaning
		false,           // immediate TODO: check meaning
		amqp.Publishing{
			DeliveryMode: amqp.Persistent, // TODO: check meaning
			ContentType:  "application/json",
			Body:         b,
			Headers: amqp.Table{
				"x-delay": delay.Milliseconds(),
			},
		},
	)
}

func (wq *RabbitMQWorkQueue) PublishURL(reqID uint32, url string, cconf *CollectorConfig, whconf *WebhookConfig) error {
	msg := &WorkQueueMessage{
		ID:              uuid.New().ID(),
		Created:         time.Now(),
		CrawlRequestID:  reqID,
		URL:             url,
		CollectorConfig: cconf,
		WebhookConfig:   whconf,
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return wq.channel.Publish(
		"tobey.default", // exchange
		wq.queue.Name,   // routing key
		false,           // mandatory TODO: check meaning
		false,           // immediate TODO: check meaning
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

func (wq *MemoryWorkQueue) PublishURL(reqID uint32, url string, cconf *CollectorConfig, whconf *WebhookConfig) error {
	// TODO: Use select in case we don't have a receiver yet (than this is blocking).
	wq.msgs <- &WorkQueueMessage{
		ID:              uuid.New().ID(),
		Created:         time.Now(),
		CrawlRequestID:  reqID,
		URL:             url,
		CollectorConfig: cconf,
		WebhookConfig:   whconf,
	}
	return nil
}

// Delay republishes a message with given delay.
func (wq *MemoryWorkQueue) Delay(delay time.Duration, msg *WorkQueueMessage) error {
	go func() {
		log.Printf("Delaying message (%d) by %.2f s", msg.ID, delay.Seconds())
		time.Sleep(delay)
		wq.msgs <- msg
	}()
	return nil
}

func (wq *MemoryWorkQueue) Consume() (*WorkQueueMessage, error) {
	return <-wq.msgs, nil
}

func (wq *MemoryWorkQueue) Close() error {
	close(wq.msgs)
	return nil
}
