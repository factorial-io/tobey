package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
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

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		fmt.Fprint(w, "Hello from Tobey.")
	}).Methods("GET")

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		reqID := uuid.New().ID()
		log.Printf("Handling incoming crawl request (%d)", reqID)

		var req APIRequest
		err := json.Unmarshal(body, &req)
		if err != nil {
			log.Printf("Failed to parse incoming JSON: %s", err)

			result := &APIError{
				Message: fmt.Sprintf("%s", err),
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(result)
			return
		}

		cconf, err := DeriveCollectorConfigFromAPIRequest(&req)
		whconf := req.WebhookConfig

		if err != nil {
			log.Printf("Failed to derivce collector config: %s", err)

			result := &APIError{
				Message: fmt.Sprintf("%s", err),
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(result)
			return
		}

		log.Printf("Processing request (%x) for (%s)", reqID, req.URL)

		workQueue.PublishURL(reqID, fmt.Sprintf("%s/sitemap.xml", cconf.Root), cconf, whconf)
		workQueue.PublishURL(reqID, req.URL, cconf, whconf)

		result := &APIResponse{
			CrawlRequestID: reqID,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}).Methods("POST")

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
