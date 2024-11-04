// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

// The rate limit optimizer takes samples and optimizer the rate limit, keeping
// it as low as possible to maximize throughput without overhelming a host.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

const (
	// MinHostRPS specifies the minimum number of requests per
	// second that are executed against a single host.
	MinHostRPS float64 = 1

	// MaxHostRPS specifies the maximum number of requests per
	// second that are exectuted against a single host.
	MaxHostRPS float64 = 50
)

// newRateLimitByHeaders returns a new rate limit according to rate limiting headers. It ensures
// that the new rate limit is at least MinHostRPS.
func newRateLimitByHeaders(cur float64, hdr *http.Header) (bool, float64) {
	if v := hdr.Get("X-RateLimit-Limit"); v != "" {
		parsed, _ := strconv.Atoi(v)
		desired := max(float64(parsed/60/60), MinHostRPS) // Convert to per second, is per hour.

		return int(desired) != int(cur), desired
	}
	return false, 0
}

// newRateLimitByLatency returns a new rate limit according to the latency of
// a request/response and status / error. It implements an algorithm to adjust
// the rate limiter, to ensure maximum througput within the bounds set by the
// rate limiter, while not overwhelming the target. The algorithm will adjust
// the rate limiter but not go below MinRequestsPerSecondPerHost and not above
// MaxRequestsPerSecondPerHost.
func newRateLimitByLatency(cur float64, statusCode int, err error, d time.Duration) (bool, float64) {
	var desired float64

	// If we have a status code that the target is already overwhelmed, we don't
	// increase the rate limit. We also don't adjust the rate limit as we
	// assume on a 429 this has already been done using TakeRateLimitHeaders

	switch {
	case statusCode == 429:
		fallthrough
	case statusCode == 503:
		return false, 0
	case statusCode == 500 || errors.Is(err, context.DeadlineExceeded):
		// The server is starting to behave badly. We should slow down.
		// Half the rate limit, capped at MinRequestsPerSecondPerHost.
		if cur/2 < MinHostRPS {
			desired = MinHostRPS
		} else {
			desired = cur / 2
		}

		return int(desired) != int(cur), desired
	}

	if d == 0 {
		return false, 0
	}

	// The higher the latency comes close to 1s the more we want to decrease the
	// rate limit, capped at MinHostRPS. The lower the latency comes clost to 0s
	// the more we want to increase the rate limit, capped at MaxHostRPS.

	latency := min(1, d.Seconds()) // Everything above 1s is considered slow.
	desired = MaxHostRPS*(1-latency) + MinHostRPS*latency

	return int(desired) != int(cur), desired
}
