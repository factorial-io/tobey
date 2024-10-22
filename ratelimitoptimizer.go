package main

// The rate limit optimizer takes samples and optimizer the rate limit, keeping
// it as low as possible to maximize throughput without overhelming a host.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	xrate "golang.org/x/time/rate"
)

func (wq *MemoryVisitWorkQueue) TakeRateLimitHeaders(ctx context.Context, url string, hdr *http.Header) {
	hq := wq.lazyHostQueue(url)

	if v := hdr.Get("X-RateLimit-Limit"); v != "" {
		cur := float64(hq.Limiter.Limit())

		parsed, _ := strconv.Atoi(v)
		desired := max(float64(parsed/60/60), MinHostRPS) // Convert to per second, is per hour.

		if int(desired) != int(cur) {
			slog.Debug("Work Queue: Rate limit updated.", "host", hq.Name, "now", desired, "change", desired-cur)
			hq.Limiter.SetLimit(xrate.Limit(desired))
		}
		hq.IsAdaptive = false
	}
}

// TakeSample implements an algorithm to adjust the rate limiter, to ensure maximum througput within the bounds
// set by the rate limiter, while not overwhelming the target. The algorithm will adjust the rate limiter
// but not go below MinRequestsPerSecondPerHost and not above MaxRequestsPerSecondPerHost.
func (wq *MemoryVisitWorkQueue) TakeSample(ctx context.Context, url string, statusCode int, err error, d time.Duration) {
	hq := wq.lazyHostQueue(url)

	cur := float64(hq.Limiter.Limit())
	var desired float64

	// If we have a status code that the target is already overwhelmed, we don't
	// increase the rate limit. We also don't adjust the rate limit as we
	// assume on a 429 this has already been done using TakeRateLimitHeaders

	switch {
	case statusCode == 429:
		fallthrough
	case statusCode == 503:
		return
	case statusCode == 500 || errors.Is(err, context.DeadlineExceeded):
		// The server is starting to behave badly. We should slow down.
		// Half the rate limit, capped at MinRequestsPerSecondPerHost.
		if cur/2 < MinHostRPS {
			desired = MinHostRPS
		} else {
			desired = cur / 2
		}

		if int(desired) != int(cur) && hq.IsAdaptive {
			slog.Debug("Work Queue: Lowering pressure on host.", "host", hq.Name, "now", desired, "change", desired-cur)
			hq.Limiter.SetLimit(xrate.Limit(desired))
		}
		return
	}

	if d == 0 {
		return
	}

	// The higher the latency comes close to 1s the more we want to decrease the
	// rate limit, capped at MinHostRPS. The lower the latency comes clost to 0s
	// the more we want to increase the rate limit, capped at MaxHostRPS.

	latency := min(1, d.Seconds()) // Everything above 1s is considered slow.
	desired = MaxHostRPS*(1-latency) + MinHostRPS*latency

	if int(desired) != int(cur) && hq.IsAdaptive {
		slog.Debug("Work Queue: Rate limit fine tuned.", "host", hq.Name, "now", desired, "change", desired-cur)
		hq.Limiter.SetLimit(xrate.Limit(desired))
	}
}
