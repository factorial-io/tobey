// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
	"tobey/internal/ctrlq"

	"github.com/mariuswilms/tears"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	_ "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

// These variables can be controlled via environment variables.
var (
	// Debug enables or disables debug mode, this can be controlled
	// via the environment variable TOBEY_DEBUG.
	Debug = false

	// SkipCache disables caching when true. It can be controlled via the
	// TOBEY_SKIP_CACHE environment variable.
	SkipCache = false

	// These can be controlled via the TOBEY_TELEMETRY environment variable.
	UseTracing = false
	UseMetrics = false
	UsePulse   = false // High Frequency Metrics can be enabled by adding "pulse".

	// NumVisitWorkers hard codes the number of workers we start at startup.
	NumVisitWorkers int = 5

	// ListenHost is the host where the main HTTP server listens and the API is served,
	// this can be controlled via the TOBEY_HOST environment variable. An empty
	// string means "listen on all interfaces".
	ListenHost string = ""

	// The port where the main HTTP server listens and the API is served, this can
	// be controlled via the TOBEY_PORT environment variable.
	ListenPort int = 8080
)

const (
	// MaxParallelRuns specifies how many collectors we keep in memory, and thus
	// limits the maximum number of parrallel runs that we can perform.
	MaxParallelRuns int = 128

	// RunTTL specifies the maximum time a run is kept in memory. After this
	// time the run is evicted from memory and the cache. The cache may contain
	// sensitve information, so we should not keep it around for too long.
	RunTTL = 24 * time.Hour

	// HostTTL specifies the maximum time a host is kept in memory. After this
	// time the host is evicted from memory and the cache. The TTL defaults to 365 days.
	HostTTL = 365 * 24 * time.Hour

	// UserAgent to be used with all HTTP request. The value is set to a
	// backwards compatible one. Some sites allowwlist this specific user agent.
	UserAgent = "WebsiteStandardsBot/1.0"

	// HTTPCachePath is the absolute or relative path (to the working
	// directory) where we store the cache for HTTP responses.
	HTTPCachePath = "./cache"

	// The port where to ping for healthcheck.
	HealthcheckListenPort = 10241

	// PulseEndpoint is the endpoint where we send the high frequency metrics.
	PulseEndpoint = "http://localhost:8090"
)

func configure() {
	if os.Getenv("TOBEY_DEBUG") == "true" {
		Debug = true
	}
	if os.Getenv("TOBEY_SKIP_CACHE") == "true" {
		SkipCache = true
		slog.Info("Skipping cache.")
	}
	if v := os.Getenv("TOBEY_WORKERS"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		NumVisitWorkers = p
	}
	v := os.Getenv("TOBEY_TELEMETRY")
	if strings.Contains(v, "traces") || strings.Contains(v, "tracing") {
		UseTracing = true
		slog.Info("Tracing enabled.")
	}
	if strings.Contains(v, "metrics") {
		UseMetrics = true
		slog.Info("Metrics enabled.")
	}
	if strings.Contains(v, "pulse") {
		UsePulse = true
		slog.Info("High Frequency Metrics (Pulse) enabled.")
	}

	if v := os.Getenv("TOBEY_HOST"); v != "" {
		ListenHost = v
	}
	if v := os.Getenv("TOBEY_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		ListenPort = p
	}
}

