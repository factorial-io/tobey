package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
)

type WorkQueue interface {
	Open() error

	PublishURL(ctx context.Context, reqID string, url string, cconf *CollectorConfig, whconf *WebhookConfig) error
	ConsumeVisit() (<-chan *VisitMessagePackage, <-chan error)
	DelayVisit(delay time.Duration, msg *VisitMessagePackage) error

	Close() error
}

type VisitMessagePackage struct {
	context *context.Context
	payload *VisitMessage
}

type VisitMessage struct {
	ID              uint32
	Created         time.Time
	CrawlRequestID  string
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

// DelayVisit republishes a message with given delay.
// Relies on: https://blog.rabbitmq.com/posts/2015/04/scheduling-messages-with-rabbitmq/
func (wq *RabbitMQWorkQueue) DelayVisit(delay time.Duration, msg *VisitMessagePackage) error {
	log.Printf("Delaying message (%d) by %.2f s", msg.payload.ID, delay.Seconds())

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	table := make(amqp.Table)
	otel.GetTextMapPropagator().Inject(context.TODO(), MapCarrierRabbitmq(table))
	table["x-delay"] = delay.Milliseconds()

	return wq.channel.Publish(
		"tobey.default", // exchange
		wq.queue.Name,   // routing key
		false,           // mandatory TODO: check meaning
		false,           // immediate TODO: check meaning
		amqp.Publishing{
			DeliveryMode: amqp.Persistent, // TODO: check meaning
			ContentType:  "application/json",
			Body:         b,
			Headers:      table,
		},
	)
}

func (wq *RabbitMQWorkQueue) PublishURL(ctx context.Context, reqID string, url string, cconf *CollectorConfig, whconf *WebhookConfig) error {
	msg := &VisitMessage{
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

	table := make(amqp.Table)
	otel.GetTextMapPropagator().Inject(ctx, MapCarrierRabbitmq(table))

	return wq.channel.Publish(
		"tobey.default", // exchange
		wq.queue.Name,   // routing key
		false,           // mandatory TODO: check meaning
		false,           // immediate TODO: check meaning
		amqp.Publishing{
			Headers:      table,
			DeliveryMode: amqp.Persistent, // TODO: check meaning
			ContentType:  "application/json",
			Body:         b,
		},
	)
}

func (wq *RabbitMQWorkQueue) ConsumeVisit() (<-chan *VisitMessagePackage, <-chan error) {
	reschan := make(chan *VisitMessagePackage)
	errchan := make(chan error)

	go func() {
		var msg *VisitMessage
		rawmsg := <-wq.receive // Blocks until we have at least one message.
		ctx := otel.GetTextMapPropagator().Extract(context.TODO(), MapCarrierRabbitmq(rawmsg.Headers))
		if err := json.Unmarshal(rawmsg.Body, &msg); err != nil {
			errchan <- err
		} else {
			package_visit := &VisitMessagePackage{
				context: &ctx,
				payload: msg,
			}
			reschan <- package_visit
		}
	}()

	return reschan, errchan
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
	msgs chan *VisitMessagePackage
}

func (wq *MemoryWorkQueue) Open() error {
	wq.msgs = make(chan *VisitMessagePackage, 10000) // make sends non-blocking
	return nil
}

func (wq *MemoryWorkQueue) PublishURL(ctx context.Context, reqID string, url string, cconf *CollectorConfig, whconf *WebhookConfig) error {
	// TODO: Use select in case we don't have a receiver yet (than this is blocking).
	// TODO marvin take a look at the trace implementation.
	ctx_test := context.TODO()
	wq.msgs <- &VisitMessagePackage{
		context: &ctx_test,
		payload: &VisitMessage{
			ID:              uuid.New().ID(),
			Created:         time.Now(),
			CrawlRequestID:  reqID,
			URL:             url,
			CollectorConfig: cconf,
			WebhookConfig:   whconf,
		}}
	return nil
}

// DelayVisit republishes a message with given delay.
func (wq *MemoryWorkQueue) DelayVisit(delay time.Duration, msg *VisitMessagePackage) error {
	go func() {
		log.Printf("Delaying message (%d) by %.2f s", msg.payload.ID, delay.Seconds())
		time.Sleep(delay)
		wq.msgs <- msg
	}()
	return nil
}

func (wq *MemoryWorkQueue) ConsumeVisit() (<-chan *VisitMessagePackage, <-chan error) {
	return wq.msgs, nil
}

func (wq *MemoryWorkQueue) Close() error {
	close(wq.msgs)
	return nil
}

// Transformer for Opentelemetry

// medium for propagated key-value pairs.
type MapCarrierRabbitmq map[string]interface{}

// Get returns the value associated with the passed key.
func (c MapCarrierRabbitmq) Get(key string) string {
	return fmt.Sprintf("%v", c[key])
}

// Set stores the key-value pair.
func (c MapCarrierRabbitmq) Set(key, value string) {
	c[key] = value
}

// Keys lists the keys stored in this carrier.
func (c MapCarrierRabbitmq) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
