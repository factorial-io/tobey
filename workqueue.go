package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type WorkQueue interface {
	Open() error

	// The following methods use the crawl run context.
	PublishURL(ctx context.Context, run string, url string, cconf *CollectorConfig, hconf *WebhookConfig, flags uint8) error
	ConsumeVisit(ctx context.Context) (<-chan *VisitJob, <-chan error)
	DelayVisit(ctx context.Context, delay time.Duration, j *VisitMessage) error

	Close() error
}

type VisitMessage struct {
	ID  uint32
	Run string

	URL string

	CollectorConfig *CollectorConfig
	WebhookConfig   *WebhookConfig

	// Whether this visit has a valid reservation by a rate limiter.
	HasReservation bool

	Flags   uint8
	Created time.Time
}

// visitPackage is used by the in-memory implementation of the WorkQueue.
// Implementations that have a built-in mechanism to transport headers do not
// need to use this.
type visitPackage struct {
	Carrier propagation.MapCarrier
	Message *VisitMessage
}

// VisitJob is similar to a http.Request, it exists only for a certain time. It
// carries its own Context. And although this violates the strict variant of the
// "do not store context on struct" it doe not violate the relaxed "do not store
// a context" rule, as a Job is transitive.
//
// We initially saw the requirement to pass a context here as we wanted to carry
// over TraceID and SpanID from the job publisher.
type VisitJob struct {
	*VisitMessage
	Context context.Context
}

// Validate ensures mandatory fields are non-zero.
func (j *VisitJob) Validate() (bool, error) {
	if j.Run == "" {
		return false, errors.New("job without run")
	}
	if j.URL == "" {
		return false, errors.New("job without URL")
	}
	return true, nil
}

func CreateWorkQueue(rabbitmq *amqp.Connection) WorkQueue {
	if rabbitmq != nil {
		slog.Debug("Using distributed work queue...")
		return &RabbitMQWorkQueue{conn: rabbitmq}
	} else {
		slog.Debug("Using in-memory work queue...")
		return &MemoryWorkQueue{}
	}
}

type MemoryWorkQueue struct {
	// TODO: MaxSize
	pkgs chan *visitPackage
}

func (wq *MemoryWorkQueue) Open() error {
	wq.pkgs = make(chan *visitPackage, 10000) // TODO: make sends non-blocking
	return nil
}

func (wq *MemoryWorkQueue) PublishURL(ctx context.Context, run string, url string, cconf *CollectorConfig, hconf *WebhookConfig, flags uint8) error {
	// TODO: Use select in case we don't have a receiver yet (than this is blocking).

	// Extract tracing information from context, to transport it over the
	// channel, without using a Context.
	propagator := otel.GetTextMapPropagator()
	carrier := propagation.MapCarrier{}
	propagator.Inject(ctx, carrier)

	wq.pkgs <- &visitPackage{
		Carrier: carrier,
		Message: &VisitMessage{
			ID:              uuid.New().ID(),
			Created:         time.Now(),
			Run:             run,
			URL:             url,
			CollectorConfig: cconf,
			WebhookConfig:   hconf,
			Flags:           flags,
		},
	}
	return nil
}

// DelayVisit republishes a message with given delay.
func (wq *MemoryWorkQueue) DelayVisit(ctx context.Context, delay time.Duration, msg *VisitMessage) error {
	go func() {
		slog.Debug("Delaying message", "msg.id", msg.ID, "delay", delay.Seconds())

		// Extract tracing information from context, to transport it over the
		// channel, without using a Context.
		propagator := otel.GetTextMapPropagator()
		carrier := propagation.MapCarrier{}
		propagator.Inject(ctx, carrier)

		time.Sleep(delay)
		wq.pkgs <- &visitPackage{
			Carrier: carrier,
			Message: msg,
		}
		slog.Debug("Delayed message accepted.", "msg.id", msg.ID, "delay", delay.Seconds())
	}()
	return nil
}

func (wq *MemoryWorkQueue) ConsumeVisit(ctx context.Context) (<-chan *VisitJob, <-chan error) {
	reschan := make(chan *VisitJob)
	errchan := make(chan error)

	go func() {
		select {
		case <-ctx.Done():
			slog.Debug("Consume context cancelled, closing channels.")

			close(reschan)
			close(errchan)
			return
		case p := <-wq.pkgs:
			slog.Debug("Received message, forwarding to results channel.", "msg.id", p.Message.ID)

			// Initializes the context for the job. Than extract the tracing
			// information from the carrier into the job's context.
			jctx := context.Background()
			jctx = otel.GetTextMapPropagator().Extract(jctx, p.Carrier)

			reschan <- &VisitJob{
				VisitMessage: p.Message,
				Context:      jctx,
			}
			slog.Debug("Forwarded message to results channel.", "msg.id", p.Message.ID)
		}
	}()

	return reschan, errchan
}

func (wq *MemoryWorkQueue) Close() error {
	close(wq.pkgs)
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

func (wq *RabbitMQWorkQueue) PublishURL(ctx context.Context, run string, url string, cconf *CollectorConfig, hconf *WebhookConfig, flags uint8) error {
	jmlctx, span := tracer.Start(ctx, "publish_url")
	defer span.End()
	msg := &VisitMessage{
		ID:              uuid.New().ID(),
		Created:         time.Now(),
		Run:             run,
		URL:             url,
		CollectorConfig: cconf,
		WebhookConfig:   hconf,
		Flags:           flags,
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	table := make(amqp.Table)

	// Add tracing information into the RabbitMQ headers, so that
	// the consumer of the message can continue the trace.
	otel.GetTextMapPropagator().Inject(jmlctx, MapCarrierRabbitmq(table))

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

// DelayVisit republishes a message with given delay.
// Relies on: https://blog.rabbitmq.com/posts/2015/04/scheduling-messages-with-rabbitmq/
func (wq *RabbitMQWorkQueue) DelayVisit(ctx context.Context, delay time.Duration, msg *VisitMessage) error {
	slog.Debug("Delaying message", "msg.id", msg.ID, "delay", delay.Seconds())

	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	table := make(amqp.Table)
	table["x-delay"] = delay.Milliseconds()

	// Extract tracing information from context into headers. The tracing information
	// should already be present in the context of the caller.
	otel.GetTextMapPropagator().Inject(ctx, MapCarrierRabbitmq(table))

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

func (wq *RabbitMQWorkQueue) ConsumeVisit(ctx context.Context) (<-chan *VisitJob, <-chan error) {
	reschan := make(chan *VisitJob)
	errchan := make(chan error)

	go func() {
		var msg *VisitMessage
		var rawmsg amqp.Delivery

		select {
		case v := <-wq.receive: // Blocks until we have at least one message.
			rawmsg = v
		case <-ctx.Done(): // The worker's context.
			close(reschan)
			close(errchan)
			return // Exit if the context is cancelled.
		}

		if err := json.Unmarshal(rawmsg.Body, &msg); err != nil {
			errchan <- err
		} else {
			// Initializes the context for the job. Than extract the tracing
			// information from the RabbitMQ headers into the job's context.
			jctx := otel.GetTextMapPropagator().Extract(context.Background(), MapCarrierRabbitmq(rawmsg.Headers))

			reschan <- &VisitJob{
				VisitMessage: msg,
				Context:      jctx,
			}
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
