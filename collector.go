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
		// TODO: Get better bot strings here: https://developers.google.com/search/docs/crawling-indexing/overview-google-crawlers
		//		colly.UserAgent(fmt.Sprintf("Website Standards Bot/2.0"))),
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

	c.OnScraped(func(res *colly.Response) {
		// log.Printf("Scraped URL (%s)", res.Ctx.Get("url"))
		o.Send(s, res)
	})

	// c.OnXML("//urlset/url/loc", func(e *colly.XMLElement) {
	// 	q.PublishURL(s, e.Text)
	// })

	return c
}
