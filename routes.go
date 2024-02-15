package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func addRoutes(router *mux.Router) {

	// TODO check if necessary
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		fmt.Fprint(w, "Hello from Tobey.")
	}).Methods("GET")

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		reqID := uuid.New().String()
		log.Printf("Handling incoming crawl request (%d)", reqID)
		ctx, span := tracer.Start(r.Context(), "handleItem", trace.WithAttributes(attribute.String("Crawl Request", reqID)))
		defer span.End()

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

		// TODO sitemap should be ask from differente server
		//workQueue.PublishURL(ctx, reqID, fmt.Sprintf("%s/sitemap.xml", cconf.Root), cconf, whconf)
		workQueue.PublishURL(ctx, reqID, req.URL, cconf, whconf)

		result := &APIResponse{
			CrawlRequestID: reqID,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}).Methods("POST")
}
