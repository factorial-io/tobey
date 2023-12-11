package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"

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

func main() {
	workQueue = MustStartWorkQueueFromEnv()
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
				c := NewCollectorForSite(workQueue, out, msg.Site) // FIXME: Probably cache Collector.

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
			w.WriteHeader(http.StatusBadRequest)
			return

		}

		log.Printf("Got request for site (%s)", req.URL)
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
	// TODO: workersWaitGroup.Wait()
}
