// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(GetEnvString("OTEL_SERVICE_NAME", "tobey")),
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

// GetEnvString gets the environment variable for a key and if that env-var hasn't been set it returns the default value
func GetEnvString(key string, defaultVal string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		value = defaultVal
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}

// GetEnvBool gets the environment variable for a key and if that env-var hasn't been set it returns the default value
func GetEnvBool(key string, defaultVal bool) bool {
	envvalue := os.Getenv(key)
	value, err := strconv.ParseBool(envvalue)
	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}

// GetEnvInt gets the environment variable for a key and if that env-var hasn't been set it returns the default value. This function is equivalent to ParseInt(s, 10, 0) to convert env-vars to type int
func GetEnvInt(key string, defaultVal int) int {
	envvalue := os.Getenv(key)
	value, err := strconv.Atoi(envvalue)

	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}

// GetEnvFloat gets the environment variable for a key and if that env-var hasn't been set it returns the default value. This function uses bitSize of 64 to convert string to float64.
func GetEnvFloat(key string, defaultVal float64) float64 {
	envvalue := os.Getenv(key)
	value, err := strconv.ParseFloat(envvalue, 64)
	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}
