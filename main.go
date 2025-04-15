// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"
	"tobey/internal/ctrlq"

	charmlog "github.com/charmbracelet/log"
	"github.com/mariuswilms/tears"
	_ "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

// These variables can be controlled via environment variables and are set
// via configure().
var (
	// Debug enables or disables debug mode, this can be controlled
	// via the environment variable TOBEY_DEBUG.
	Debug = false

	// SkipCache disables caching when true. It can be controlled via the
	// TOBEY_SKIP_CACHE environment variable.
	SkipCache = false

	// ListenHost is the host where the main HTTP server listens and the API is served,
	// this can be controlled via the TOBEY_HOST environment variable. An empty
	// string means "listen on all interfaces".
	ListenHost string = ""

	// The port where the main HTTP server listens and the API is served, this can
	// be controlled via the TOBEY_PORT environment variable.
	ListenPort int = 8080

	// NumVisitWorkers hard codes the number of workers we start at startup.
	NumVisitWorkers int = 5

	// UserAgent to be used with all HTTP requests unless overridden per run.
	// Can be controlled via TOBEY_USER_AGENT env var.
	UserAgent = "Tobey/0"

	// DynamicConfig allows the user to reconfigure i.e. the result reporter
	// at runtime via the API. This should only be enabled if the users of the API
	// can be fully trusted!
	DynamicConfig = false

	// These can be controlled via the TOBEY_TELEMETRY environment variable.
	UseTracing = false
	UseMetrics = false
	UsePulse   = false // High Frequency Metrics can be enabled by adding "pulse".
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

	// HTTPCachePath is the absolute or relative path (to the working
	// directory) where we store the cache for HTTP responses.
	HTTPCachePath = "./cache"

	// The port where to ping for healthcheck.
	HealthcheckListenPort = 10241

	// PulseEndpoint is the endpoint where we send the high frequency metrics.
	PulseEndpoint = "http://localhost:8090"
)

func main() {
	asService, req := configure()

	if asService {
		service()
	} else {
		cli(req)
	}
}

func service() {
	slog.Info("Tobey starting...")
	tear, down := tears.New()

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

	// Set up the default result reporter. If dynamic config is enabled, we
	// will use the API to change the result reporter at runtime.
	defaultrr, err := CreateResultReporter(ctx, os.Getenv("TOBEY_RESULT_REPORTER_DSN"), nil, nil)
	if err != nil {
		panic(err)
	}

	progress, err := CreateProgressReporter(os.Getenv("TOBEY_PROGRESS_DSN"))
	if err != nil {
		panic(err)
	}

	vpool := NewVisitorPool(
		ctx,
		NumVisitWorkers,
		runs,
		queue,
		defaultrr,
		progress,
	)
	tear(vpool.Close)

	// Set up and start the main API server.
	slog.Info("Starting HTTP API server...", "host", ListenHost, "port", ListenPort)
	apiserver := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", ListenHost, ListenPort),
		Handler: setupRoutes(runs, queue, defaultrr, progress),
	}
	go func() {
		if err := apiserver.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error.", "error", err)
		}
		slog.Info("Stopped serving new API HTTP connections.")
	}()
	tear(apiserver.Shutdown)

	// Set up and start the healthcheck server.
	slog.Info("Starting HTTP Healthcheck server...", "port", HealthcheckListenPort)
	healthcheckHandler := setupHealthcheckRoutes(vpool)
	hcserver := &http.Server{
		Addr:    fmt.Sprintf(":%d", HealthcheckListenPort),
		Handler: healthcheckHandler,
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

// cli runs tobey in cli mode. This preconfigures tobey in a way
// that makes sense for the cli, i.e. outputting logs to the console.
func cli(req ConsoleRequest) {
	// Sync log levels, limited.
	var charmlogLevel charmlog.Level
	if Debug {
		charmlogLevel = charmlog.DebugLevel
	} else {
		charmlogLevel = charmlog.InfoLevel
	}
	charmLogger := charmlog.NewWithOptions(os.Stdout, charmlog.Options{
		ReportTimestamp: false,
		ReportCaller:    false,
		Level:           charmlogLevel,
	})
	slog.SetDefault(slog.New(charmLogger))

	tear, down := tears.New()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	robots := NewRobots()
	sitemaps := NewSitemaps(robots)
	runs := NewRunManager(nil, robots, sitemaps)
	queue := ctrlq.CreateWorkQueue(nil)

	if err := queue.Open(ctx); err != nil {
		panic(err)
	}
	if queue != nil {
		tear(queue.Close)
	}

	if ok, violations := req.Validate(); !ok {
		for _, v := range violations {
			slog.Warn(fmt.Sprintf("Violation: %s", v))
		}
		slog.Error("Invalid params/args")
		os.Exit(1)
	}

	rr, err := CreateResultReporter(ctx, "disk://"+req.GetOutputDir(), nil, nil)
	if err != nil {
		panic(err)
	}
	progress := NewMemoryProgressReporter()

	vpool := NewVisitorPool(
		ctx,
		NumVisitWorkers,
		runs,
		queue,
		rr,
		progress,
	)
	tear(vpool.Close)

	run := &Run{
		SerializableRun: SerializableRun{
			ID: req.GetRun(),

			URLs: req.GetURLs(true),

			AuthConfigs: req.GetAuthConfigs(),

			AllowDomains: req.GetAllowDomains(),
			IgnorePaths:  req.GetIgnorePaths(),

			UserAgent: req.GetUserAgent(),
		},
	}

	slog.Info("Performing run, please hit STRG+C to cancel and exit.")

	runs.Add(ctx, run)
	go run.Start(ctx, queue, rr, progress, req.GetURLs(true))

	select {
	case <-ctx.Done():
		slog.Info("Cancelled, exiting...")
	case <-progress.IsRunFinished(run.ID):
		slog.Info("Run finished, exiting...")
		stop() // Safe to call multiple times.
	}

	down(context.Background())
}
