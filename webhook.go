package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"tobey/internal/collector"

	"github.com/cenkalti/backoff/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

type WebhookConfig struct {
	Endpoint string      `json:"endpoint"`
	Data     interface{} `json:"data"` // Accept arbitrary data here.
}

// Have a Package to handle metadata.
type WebhookPayloadPackage struct {
	ctx     context.Context
	payload WebhookPayload
}

// The messages that should go over the wire.
type WebhookPayload struct {
	Action string         `json:"action"`
	Data   *WebhookConfig `json:"data"` // Pass through arbitrary data here.
	// TODO: Figure out if we want to use "Standard Webhook" and/or if
	// we than want to nest all results data under Data as to prevent
	// collisions with Action and other fields.
	// TODO Talk about the interface variation
	RequestURL   string `json:"request_url"`
	ResponseBody []byte `json:"response_body"` // Will be base64 encoded once marshalled.
}

var (
	tracer              = otel.Tracer("call.webhook")
	meter               = otel.Meter("call.webhook")
	numbe_of_exceptions metric.Int64Counter
)

type ProcessWebhooksManager struct {
	client *http.Client
}

func NewProcessWebhooksManager() *ProcessWebhooksManager {
	return &ProcessWebhooksManager{
		&http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
	}
}

func (w ProcessWebhooksManager) startHandle(ctx context.Context, webhookQueue chan WebhookPayloadPackage, pnumber int) {
	wlogger := slog.With("worker.id", pnumber)
	// todo handle empty buffered queue
	for {
		select {
		case <-ctx.Done():
			wlogger.Debug("Close worker")
			// The context is over, stop processing results
			return
		case result_package, ok1 := <-webhookQueue:

			// This context is dynamic because of the different items.
			if result_package.ctx == nil {
				result_package.ctx = context.Background()
			}

			// Start the tracing
			ctx_fresh, parentSpan := tracer.Start(result_package.ctx, "handle.webhook.queue.worker")
			result := result_package.payload

			parentSpan.SetAttributes(attribute.Int("worker", pnumber))
			if !ok1 {
				parentSpan.SetAttributes(attribute.Int("worker", pnumber))
				parentSpan.RecordError(errors.New("channel is closed"))
				parentSpan.End()
				return
			}

			if result.RequestURL == "" {
				wlogger.Error("url empty")
				parentSpan.SetAttributes(attribute.Int("worker", pnumber))
				parentSpan.RecordError(errors.New("URL is empty on page"))
				parentSpan.End()
				continue
			}

			err := backoff.RetryNotify(func() error {
				err := w.sendWebhook(ctx_fresh, result, result.Data.Endpoint, "")
				return err
			}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx), func(err error, t time.Duration) {
				wlogger.Info("Retrying to send webhook in", t, err)
			})

			if err != nil {
				wlogger.Info("Sending webhook ultimately failed.", "error", err)
			} else {
				wlogger.Info("Webhook succesfully sent.", "url", result.RequestURL)
			}

			parentSpan.End()
		}
	}
}

func (w *ProcessWebhooksManager) sendWebhook(ctx context.Context, data WebhookPayload, url string, webhookId string) error {
	logger := slog.With("Path", "progress::sendProgressUpdate")

	ctx_send_webhook, span := tracer.Start(ctx, "handle.webhook.queue.send")
	defer span.End()

	// Marshal the data into JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		logger.Error("json marshal failed")
		span.SetStatus(codes.Error, "json marshal failed")
		span.SetAttributes(attribute.String("data", "TODO"))
		span.RecordError(err)
		return err
	}

	// Prepare the webhook request
	span.SetAttributes(attribute.String("webhook_url", url))
	span.SetAttributes(attribute.String("request_url", data.RequestURL))
	req, err := http.NewRequestWithContext(ctx_send_webhook, "POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		logger.Error("Cant create request")
		span.SetStatus(codes.Error, "cant create new request")
		span.RecordError(err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	// Send the webhook request
	resp, err := w.client.Do(req)
	if err != nil {
		logger.Error("Cant do request")
		span.SetStatus(codes.Error, "Request failed")
		span.SetAttributes(attribute.String("url", resp.Status))
		span.RecordError(err)
		return err
	}

	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			span.SetStatus(codes.Error, "operationThatCouldFail failed")
			span.RecordError(err)
			logger.Error("Error closing response body.", "error", err)
		}
	}(resp.Body)
	span.SetAttributes(attribute.String("status", resp.Status))
	// Determine the status based on the response code
	status := "failed"
	if resp.StatusCode == http.StatusOK {
		status = "delivered"
	}

	logger.Debug("Current status changed.", "status", status)

	if status == "failed" {
		var body_bytes []byte
		resp.Body.Read(body_bytes)
		span.SetAttributes(attribute.String("Body", string(body_bytes[:])))
		span.SetStatus(codes.Error, "operationThatCouldFail failed")
		span.RecordError(err)
		return errors.New(status)
	}

	return nil
}

func (w *ProcessWebhooksManager) Start(ctx context.Context, webhookQueue chan WebhookPayloadPackage) {
	//todo add recovery
	go func(ctx context.Context, webhookQueue chan WebhookPayloadPackage) {
		count := GetEnvInt("TOBEY_WEBHOOK_WORKER", 4)
		for i := 0; i < count; i++ {
			go w.startHandle(ctx, webhookQueue, i)
		}
	}(ctx, webhookQueue)
}

type WebhookDispatcher struct {
	webhookQueue chan WebhookPayloadPackage
}

func NewWebhookDispatcher(webhookQueue chan WebhookPayloadPackage) *WebhookDispatcher {
	return &WebhookDispatcher{
		webhookQueue: webhookQueue,
	}
}

func (wd *WebhookDispatcher) Send(ctx context.Context, webhook *WebhookConfig, res *collector.Response) error {
	webhook_package := WebhookPayloadPackage{
		ctx: ctx,
		payload: WebhookPayload{
			Action: "collector.response",

			// We pass through the data we received taking in the
			// initial crawl request, verbatim.
			Data: webhook,

			RequestURL:   res.Request.URL.String(),
			ResponseBody: res.Body[:],
		},
	}

	wd.webhookQueue <- webhook_package

	return nil
}