func main() {
	slog.Info("Tobey starting...")
	tear, down := tears.New()

	configure()

	if Debug {
		slog.Info("Debug mode is on.")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	// This sets up the main process context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	var err error
	tear(stop)

	shutdownOtel, err := StartOTel(ctx)
	if err != nil {
		panic(err)
	}
	tear(shutdownOtel).End()

	if UseMetrics {
		err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
		if err != nil {
			log.Fatal(err)
		}
	}

	if UsePulse {
		startPulse(ctx)
	}

	redisconn, err := maybeRedis(ctx)
	if err != nil {
		panic(err)
	}
	if redisconn != nil {
		tear(redisconn.Close)
	}

	robots := NewRobots()
	sitemaps := NewSitemaps(robots) // Sitemaps will use Robots to discover sitemaps.

	runs := NewRunManager(redisconn, robots, sitemaps)

	queue := ctrlq.CreateWorkQueue(redisconn)
	if err := queue.Open(ctx); err != nil {
		panic(err)
	}
	if queue != nil {
		tear(queue.Close)
	}

	if _, ok := os.LookupEnv("TOBEY_RESULTS_DSN"); !ok {
		if _, ok := os.LookupEnv("TOBEY_RESULT_DSN"); ok {
			slog.Debug("You have a typo in your env var: TOBEY_RESULTS_DSN is not set, but TOBEY_RESULT_DSN is set. Please use TOBEY_RESULTS_DSN instead.")
		}
	}
	rs, err := CreateResultReporter(os.Getenv("TOBEY_RESULTS_DSN"))
	if err != nil {
		panic(err)
	}

	progress, err := CreateProgressReporter(os.Getenv("TOBEY_PROGRESS_DSN"))
	if err != nil {
		panic(err)
	}

	workers := CreateVisitWorkersPool(
		ctx,
		NumVisitWorkers,
		runs,
		queue,
		progress,
		rs,
	)
	tear(workers.Wait)

	apirouter := http.NewServeMux()

	apirouter.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		fmt.Fprint(w, "Hello from Tobey.")
	})

	apirouter.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()

		slog.Debug("Handling incoming request for crawl run...")

		// The context of the HTTP request might contain OpenTelemetry information,
		// i.e. SpanID or TraceID. If this is the case the line below creates
		// a sub span. Otherwise we'll start a new root span here.
		reqctx, span := tracer.Start(r.Context(), "receive_crawl_request")
		// This ends the very first span in handling the crawl run. It ends the HTTP handling span.
		defer span.End()

		w.Header().Set("Content-Type", "application/json")

		var req APIRequest
		if bytes.HasPrefix(body, []byte("http://")) || bytes.HasPrefix(body, []byte("https://")) {
			// As a special case, and to support minimalism, we allow directly
			// posting a single URL.
			req.URL = string(body)
		} else {
			err := json.Unmarshal(body, &req)
			if err != nil {
				slog.Error("Failed to parse incoming JSON.", "error", err)

				result := &APIError{
					Message: fmt.Sprintf("%s", err),
				}
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(result)
				return
			}
		}

		if ok := req.Validate(); !ok {
			result := &APIError{
				Message: "Invalid request.",
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(result)
			return
		}

		id, err := req.GetRun()
		if err != nil {
			slog.Error("Failed to parse given run as UUID.", "run", req.Run)

			result := &APIError{
				Message: fmt.Sprintf("Failed to parse given run (%s) as UUID or number.", req.Run),
			}

			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(result)
			return
		}

		run := &Run{
			SerializableRun: SerializableRun{
				ID:       id,
				Metadata: req.RunMetadata,

				URLs: req.GetURLs(true),

				AuthConfigs: req.GetAuthConfigs(),

				AllowDomains: req.GetAllowDomains(),
				AllowPaths:   req.GetAllowPaths(),
				DenyPaths:    req.GetDenyPaths(),

				SkipRobots:           req.SkipRobots,
				SkipSitemapDiscovery: req.SkipSitemapDiscovery,

				WebhookConfig: req.WebhookResultStoreConfig,
			},
		}

		// Ensure we make the run configuration available in the store, before
		// we start publishing to the work queue.
		runs.Add(ctx, run)

		go run.Start(reqctx, queue, progress, rs, req.GetURLs(true))

		result := &APIResponse{
			Run: run.ID,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	})

	if UseMetrics {
		apirouter.Handle("GET /metrics", promhttp.Handler())
	}

	slog.Info("Starting HTTP API server...", "host", ListenHost, "port", ListenPort)
	apiserver := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", ListenHost, ListenPort),
		Handler: otelhttp.NewHandler(apirouter, "get_new_request"),
	}
	go func() {
		if err := apiserver.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error.", "error", err)
		}
		slog.Info("Stopped serving new API HTTP connections.")
	}()
	tear(apiserver.Shutdown)

	slog.Info("Starting HTTP Healthcheck server...", "port", HealthcheckListenPort)
	hcrouter := http.NewServeMux()

	// Supports HEAD requests as well.
	hcrouter.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		// TODO: Add actual healthchecking logic here.

		fmt.Fprint(w, "OK")
	})

	hcserver := &http.Server{
		Addr:    fmt.Sprintf(":%d", HealthcheckListenPort),
		Handler: hcrouter,
	}
	go func() {
		if err := hcserver.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error.", "error", err)
		}
		slog.Info("Stopped serving new Healthcheck HTTP connections.")
	}()
	tear(hcserver.Shutdown)

	<-ctx.Done()

	slog.Info("Exiting...")
	down(context.Background())
}
