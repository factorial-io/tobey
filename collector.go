package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/gocolly/colly/v2"
	"github.com/redis/go-redis/v9"
)

type EnqueueFn func(string) error
type CollectFn func(*colly.Response)

type CollectorConfig struct {
	Root           string
	AllowedDomains []string
}

func DeriveCollectorConfigFromAPIRequest(req *APIRequest) (*CollectorConfig, error) {
	conf := &CollectorConfig{}

	url, err := url.Parse(req.URL)
	if err != nil {
		return conf, err
	}
	nakedDomain := strings.TrimPrefix(url.Hostname(), "www.")

	conf.Root = strings.TrimRight(req.URL, "/")

	conf.AllowedDomains = append(conf.AllowedDomains, nakedDomain, fmt.Sprintf("www.%s", nakedDomain))
	conf.AllowedDomains = append(conf.AllowedDomains, req.Domains...)

	return conf, nil
}

func CreateCollector(ctx context.Context, reqID uint32, redis *redis.Client, domains []string) *colly.Collector {
	c := colly.NewCollector(
		colly.UserAgent(fmt.Sprintf("Website Standards Bot/2.0")),
		colly.CacheDir("./cache"),
		colly.AllowedDomains(domains...),
		colly.ID(reqID),
		// colly.Debugger(&debug.LogDebugger{}),
	)
	c.CheckHead = true

	// TODO: Replace standard client with a retryable client.
	// c.WithTransport(NewThrottledTransport(reqID, redis))

	// SetStorage must come after SetClient as the storage's cookie jar
	// will be mounted on the client by SetStorage.
	if redis != nil {
		// Collectors of all nodes will persist and share visits /
		// caching data via the Redis backend.
		s := NewRedisStorage(ctx, redis, "collector")

		if err := c.SetStorage(s); err != nil {
			panic(err)
		}
	} else {
		// Use built-in memory backend
	}

	return c
}

func CollectorAttachCallbacks(c *colly.Collector, enqueue EnqueueFn, collect CollectFn) {
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		enqueue(e.Request.AbsoluteURL(e.Attr("href")))
	})

	c.OnScraped(func(res *colly.Response) {
		collect(res)
	})

	// Resolve linked sitemaps.
	c.OnXML("//sitemap/loc", func(e *colly.XMLElement) {
		enqueue(e.Text)
	})

	c.OnXML("//urlset/url/loc", func(e *colly.XMLElement) {
		enqueue(e.Text)
	})
}

// IsDomainAllowed has been extracted from colly.Collector, so that we can check
// early if following actions, i.e. rate limiting should be performed at all.
func IsDomainAllowed(domain string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, d := range allowed {
		if d == domain {
			return true
		}
	}
	return false
}
