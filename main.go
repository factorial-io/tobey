package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"tobey/internal/collector"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/peterbourgon/diskv"
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
	cachedisk    *diskv.Diskv

	workQueue WorkQueue
	runStore  RunStore

	limiter           LimiterAllowFn
	webhookDispatcher *WebhookDispatcher
	progress          Progress
)

func main() {
	log.Print("Tobey starting...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	redisconn = maybeRedis(ctx)
	rabbitmqconn = maybeRabbitMQ(ctx)

	runStore = CreateRunStore(redisconn)

	workQueue = CreateWorkQueue(rabbitmqconn)
	if err := workQueue.Open(); err != nil {
		panic(err)
	}

	webhookDispatcher = NewWebhookDispatcher()
	progress = MustStartProgressFromEnv()

	limiter = CreateLimiter(ctx, redisconn, 1*time.Second)

	wd, _ := os.Getwd()
	cachedir := filepath.Join(wd, "cache")
	log.Printf("Using cache directory (%s)...", cachedir)

	tempdir := os.TempDir()
	log.Printf("Using temporary directory (%s)...", tempdir)

	cachedisk = diskv.New(diskv.Options{
		BasePath:     cachedir,
		TempDir:      tempdir,
		CacheSizeMax: 1000 * 1024 * 1024, // 1GB
	})
	log.Print("Initialized disk backed cache with a 1GB limit.")

	httpClient := NewCachingHTTPClient(cachedisk)

	cm := collector.NewManager()

	workers := CreateVisitWorkersPool(ctx, NumVisitWorkers, cm)

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
		log.Print("Handling incoming crawl request...")

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

		var runID uint32
		if req.RunID != "" {
			v, err := uuid.Parse(req.RunID)
			if err != nil {
				v, err := strconv.ParseUint(req.RunID, 10, 32)
				if err != nil {
					log.Printf("Failed to parse given run ID (%s) as UUID or number.", req.RunID)

					result := &APIError{
						Message: fmt.Sprintf("Failed to parse given run ID (%s) as UUID or number.", req.RunID),
					}

					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(result)
					return
				} else {
					runID = uint32(v)
				}
			} else {
				runID = v.ID()
			}
		} else {
			runID = uuid.New().ID()
		}

		// Ensure at least the URL host is in allowed domains, otherwise we'll
		// crawl the whole internet.
		var allowedDomains []string
		if req.Domains != nil {
			allowedDomains = req.Domains
		} else {
			p, _ := url.Parse(req.URL)
			allowedDomains = append(allowedDomains, p.Hostname())
		}

		c := collector.NewCollector(
			ctx,

			httpClient,

			runID,
			allowedDomains,

			// enqueue function, that will enqueue a single URL to
			// be crawled. The enqueue function is called whenever
			// a new URL is discovered by that Collector, i.e. by
			// looking at all links in a crawled page HTML.
			func(c *collector.Collector, url string) error {
				// Ensure we never publish a URL twice for a single run. Not only does
				// this help us not put unnecessary load on the queue, it also helps with
				// ensuring there will only (mostly) be one result for a page. There is a slight
				// chance that two processes enter this function with the same runID and url,
				// before one of them is finished.
				if !c.IsDomainAllowed(GetHostFromURL(url)) {
					// log.Printf("Skipping enqueuing of crawl of URL (%s), domain not allowed...", msg.URL)
					return nil
				}
				if runStore.HasSeen(ctx, runID, url) {
					// Do not need to enqueue an URL that has already been crawled, and its response
					// can be served from cache.
					return nil
				}

				err := workQueue.PublishURL(
					// Passing the crawl request ID, so when
					// consumed the URL is crawled by the matching
					// Collector.
					c.ID, // The collector's ID is the run ID.
					url,
					req.WebhookConfig,
				)
				if err == nil {
					runStore.Seen(ctx, runID, url)
				}
				return err
			},

			// visit function
			func(c *collector.Collector, url string) (bool, time.Duration, error) {
				ok, retryAfter, err := limiter(url)
				if err != nil {
					log.Printf("Error while checking rate limiter for  message: %d", c.ID)
					return ok, retryAfter, err
				}
				if !ok {
					return ok, retryAfter, err
				}
				return ok, retryAfter, c.Scrape(url)
			},

			// collect function that is called once we have a
			// result. Uses the information provided in the original
			// crawl request, i.e. the WebhookConfig, that we have
			// received via the queued message.
			func(c *collector.Collector, res *collector.Response) {
				if req.WebhookConfig != nil && req.WebhookConfig.Endpoint != "" {
					webhookDispatcher.Send(req.WebhookConfig, res)
				}
			},
		)

		// Provide workers access to the collector, through the collectors manager.
		cm.Add(runID, c)

		c.EnqueueVisit(fmt.Sprintf("%s/sitemap.xml", strings.TrimRight(req.URL, "/")))
		c.EnqueueVisit(req.URL)

		result := &APIResponse{
			RunID: runID,
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
