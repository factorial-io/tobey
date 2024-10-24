// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
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

	// ListenHost is the host where the main HTTP server listens and the API is served,
	// this can be controlled via the TOBEY_HOST environment variable. An empty
	// string means "listen on all interfaces".
	ListenHost string = ""

	// The port where the main HTTP server listens and the API is served, this can
	// be controlled via the TOBEY_PORT environment variable.
	ListenPort int = 8080
)

const (
	// NumVisitWorkers hard codes the number of workers we start at startup.
	NumVisitWorkers int = 5

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
	HealthcheckListenPort int = 10241

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
	configure()

	if Debug {
		slog.Info("Debug mode is on.")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	// This sets up the main process context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	var err error
	// Setup Opentelemetry
	// TODO: Add opentelemetry logging
	shutdown, err := setupOTelSDK(ctx)
	if err != nil {
		panic(err)
	}
	err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		log.Fatal(err)
	}

	if UsePulse {
		startPulse(ctx)
	}

	redisconn, err := maybeRedis(ctx)
	if err != nil {
		panic(err)
	}

	robots := NewRobots()
	sitemaps := NewSitemaps(robots) // Sitemaps will use Robots to discover sitemaps.

	runs := NewRunManager(redisconn, robots, sitemaps)

	queue := CreateWorkQueue(redisconn)
	if err := queue.Open(ctx); err != nil {
		panic(err)
	}

	// Create Webhook Handling, TODO: this should always be non-blocking, as otherwise
	// our visit workers will stand still.
	hooksqueue := make(chan WebhookPayloadPackage, 1000)
	hooksmgr := NewProcessWebhooksManager()
	hooksmgr.Start(ctx, hooksqueue)
	hooks := NewWebhookDispatcher(hooksqueue)

	progress := MustStartProgressFromEnv(ctx)

	workers := CreateVisitWorkersPool(
		ctx,
		NumVisitWorkers,
		runs,
		queue,
		progress,
		hooks,
	)

	apirouter := http.NewServeMux()

	apirouter.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		fmt.Fprint(w, "Hello from Tobey.")
	})

	apirouter.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		slog.Debug("Handling incoming request for crawl run...")

		// The context of the HTTP request might contain OpenTelemetry information,
		// i.e. SpanID or TraceID. If this is the case the line below creates
		// a sub span. Otherwise we'll start a new root span here.
		reqctx, span := tracer.Start(r.Context(), "receive_crawl_request")
		// This ends the very first span in handling the crawl run. It ends the HTTP handling span.
		defer span.End()

		var req APIRequest
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
				ID: id,

				URLs: req.GetURLs(true),

				AuthConfigs: req.GetAuthConfigs(),

				AllowDomains: req.GetAllowDomains(),
				AllowPaths:   req.GetAllowPaths(),
				DenyPaths:    req.GetDenyPaths(),

				SkipRobots:           req.SkipRobots,
				SkipSitemapDiscovery: req.SkipSitemapDiscovery,

				WebhookConfig: req.WebhookConfig,
			},
		}

		// Ensure we make the run configuration available in the store, before
		// we start publishing to the work queue.
		runs.Add(ctx, run)

		go run.Start(reqctx, queue, progress, hooks, req.GetURLs(true))

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

	<-ctx.Done()
	slog.Info("Exiting...")
	stop() // Exit everything that took the context.

	slog.Debug("Cleaning up...")

	workers.Wait()
	slog.Debug("All visit workers stopped.")

	apiserver.Shutdown(context.Background())
	hcserver.Shutdown(context.Background())

	if queue != nil {
		queue.Close()
	}
	if progress != nil {
		progress.Close()
	}
	if redisconn != nil {
		redisconn.Close()
	}

	shutdown(ctx)
}
