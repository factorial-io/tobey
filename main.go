package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"github.com/gocolly/colly"
	"github.com/gorilla/mux"
)

var (
	NumWorkers       int = 5
	workersWaitGroup sync.WaitGroup

	// Services we connect to...
	workQueue WorkQueue
	progress  Progress
	out       Out
)

var cachedCollectors map[string]*colly.Collector
var cachedCollectorsLock sync.RWMutex

func main() {
	cachedCollectors = make(map[string]*colly.Collector)

	redis := maybeRedis()
	rabbitmq := maybeRabbitMQ()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		select {
		case <-ctx.Done():
			log.Print("Exiting...")

			stop() // Exit everything that took the context.

			if workQueue != nil {
				workQueue.Close()
			}
			if progress != nil {
				progress.Close()
			}
			if out != nil {
				out.Close()
			}
			if redis != nil {
				redis.Close()
			}
			if rabbitmq != nil {
				rabbitmq.Close()
			}

			workersWaitGroup.Wait()
			os.Exit(1)
		}
	}()

	workQueue = CreateWorkQueue(rabbitmq)
	if err := workQueue.Open(); err != nil {
		panic(err)
	}
	defer workQueue.Close()

	out = MustStartOutFromEnv()
	defer out.Close()

	progress = MustStartProgressFromEnv()
	defer progress.Close()

	log.Printf("Starting %d workers...", NumWorkers)
	for i := 0; i < NumWorkers; i++ {
		workersWaitGroup.Add(1)

		go func(id int) {
			for {
				msg, err := workQueue.Consume()

				if err != nil {
					log.Print(err)
					continue
				}

				cachedCollectorsLock.Lock()
				var c *colly.Collector
				if v, ok := cachedCollectors[msg.Site.ID]; ok {
					c = v
				} else {
					c = CreateCollectorForSite(redis, workQueue, out, msg.Site)
					cachedCollectors[msg.Site.ID] = c
				}
				cachedCollectorsLock.Unlock()

				if err := c.Visit(msg.URL); err != nil {
					// log.Printf("Error visiting URL (%s): %s", msg.URL, err)
					continue
				}
				log.Printf("Worker (%d) scraped URL (%s)", id, msg.URL)
			}
			workersWaitGroup.Done()
			log.Print("Worker exited!")
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

		var req APIRequest
		json.Unmarshal(body, &req)

		site, err := DeriveSiteFromAPIRequest(&req)
		if err != nil {
			log.Print(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		log.Printf("Got request for site (%s)", req.URL)

		workQueue.PublishURL(site, fmt.Sprintf("%s/sitemap.xml", site.Root))
		workQueue.PublishURL(site, req.URL)

		result := &APIResponse{
			Site: site,
			URL:  req.URL,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}).Methods("POST")

	log.Printf("Starting HTTP server, listening on %s...", ":8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}
