package main

import (
	"fmt"
	"path/filepath"

	"github.com/gocolly/colly"
)

// The directory that holds response cache, i.e. /tmp/cache. When missing the directory
// will be automatically created. Defaults to ./cache.
var CacheDir string = "./cache"

func NewCollectorForSite(q WorkQueue, o Out, s *Site) *colly.Collector {
	c := colly.NewCollector(
		colly.Async(true),
		colly.CacheDir(filepath.Join(CacheDir, s.Domain)),
		//	colly.Debugger(&debug.LogDebugger{}),
		colly.AllowedDomains(s.Domain, fmt.Sprintf("www.%s", s.Domain)), // Constrain to requested domain
	)

	//	c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: 2}) // TODO: Hook into control over overall parallelism.

	// Find and visit all links
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		// log.Printf("Link found: %q -> %s\n", e.Text, link)
		//		log.Printf("Found URL (%s)", e.Request.AbsoluteURL(link))
		q.PublishURL(s, e.Request.AbsoluteURL(link))
	})

	c.OnRequest(func(r *colly.Request) {
		//		log.Printf("Visiting URL (%s)...", r.URL)
		r.Ctx.Put("url", r.URL.String())
	})
	c.OnResponse(func(r *colly.Response) {
		//		log.Printf("Visited URL (%s)", r.Ctx.Get("url"))
	})
	c.OnScraped(func(res *colly.Response) {
		// log.Printf("Scraped URL (%s)", res.Ctx.Get("url"))
		o.Send(s, res)
	})

	return c
}
