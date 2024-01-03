package main

import (
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	xrate "golang.org/x/time/rate"
)

var (
	redisLimiter   *redis_rate.Limiter
	memoryLimiters map[string]*xrate.Limiter
)

type LimiterAllowFn func(url string) (ok bool, retryAfter time.Duration, err error)

func CreateLimiter(ctx context.Context, redis *redis.Client, rate time.Duration) LimiterAllowFn {
	if redis != nil {
		log.Print("Using distributed rate limiter...")
		redisLimiter = redis_rate.NewLimiter(redis)

		return func(url string) (bool, time.Duration, error) {
			host := GetHostFromURL(url)
			res, err := redisLimiter.Allow(ctx, host, redis_rate.PerSecond(int(rate.Seconds())))

			if err != nil {
				log.Printf("Rate limit exceeded for host (%s)", host)
			}
			return err == nil, res.RetryAfter, err
		}
	}
	log.Print("Using in-memory rate limiter...")

	memoryLimiters = make(map[string]*xrate.Limiter)

	return func(url string) (bool, time.Duration, error) {
		host := GetHostFromURL(url)

		var memoryLimiter *xrate.Limiter
		if v, ok := memoryLimiters[host]; ok {
			memoryLimiter = v
		} else {
			memoryLimiter = xrate.NewLimiter(xrate.Every(rate), 1)
			memoryLimiters[host] = memoryLimiter
		}

		r := memoryLimiter.Reserve()

		if !r.OK() {
			log.Printf("Rate limit exceeded for host (%s)", host)
		}
		return r.OK(), r.Delay(), nil
	}
}

func GetHostFromURL(u string) string {
	p, _ := url.Parse(u)
	return strings.TrimPrefix(p.Hostname(), "www.")
}
