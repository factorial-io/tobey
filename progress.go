package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
	"tobey/helper"

	"github.com/cenkalti/backoff/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	PROGRESS_STATE_UKNOWN                = "unknown"
	PROGRESS_STATE_QUEUED_FOR_CRAWLING   = "queued_for_crawling"
	PROGRESS_STATE_CRAWLED               = "crawled"
	PROGRESS_STATE_QUEUED_FOR_Processing = "queued_for_processing"
	PROGRESS_STATE_Processing_Started    = "processing_started"
	PROGRESS_STATE_Processed             = "processed"
	PROGRESS_STATE_Succeeded             = "succeeded"
	PROGRESS_STATE_Cancelled             = "cancelled"
	PROGRESS_STATE_Errored               = "errored"

	PROGRESS_STAGE_NAME = "spider"

	PROGRESS_ENDPOINTS_UPDATE     = "api/status/update"
	PROGRESS_ENDPOINTS_TRANSITION = "api/status/transition"
)

type ProgressUpdateMessagePackage struct {
	ctx     context.Context
	payload ProgressUpdateMessage
}

type ProgressUpdateMessage struct {
	Stage  string `json:"stage"`
	Status string `json:"status"`   // only constanz allowed
	Run    uint32 `json:"run_uuid"` // uuid of the run
	Url    string `json:"url"`
}

type ProgressManager struct {
	api_url string
	client  *http.Client
}

func MustStartProgressFromEnv(ctx context.Context) Progress {
	if dsn := os.Getenv("TOBEY_PROGRESS_DSN"); dsn != "" {
		slog.Info("Using progress service for updates.", "dsn", dsn)

		queue := make(chan ProgressUpdateMessagePackage, helper.GetEnvInt("TORBEY_PROGRESS_PAYLOAD_LIMIT", 100))
		progress_manager := NewProgressManager()
		progress_manager.Start(ctx, queue)

		return &BaseProgress{
			queue,
		}
	} else {
		slog.Debug("Not sharing progress updates.")
		return &NoopProgress{}
	}
}

func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		helper.GetEnvString("TOBEY_PROGRESS_DSN", "http://progress:9020"),
		&http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
	}
}

type Progress interface {
	Update(update_message ProgressUpdateMessagePackage) error
	Close() error
}

func (w *ProgressManager) startHandle(ctx context.Context, progressQueue chan ProgressUpdateMessagePackage, pnumber int) {
	wlogger := slog.With("worker.id", pnumber)

	// todo handle empty buffered queue
	for {
		select {
		case <-ctx.Done():
			wlogger.Debug("Close worker")
			// The context is over, stop processing results
			return
		case result_package, ok1 := <-progressQueue:

			// This context is dynamic because of the different items.
			if result_package.ctx == nil {
				result_package.ctx = context.Background()
			}

			// Start the tracing
			ctx_fresh, parentSpan := tracer.Start(result_package.ctx, "handle.progress.queue.worker")
			result := result_package.payload

			parentSpan.SetAttributes(attribute.Int("worker", pnumber))
			if !ok1 {
				parentSpan.SetAttributes(attribute.Int("worker", pnumber))
				parentSpan.RecordError(errors.New("channel is closed"))
				parentSpan.End()
				return
			}

			err := backoff.RetryNotify(func() error {
				err := w.sendProgressUpdate(ctx_fresh, result)
				return err
			}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx), func(err error, t time.Duration) {
				wlogger.Info("Retrying to send progress.", "in", t, "error", err)
			})

			if err != nil {
				wlogger.Error("Sending progress ultimately failed.", "error", err)
			} else {
				wlogger.Debug("Progress succesfully sent.", "url", result.Url)
			}

			parentSpan.End()
		}
	}
}

func (w *ProgressManager) sendProgressUpdate(ctx context.Context, msg ProgressUpdateMessage) error {
	logger := slog.With("Path", "progress::sendProgressUpdate")
	ctx_send_webhook, span := tracer.Start(ctx, "handle.progress.queue.send")
	defer span.End()

	// Marshal the data into JSON
	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		logger.Error("Cant create request")
		span.SetStatus(codes.Error, "json marshal failed")
		span.SetAttributes(attribute.String("data", "TODO"))
		span.RecordError(err)
		return err
	}

	api_request_url := fmt.Sprintf("%v/%v", w.api_url, PROGRESS_ENDPOINTS_UPDATE)
	span.SetAttributes(attribute.String("API_URL", api_request_url))
	span.SetAttributes(attribute.String("url", msg.Url))
	span.SetAttributes(attribute.String("status_update", msg.Status))
	// Prepare the webhook request
	req, err := http.NewRequestWithContext(ctx_send_webhook, "POST", api_request_url, bytes.NewBuffer(jsonBytes))
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

	span.SetAttributes(attribute.Int("StatusCode", resp.StatusCode))
	// Determine the status based on the response code
	status := "failed"
	if resp.StatusCode == http.StatusAccepted {
		status = "delivered"
	}

	logger.Debug("The current status changed.", "status", status)

	if status == "failed" {
		var body_bytes []byte
		resp.Body.Read(body_bytes)
		span.SetAttributes(attribute.String("Body", string(body_bytes[:])))
		span.SetStatus(codes.Error, "Wrong response code")
		span.RecordError(errors.New(status))
		return errors.New(status)
	}

	return nil
}

func (w *ProgressManager) Start(ctx context.Context, progressQueue chan ProgressUpdateMessagePackage) {
	//todo add recovery
	go func(ctx context.Context, progressQueue chan ProgressUpdateMessagePackage) {
		count := helper.GetEnvInt("TOBEY_PROGRESS_WORKER", 4)
		for i := 0; i < count; i++ {
			go w.startHandle(ctx, progressQueue, i)
		}
	}(ctx, progressQueue)
}

type NoopProgress struct {
}

func (p *NoopProgress) Update(update_message ProgressUpdateMessagePackage) error {
	return nil
}

func (p *NoopProgress) Close() error {
	return nil
}

type BaseProgress struct {
	progressQueue chan ProgressUpdateMessagePackage
}

func (p *BaseProgress) Update(update_message ProgressUpdateMessagePackage) error {
	p.progressQueue <- update_message
	return nil
}

func (p *BaseProgress) Close() error {
	close(p.progressQueue)
	return nil
}
