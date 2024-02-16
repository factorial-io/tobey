package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	logger "tobey/logger"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func addRoutes(router *mux.Router) {
	log := logger.GetBaseLogger()
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
		log.Infof("Handling incoming crawl request (%s)", reqID)
		ctx, span := tracer.Start(r.Context(), "handleItem", trace.WithAttributes(attribute.String("crawl_request", reqID)))
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

		progress.Update(ProgressUpdateMessagePackage{
			context.WithoutCancel(ctx),
			ProgressUpdateMessage{
				PROGRESS_STAGE_NAME,
				PROGRESS_STATE_QUEUED_FOR_CRAWLING,
				reqID,
				req.URL,
			},
		})
		workQueue.PublishURL(context.WithoutCancel(ctx), reqID, req.URL, cconf, whconf)

		result := &APIResponse{
			CrawlRequestID: reqID,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}).Methods("POST")
}
