// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	ProgressStage = "crawler"

	ProgressEndpointUpdate     = "api/status/update"
	ProgressEndpointTransition = "api/status/transition-to"
)

// Constants to be used for indicating what state the progress is in.
const (
	ProgressStateQueuedForCrawling = "queued_for_crawling" // Used when an URL has been enqueued, see collector.Collector.EnqueueFn.
	ProgressStateCrawling          = "crawling"            // Used when actively crawling an URL, i.e. right before collector.Collector.Visit.
	ProgressStateCrawled           = "crawled"
	ProgressStateSucceeded         = "succeeded" // When crawling has been successful.
	ProgressStateErrored           = "errored"
	ProgressStateCancelled         = "cancelled"
)

type ProgressUpdateMessagePackage struct {
	ctx     context.Context
	payload ProgressUpdateMessage
}

type ProgressUpdateMessage struct {
	Stage  string `json:"stage"`
	Status string `json:"status"`   // only constanz allowed
	Run    string `json:"run_uuid"` // uuid of the run
	Url    string `json:"url"`
}

type ProgressManager struct {
	apiURL string
	client *http.Client
}

func MustStartProgressFromEnv(ctx context.Context) Progress {
	if dsn := os.Getenv("TOBEY_PROGRESS_DSN"); dsn != "" {
		slog.Info("Using progress service for updates.", "dsn", dsn)

		// TODO: Make this always non-blocking as otherwise it can block the whole application.
		queue := make(chan ProgressUpdateMessagePackage, 1000)
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
		GetEnvString("TOBEY_PROGRESS_DSN", "http://progress:9020"),
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
			wlogger.Debug("Progress Dispatcher: Context cancelled, stopping worker.")
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

			err := w.sendProgressUpdate(ctx_fresh, result)
			if err != nil {
				wlogger.Error("Progress Dispatcher: Sending update ultimately failed.", "error", err)
			} else {
				wlogger.Debug("Progress Dispatcher: Update succesfully sent.", "url", result.Url)
			}

			parentSpan.End()
		}
	}
}

func (w *ProgressManager) sendProgressUpdate(ctx context.Context, msg ProgressUpdateMessage) error {
	logger := slog.With("url", msg.Url, "status", msg.Status, "run", msg.Run)
	logger.Debug("Progress Dispatcher: Sending progress update...")

	ctx_send_webhook, span := tracer.Start(ctx, "handle.progress.queue.send")
	defer span.End()

	url := fmt.Sprintf("%v/%v", w.apiURL, ProgressEndpointUpdate)

	span.SetAttributes(attribute.String("API_URL", url))
	span.SetAttributes(attribute.String("url", msg.Url))
	span.SetAttributes(attribute.String("status_update", msg.Status))

	body, err := json.Marshal(msg)
	if err != nil {
		span.SetStatus(codes.Error, "failed to marshal body")
		span.SetAttributes(attribute.String("data", "TODO"))
		span.RecordError(err)

		return err
	}

	req, err := http.NewRequestWithContext(ctx_send_webhook, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		span.SetStatus(codes.Error, "failed to create request")
		span.RecordError(err)

		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "performing request failed")
		span.RecordError(err)

		return err
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("StatusCode", resp.StatusCode))

	if resp.StatusCode != http.StatusAccepted {
		err := errors.New("status update was not accepted")

		var body_bytes []byte
		resp.Body.Read(body_bytes)

		span.SetAttributes(attribute.String("Body", string(body_bytes[:])))
		span.SetStatus(codes.Error, "update not accepted")
		span.RecordError(err)

		return err
	}
	return nil
}

func (w *ProgressManager) Start(ctx context.Context, progressQueue chan ProgressUpdateMessagePackage) {
	//todo add recovery
	go func(ctx context.Context, progressQueue chan ProgressUpdateMessagePackage) {
		count := GetEnvInt("TOBEY_PROGRESS_WORKER", 4)
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
