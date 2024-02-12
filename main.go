package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/mux"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
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
	log.Print("Tobey starting...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	redisconn = maybeRedis(ctx)
	rabbitmqconn = maybeRabbitMQ(ctx)

	workQueue = CreateWorkQueue(rabbitmqconn)
	if err := workQueue.Open(); err != nil {
		panic(err)
	}

	limiter = CreateLimiter(ctx, redisconn, 1*time.Second)
	webhookDispatcher = NewWebhookDispatcher()
	progress = MustStartProgressFromEnv()

	workers := CreateVisitWorkersPool(ctx, NumVisitWorkers)

	router := mux.NewRouter()
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
	if webhookDispatcher != nil {
		webhookDispatcher.Close()
	}
	if redisconn != nil {
		redisconn.Close()
	}
	if rabbitmqconn != nil {
		rabbitmqconn.Close()
	}

}
