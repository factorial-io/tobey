// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mariuswilms/tears"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

var (
	tracer = otel.Tracer("tobey")
)

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

// StartOTel bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func StartOTel(ctx context.Context) (tears.DownFn, error) {
	tear, down := tears.New()

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	if UseTracing && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		// Set up trace provider.
		tracerProvider, erro := newTraceProvider(ctx)
		if erro != nil {
			return down, erro
		}

		tear(tracerProvider.Shutdown)
		otel.SetTracerProvider(tracerProvider)
	}

	if UseMetrics && os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" {
		// Set up meter provider.
		meterProvider, erra := newMeterProvider(ctx)
		if erra != nil {
			return down, erra
		}
		tear(meterProvider.Shutdown)
		otel.SetMeterProvider(meterProvider)
	}

	return down, nil
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTraceProvider(ctx context.Context) (*trace.TracerProvider, error) {
	getEnvString := func(key string, defaultVal string) string {
		value := os.Getenv(key)
		if len(value) == 0 {
			value = defaultVal
		}
		return value
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(getEnvString("OTEL_SERVICE_NAME", "tobey")),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	/*traceExporter, err := stdouttrace.New(
	stdouttrace.WithPrettyPrint())*/
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(traceExporter,
			// Default is 5s. Set to 1s for demonstrative purposes.
			trace.WithBatchTimeout(time.Second)),
	)

	return traceProvider, nil
}

func newMeterProvider(ctx context.Context) (*metric.MeterProvider, error) {
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			// Default is 1m. Set to 3s for demonstrative purposes.
			metric.WithInterval(3*time.Second))),
	)
	return meterProvider, nil
}
