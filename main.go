package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"time"
	"tobey/helper"
	logger "tobey/logger"

	"github.com/gorilla/mux"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

var (
	// NumVisitWorkers hard codes the number of workers we start at startup.
	NumVisitWorkers int = 10
)

var (
	redisconn    *redis.Client
	rabbitmqconn *amqp.Connection

	workQueue         WorkQueue
	limiter           LimiterAllowFn
	webhookDispatcher *WebhookDispatcher
	progress          Progress
)

func main() {

	logger.InitLoggerDefault(helper.GetEnvString("LOG_LEVEL", "debug"))

	log := logger.GetBaseLogger().WithField("Version", "0.0.1")
	//todo add opentelemetry logging
	log.Print("Tobey starting...")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	// Setup Opentelemetry
	shutdown, erro := setupOTelSDK(ctx)
	if erro != nil {
		panic("ahh")
	}
	err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		log.Fatal(err)
	}

	go startHealthcheck()

	redisconn = maybeRedis(ctx)
	rabbitmqconn = maybeRabbitMQ(ctx)

	workQueue = CreateWorkQueue(rabbitmqconn)
	if err := workQueue.Open(); err != nil {
		panic(err)
	}

	limiter = CreateLimiter(ctx, redisconn, 1*time.Second)

	// Create Webhook Handling
	webhookQueue := make(chan WebhookPayloadPackage, helper.GetEnvInt("TORBEY_WEBHOOK_PAYLOAD_LIMIT", 100))
	webhook := NewProcessWebhooksManager()
	webhook.Start(ctx, webhookQueue)

	webhookDispatcher = NewWebhookDispatcher(webhookQueue)

	progress = MustStartProgressFromEnv(ctx)

	workers := CreateVisitWorkersPool(ctx, NumVisitWorkers)

	router := mux.NewRouter()
	router.Use(otelmux.Middleware("torbey-api"))
	addRoutes(router)

	log.Printf("Starting HTTP server, listening on %s...", ":8080")

	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}
	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
		}
		log.Println("Stopped serving new HTTP connections.")
	}()

	<-ctx.Done()
	log.Print("Exiting...")
	stop() // Exit everything that took the context.

	log.Print("Cleaning up...")
	workers.Wait()

	server.Shutdown(context.Background())

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
