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

	"github.com/gocolly/colly"
	"github.com/gocolly/colly/queue"
	"github.com/gorilla/mux"
)

type Site struct {
	ID        string
	Domain    string
	Queue     *queue.Queue
	Collector *colly.Collector
}

func newCollectorQueue(domain string) (*colly.Collector, *queue.Queue) {
	basePath, _ := os.Getwd()

	c := colly.NewCollector(
		colly.Async(true),
		colly.CacheDir(filepath.Join(basePath, "cache", domain)),
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

func main() {
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
