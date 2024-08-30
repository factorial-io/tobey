// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics exposed for collection by Prometheus.
var (
	PromVisitTxns = promauto.NewCounter(prometheus.CounterOpts{
		Name: "visits_txns_total",
		Help: "The total number of visits.",
	})
)

// High Frequency metrics, these should be mutated through atomic operations.
var (
	PulseVisitTxns int32 // A gauge that is reset every second.
)

// startPulse starts a go routine that pushes updates to the pulse endpoint.
func startPulse(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)

	go func() {
		for {
			select {
			case <-ticker.C:
				v := atomic.LoadInt32(&PulseVisitTxns)
				atomic.StoreInt32(&PulseVisitTxns, 0)

				rb := strings.NewReader(strconv.Itoa(int(v)))
				http.Post(PulseEndpoint+"/rps", "text/plain", rb)
			}
		}
	}()
}
