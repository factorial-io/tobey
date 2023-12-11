package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gocolly/colly"
	"github.com/gocolly/colly/queue"
	"github.com/gorilla/mux"
	"github.com/streadway/amqp"
)

var (
	// The directory that holds response cache, i.e. /tmp/cache. When missing the directory
	// will be automatically created. Defaults to ./cache.
	CacheDir string = "./cache"

	RabbitMQConnection *amqp.Connection
	FanoutConnection   <-chan *Result // TODO: Implement
	ProgressConnection <-chan bool    // TODO: Implement
)

type Site struct {
	ID        string
	Domain    string
	Queue     *queue.Queue
	Collector *colly.Collector
}

type Result struct {
	SiteID   string
	Request  *colly.Request
	Response *colly.Response
	Callback interface{}
}

func main() {
	FanoutConnection = make(chan *Result) // assing to global
	ProgressConnection = make(chan bool)  // assign to global

	// Simulate FannoutService
	go func() {
		for {
			select {
			case r := <-FanoutConnection:
				log.Print("Fanout Service got incoming:", r)
			}
		}
	}()

	// Simulate ProgressService
	go func() {
		for {
			select {
			case r := <-FanoutConnection:
				log.Print("Fanout Service got incoming:", r)
			}
		}
	}()

	mqdsn, ok := os.LookupEnv("TOBEY_RABBITMQ_DSN")

	if ok { // TODO: RabbitMQ isn't used yet.
		log.Printf("Using RabbitMQ work queue with DSN (%s)...", mqdsn)

		conn, err := backoff.RetryNotifyWithData(func() (*amqp.Connection, error) {
			return amqp.Dial(mqdsn)
		}, backoff.NewExponentialBackOff(), func(err error, t time.Duration) {
			log.Print(err)
		})

		if err != nil {
			panic(err)
		}
		log.Printf("Successfully connected to RabbitMQ work queue with DSN (%s)", mqdsn)

		RabbitMQConnection = conn // assign to global
		defer RabbitMQConnection.Close()
	} else {
		log.Print("Using in-memory work queue...")
	}

	router := mux.NewRouter()

	// Maps a site, currently identified by a hostname to a collector
	var sites map[string]*Site
	sites = make(map[string]*Site, 0)

	router.HandleFunc("/sites", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "application/json")

		var req SiteRequest
		json.Unmarshal(body, &req)

		siteID, _ := generateSiteIDFromURL(req.URL)

		var site *Site

		if val, ok := sites[siteID]; ok {
			site = val
		} else {
			url, err := url.Parse(req.URL)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			domain := strings.TrimPrefix(url.Hostname(), "www.")

			c, q := newCollectorQueue(domain)

			site = &Site{
				ID:        siteID,
				Domain:    domain,
				Collector: c,
				Queue:     q,
			}
			sites[site.ID] = site // Put in the map for later use.
		}

		log.Printf("Got request for site (%s)", req.URL)
		site.Queue.AddURL(req.URL)

		site.Queue.Run(site.Collector) // TODO: Move this elsewhere.

		result := &SiteResponse{
			ID:  site.ID,
			URL: req.URL,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}).Methods("POST")

	log.Printf("Starting HTTP server, listening on %s...", ":8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func newCollectorQueue(domain string) (*colly.Collector, *queue.Queue) {
	c := colly.NewCollector(
		colly.Async(true),
		colly.CacheDir(filepath.Join(CacheDir, domain)),
		//	colly.Debugger(&debug.LogDebugger{}),
		colly.AllowedDomains(domain, fmt.Sprintf("www.%s", domain)), // Constrain to requested domain
	)

	q, _ := queue.New(
		2, // Number of consumer threads
		&queue.InMemoryQueueStorage{MaxSize: 10000}, // Use default queue storage, TODO: use rabbitMQ
		// https://github.com/gocolly/colly/issues/281
	)
	c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: 2})

	// Find and visit all links
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		// log.Printf("Link found: %q -> %s\n", e.Text, link)

		log.Printf("Found URL (%s)", e.Request.AbsoluteURL(link))
		q.AddURL(e.Request.AbsoluteURL(link))
	})

	c.OnRequest(func(r *colly.Request) {
		log.Printf("Visiting URL (%s)...", r.URL)
		r.Ctx.Put("url", r.URL.String())
	})
	c.OnResponse(func(r *colly.Response) {
		log.Printf("Visited URL (%s)", r.Ctx.Get("url"))
	})
	c.OnScraped(func(r *colly.Response) {
		log.Printf("Scraped URL (%s)", r.Ctx.Get("url"))
	})

	return c, q
}
