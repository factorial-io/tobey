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
	"sync"
	"time"

	"tobey/internal/colly"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

var (
	// TODO: Once we have rate limiting in place
	NumWorkers       int = 5
	workersWaitGroup sync.WaitGroup
)

var (
	redisconn *redis.Client
	rabbitmq  *amqp.Connection

	workQueue         WorkQueue
	limiter           LimiterAllowFn
	webhookDispatcher *WebhookDispatcher
	progress          Progress
)

var (
	cachedCollectors     map[uint32]*colly.Collector // TODO: Ensure this doesn't grow unbounded.
	cachedCollectorsLock sync.RWMutex
)

func main() {
	log.Print("Tobey starting...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	redisconn = maybeRedis(ctx)
	rabbitmq = maybeRabbitMQ(ctx)

	workQueue = CreateWorkQueue(rabbitmq)
	if err := workQueue.Open(); err != nil {
		panic(err)
	}

	limiter = CreateLimiter(ctx, redisconn, 1*time.Second)
	webhookDispatcher = NewWebhookDispatcher()
	progress = MustStartProgressFromEnv()

	cachedCollectors = make(map[uint32]*colly.Collector)

	log.Printf("Starting %d workers...", NumWorkers)
	for i := 0; i < NumWorkers; i++ {
		workersWaitGroup.Add(1)

		go func(id int) {
			if err := Worker(ctx, id); err != nil {
				log.Printf("Worker (%d) exited with error: %s", id, err)
			} else {
				log.Printf("Worker (%d) exited cleanly.", id)
			}
			workersWaitGroup.Done()
		}(i)
	}

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
	workersWaitGroup.Wait()

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
	if rabbitmq != nil {
		rabbitmq.Close()
	}

}
