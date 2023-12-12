package main

import (
	"github.com/gocolly/colly"
)

// Result is sent to Out, must be serialiazable.
type Result struct {
	Site     *Site
	Request  *colly.Request
	Response *colly.Response
	Callback interface{}
}

type Out interface {
	Send(s *Site, res *colly.Response) error
	Close() error
}

func MustStartOutFromEnv() Out {
	return &NoopOut{}
}

type NoopOut struct{}

func (p *NoopOut) Send(s *Site, res *colly.Response) error {

	// log.Printf("Got result: for %v with %d", s, res.StatusCode)
	return nil
}

func (p *NoopOut) Close() error {
	return nil
}
