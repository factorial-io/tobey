package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log/slog"
	"net/http"
	"tobey/internal/ctrlq"
	"tobey/internal/result"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func setupRoutes(runs *RunManager, queue ctrlq.VisitWorkQueue, rr result.Reporter, progress ProgressReporter) http.Handler {
	apirouter := http.NewServeMux()

	apirouter.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		fmt.Fprint(w, "Hello from Tobey.")
	})

	apirouter.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()

		slog.Debug("Handling incoming request for crawl run...")

		// The context of the HTTP request might contain OpenTelemetry information,
		// i.e. SpanID or TraceID. If this is the case the line below creates
		// a sub span. Otherwise we'll start a new root span here.
		reqctx, span := tracer.Start(r.Context(), "receive_crawl_request")
		// This ends the very first span in handling the crawl run. It ends the HTTP handling span.
		defer span.End()

		w.Header().Set("Content-Type", "application/json")

		var req APIRequest
		if bytes.HasPrefix(body, []byte("http://")) || bytes.HasPrefix(body, []byte("https://")) {
			// Support both single URL and newline-delimited URLs in plaintext for minimalism.
			urls := bytes.Split(bytes.TrimSpace(body), []byte("\n"))

			for _, url := range urls {
				req.URLs = append(req.URLs, string(bytes.TrimSpace(url)))
			}
		} else {
			err := json.Unmarshal(body, &req)
			if err != nil {
				slog.Error("Failed to parse incoming JSON.", "error", err)

				result := &APIError{
					Message: fmt.Sprintf("%s", err),
				}
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(result)
				return
			}
		}

		if ok, _ := req.Validate(); !ok {
			result := &APIError{
				Message: "Invalid request.",
			}
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(result)
			return
		}

		run := &Run{
			SerializableRun: SerializableRun{
				ID:       req.GetRun(),
				Metadata: req.RunMetadata,

				URLs: req.GetURLs(true),

				AuthConfigs: req.GetAuthConfigs(),

				AllowDomains: req.GetAllowDomains(),
				IgnorePaths:  req.GetIgnorePaths(),

				UserAgent: req.GetUserAgent(),

				ResultReporterDSN: req.ResultReporterDSN,
			},
		}

		// Ensure we make the run configuration available in the store, before
		// we start publishing to the work queue.
		runs.Add(reqctx, run)

		go run.Start(reqctx, queue, rr, progress, req.GetURLs(true))

		result := &APIResponse{
			Run: run.ID,
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	})

	if UseMetrics {
		apirouter.Handle("GET /metrics", promhttp.Handler())
	}

	return otelhttp.NewHandler(apirouter, "get_new_request")
}

// setupHealthcheckRoutes sets up a healthcheck route. It is common
// that a Visitor dies when they are fetching from a misbehaving website.
func setupHealthcheckRoutes(vpool *VisitorPool) http.Handler {
	hcrouter := http.NewServeMux()

	hcrouter.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		r.Body.Close()

		ok, err := vpool.IsHealthy()
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			if err != nil {
				fmt.Fprintf(w, "Visitor Pool unhealthy: %s", err)
			} else {
				fmt.Fprint(w, "Visitor Pool unhealthy.")
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	return hcrouter
}
