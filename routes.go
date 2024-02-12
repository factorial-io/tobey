package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func addRoutes(router *mux.Router) {
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
}
