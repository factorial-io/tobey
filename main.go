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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"
	"tobey/helper"
	"tobey/internal/collector"

	"github.com/google/uuid"
	"github.com/peterbourgon/diskv"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	_ "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

var (
	// Debug enables or disables debug mode, this can be controlled
	// via the environment variable TOBEY_DEBUG.
	Debug = false
)

const (
	// The port where the main HTTP server listens and the API is served.
	ListenPort int = 8080

	// The port where to ping for healtcheck.
	HealthcheckListenPort int = 10241

	// NumVisitWorkers hard codes the number of workers we start at startup.
	NumVisitWorkers int = 10

	// MaxParallelRuns specifies how many collectors we keep in memory, and thus
	// limits the maximum number of parrallel runs that we can perform.
	MaxParallelRuns int = 128
)

var (
	redisconn    *redis.Client
	rabbitmqconn *amqp.Connection

	workQueue WorkQueue
	runStore  RunStore

	webhookDispatcher *WebhookDispatcher
	progress          Progress
)

func main() {
	slog.Info("Tobey starting...")

	if os.Getenv("TOBEY_DEBUG") == "true" {
		Debug = true
	}
	if Debug {
		slog.Info("Debug mode is on.")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	// This sets up the main process context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	// Setup Opentelemetry
	//todo add opentelemetry logging
	shutdown, erro := setupOTelSDK(ctx)
	if erro != nil {
		panic("ahh")
	}
	err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		log.Fatal(err)
	}

	redisconn = maybeRedis(ctx)
	rabbitmqconn = maybeRabbitMQ(ctx)

	runStore = CreateRunStore(redisconn)

	workQueue = CreateWorkQueue(rabbitmqconn)
	if err := workQueue.Open(); err != nil {
		panic(err)
	}

	// Create Webhook Handling
	webhookQueue := make(chan WebhookPayloadPackage, helper.GetEnvInt("TORBEY_WEBHOOK_PAYLOAD_LIMIT", 100))
	webhook := NewProcessWebhooksManager()
	webhook.Start(ctx, webhookQueue)

	webhookDispatcher = NewWebhookDispatcher(webhookQueue)

	progress = MustStartProgressFromEnv(ctx)

	limiter := CreateLimiter(ctx, redisconn, 1)

	wd, _ := os.Getwd()
	cachedir := filepath.Join(wd, "cache")

	tempdir := os.TempDir()
	slog.Debug("Using temporary directory for atomic file operations.", "dir", tempdir)

	cachedisk := diskv.New(diskv.Options{
		BasePath:     cachedir,
		TempDir:      tempdir,
		CacheSizeMax: 1000 * 1024 * 1024, // 1GB
	})

	httpClient := NewCachingHTTPClient(cachedisk)
	slog.Debug(
		"Initialized caching HTTP client.",
		"cache.dir", cachedir,
		"cache.size", cachedisk.CacheSizeMax,
	)

	cm := collector.NewManager(MaxParallelRuns)

	robots := NewRobots(httpClient)

	visitWorkers := CreateVisitWorkersPool(ctx, NumVisitWorkers, cm, httpClient, limiter, robots)

	apirouter := http.NewServeMux()
	// TODO: Use otel's http mux.

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
		rctx, span := tracer.Start(r.Context(), "receive_crawl_request")
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

		var run uint32
		if req.Run != "" {
			v, err := uuid.Parse(req.Run)
			if err != nil {
				slog.Error("Failed to parse given run as UUID.", "run", req.Run)

				result := &APIError{
					Message: fmt.Sprintf("Failed to parse given run (%s) as UUID or number.", req.Run),
				}

				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(result)
				return
			} else {
				run = v.ID()
			}
		} else {
			run = uuid.New().ID()
		}

		// Ensure at least the URL host is in allowed domains.
		var allowedDomains []string
		if req.Domains != nil {
			allowedDomains = req.Domains
		} else {
			p, _ := url.Parse(req.URL)
			allowedDomains = append(allowedDomains, p.Hostname())
		}

		// Cannot be already present for this run, as the run starts here and
		// now. We don't NewEncoder to check the Manager for an existing one, or
		// fear that we overwrite an existing one.
		c := collector.NewCollector(
			ctx,
			httpClient,
			run,
			allowedDomains,
			func(a string, u *url.URL) (bool, error) {
				if req.SkipRobots {
					return true, nil
				}
				return robots.Check(a, u)
			},
			// Need to use WithoutCancel, to avoid the crawl run to be cancelled once
			// the HTTP request is done. The crawl run should proceed to be handled.
			getEnqueueFn(context.WithoutCancel(rctx), req.WebhookConfig),
			getCollectFn(context.WithoutCancel(rctx), req.WebhookConfig),
		)

		// Also provide local workers access to the collector, through the
		// collectors manager.
		cm.Add(run, c, func(id uint32) {
			runStore.Clear(ctx, id)
		})

		progress.Update(ProgressUpdateMessagePackage{
			context.WithoutCancel(rctx),
			ProgressUpdateMessage{
				PROGRESS_STAGE_NAME,
				PROGRESS_STATE_QUEUED_FOR_CRAWLING,
				run,
				req.URL,
			},
		})

		if !req.SkipSitemap {
			p, _ := url.Parse(req.URL)
			sitemaps := make([]string, 0)

			// Discover sitemaps for the host, if the robots.txt has no
			// information about it, fall back to a well known location.
			urls, err := robots.Sitemaps(p)
			if err != nil {
				slog.Error("Failed to fetch sitemap URLs.", "error", err)
				sitemaps = append(sitemaps, fmt.Sprintf("%s://%s/sitemap.xml", p.Scheme, p.Hostname()))
			} else if len(urls) > 0 {
				sitemaps = urls
			} else {
				sitemaps = append(sitemaps, fmt.Sprintf("%s://%s/sitemap.xml", p.Scheme, p.Hostname()))
			}
			for _, url := range sitemaps {
				slog.Debug("Enqueueing sitemap URL for crawling.", "url", url)
				c.Enqueue(url)
			}
		}
		c.Enqueue(req.URL)

		result := &APIResponse{
			Run: run,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	})

	slog.Info("Starting HTTP API server...", "port", ListenPort)
	apiserver := &http.Server{
		Addr:    fmt.Sprintf(":%d", ListenPort),
		Handler: apirouter,
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

	visitWorkers.Wait()
	slog.Debug("All visit workers stopped.")

	apiserver.Shutdown(context.Background())
	hcserver.Shutdown(context.Background())

	if workQueue != nil {
		workQueue.Close()
	}
	if progress != nil {
		progress.Close()
	}
	if redisconn != nil {
		redisconn.Close()
	}
	if rabbitmqconn != nil {
		rabbitmqconn.Close()
	}

	shutdown(ctx)
}
