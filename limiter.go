// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	xrate "golang.org/x/time/rate"
)

var (
	// memoryLimiters is a map of hostnames to rate limiters.
	memoryLimiters     map[string]*xrate.Limiter
	memoryLimitersLock sync.Mutex

	redisLimiter *redis_rate.Limiter
)

type LimiterAllowFn func(url string) (hasReservation bool, retryAfter time.Duration, err error)

func CreateLimiter(ctx context.Context, redis *redis.Client, ratePerS int) LimiterAllowFn {
	host := func(u string) string {
		p, _ := url.Parse(u)
		return strings.TrimPrefix(p.Hostname(), "www.")
	}

	if redis != nil {
		slog.Debug("Using distributed rate limiter...")
		redisLimiter = redis_rate.NewLimiter(redis)

		return func(url string) (bool, time.Duration, error) {
			res, err := redisLimiter.Allow(ctx, host(url), redis_rate.PerSecond(ratePerS))
			return res.Allowed > 0, res.RetryAfter, err
		}
	}

	slog.Debug("Using in-memory rate limiter...")
	memoryLimiters = make(map[string]*xrate.Limiter)

	return func(url string) (bool, time.Duration, error) {
		key := host(url)

		var memoryLimiter *xrate.Limiter
		memoryLimitersLock.Lock()
		if v, ok := memoryLimiters[key]; ok {
			memoryLimiter = v
		} else {
			memoryLimiter = xrate.NewLimiter(xrate.Limit(ratePerS), 1)
			memoryLimiters[key] = memoryLimiter
		}
		memoryLimitersLock.Unlock()

		r := memoryLimiter.Reserve()
		return r.OK(), r.Delay(), nil
	}
}
