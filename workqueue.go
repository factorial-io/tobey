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

	PublishURL(runID uint32, url string, whconf *WebhookConfig) error
	ConsumeVisit() (<-chan *VisitMessage, <-chan error)
	DelayVisit(delay time.Duration, msg *VisitMessage) error

	Close() error
}

type VisitMessage struct {
	ID    uint32
	RunID uint32

	URL           string
	WebhookConfig *WebhookConfig

	Created time.Time
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

type MemoryWorkQueue struct {
	// TODO: MaxSize
	msgs chan *VisitMessage
}

func (wq *MemoryWorkQueue) Open() error {
	wq.msgs = make(chan *VisitMessage, 10000) // make sends non-blocking
	return nil
}

func (wq *MemoryWorkQueue) PublishURL(runID uint32, url string, whconf *WebhookConfig) error {
	// TODO: Use select in case we don't have a receiver yet (than this is blocking).
	wq.msgs <- &VisitMessage{
		ID:            uuid.New().ID(),
		RunID:         runID,
		Created:       time.Now(),
		URL:           url,
		WebhookConfig: whconf,
	}
	return nil
}

// DelayVisit republishes a message with given delay.
func (wq *MemoryWorkQueue) DelayVisit(delay time.Duration, msg *VisitMessage) error {
	go func() {
		log.Printf("Delaying message (%d) by %.2f s", msg.ID, delay.Seconds())
		time.Sleep(delay)
		wq.msgs <- msg
	}()
	return nil
}

func (wq *MemoryWorkQueue) ConsumeVisit() (<-chan *VisitMessage, <-chan error) {
	return wq.msgs, nil
}

func (wq *MemoryWorkQueue) Close() error {
	close(wq.msgs)
	return nil
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
func (wq *RabbitMQWorkQueue) DelayVisit(delay time.Duration, msg *VisitMessage) error {
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

func (wq *RabbitMQWorkQueue) PublishURL(runID uint32, url string, whconf *WebhookConfig) error {
	msg := &VisitMessage{
		ID:            uuid.New().ID(),
		Created:       time.Now(),
		RunID:         runID,
		URL:           url,
		WebhookConfig: whconf,
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

func (wq *RabbitMQWorkQueue) ConsumeVisit() (<-chan *VisitMessage, <-chan error) {
	reschan := make(chan *VisitMessage)
	errchan := make(chan error)

	go func() {
		var msg *VisitMessage
		rawmsg := <-wq.receive // Blocks until we have at least one message.

		if err := json.Unmarshal(rawmsg.Body, &msg); err != nil {
			errchan <- err
		} else {
			reschan <- msg
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
