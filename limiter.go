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
	redisLimiter       *redis_rate.Limiter
	memoryLimiters     map[string]*xrate.Limiter
	memoryLimitersLock sync.Mutex
)

type LimiterAllowFn func(url string) (ok bool, retryAfter time.Duration, err error)

func CreateLimiter(ctx context.Context, redis *redis.Client, rate time.Duration) LimiterAllowFn {
	if redis != nil {
		slog.Debug("Using distributed rate limiter...")
		redisLimiter = redis_rate.NewLimiter(redis)

		return func(url string) (bool, time.Duration, error) {
			host := GetHostFromURL(url)
			res, err := redisLimiter.Allow(ctx, host, redis_rate.PerSecond(int(rate.Seconds())))

			if err != nil {
				slog.Info("Host rate limit exceeded.", "host", host)
			}
			return err == nil, res.RetryAfter, err
		}
	}
	slog.Debug("Using in-memory rate limiter...")

	memoryLimiters = make(map[string]*xrate.Limiter)

	return func(url string) (bool, time.Duration, error) {
		host := GetHostFromURL(url)

		var memoryLimiter *xrate.Limiter
		memoryLimitersLock.Lock()
		if v, ok := memoryLimiters[host]; ok {
			memoryLimiter = v
		} else {
			memoryLimiter = xrate.NewLimiter(xrate.Every(rate), 1)
			memoryLimiters[host] = memoryLimiter
		}
		memoryLimitersLock.Unlock()

		r := memoryLimiter.Reserve()

		if !r.OK() {
			slog.Info("Host rate limit exceeded.", "host", host)
		}
		return r.OK(), r.Delay(), nil
	}
}

func GetHostFromURL(u string) string {
	p, _ := url.Parse(u)
	return strings.TrimPrefix(p.Hostname(), "www.")
}
