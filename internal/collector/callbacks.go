// Based on colly HTTP scraping framework, Copyright 2018 Adam Tauber,
// originally licensed under the Apache License 2.0

package collector

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/antchfx/htmlquery"
	"github.com/antchfx/xmlquery"
)

// RequestCallback is a type alias for OnRequest callback functions
type RequestCallback func(*Request)

// ResponseHeadersCallback is a type alias for OnResponseHeaders callback functions
type ResponseHeadersCallback func(*Response)

// ResponseCallback is a type alias for OnResponse callback functions
type ResponseCallback func(*Response)

// HTMLCallback is a type alias for OnHTML callback functions
type HTMLCallback func(*HTMLElement)

// XMLCallback is a type alias for OnXML callback functions
type XMLCallback func(*XMLElement)

// ErrorCallback is a type alias for OnError callback functions
type ErrorCallback func(*Response, error)

// ScrapedCallback is a type alias for OnScraped callback functions
type ScrapedCallback func(*Response)

type htmlCallbackContainer struct {
	Selector string
	Function HTMLCallback
}

type xmlCallbackContainer struct {
	Query    string
	Function XMLCallback
}

// OnRequest registers a function. Function will be executed on every
// request made by the Collector
func (c *Collector) OnRequest(f RequestCallback) {
	if c.requestCallbacks == nil {
		c.requestCallbacks = make([]RequestCallback, 0, 4)
	}
	c.requestCallbacks = append(c.requestCallbacks, f)
}

// OnResponseHeaders registers a function. Function will be executed on every response
// when headers and status are already received, but body is not yet read.
//
// Like in OnRequest, you can call Request.Abort to abort the transfer. This might be
// useful if, for example, you're following all hyperlinks, but want to avoid
// downloading files.
//
// Be aware that using this will prevent HTTP/1.1 connection reuse, as
// the only way to abort a download is to immediately close the connection.
// HTTP/2 doesn't suffer from this problem, as it's possible to close
// specific stream inside the connection.
func (c *Collector) OnResponseHeaders(f ResponseHeadersCallback) {
	c.responseHeadersCallbacks = append(c.responseHeadersCallbacks, f)
}

// OnResponse registers a function. Function will be executed on every response
func (c *Collector) OnResponse(f ResponseCallback) {
	if c.responseCallbacks == nil {
		c.responseCallbacks = make([]ResponseCallback, 0, 4)
	}
	c.responseCallbacks = append(c.responseCallbacks, f)
}

// OnHTML registers a function. Function will be executed on every HTML
// element matched by the GoQuery Selector parameter.
// GoQuery Selector is a selector used by https://github.com/PuerkitoBio/goquery
func (c *Collector) OnHTML(goquerySelector string, f HTMLCallback) {
	if c.htmlCallbacks == nil {
		c.htmlCallbacks = make([]*htmlCallbackContainer, 0, 4)
	}
	c.htmlCallbacks = append(c.htmlCallbacks, &htmlCallbackContainer{
		Selector: goquerySelector,
		Function: f,
	})
}

// OnXML registers a function. Function will be executed on every XML
// element matched by the xpath Query parameter.
// xpath Query is used by https://github.com/antchfx/xmlquery
func (c *Collector) OnXML(xpathQuery string, f XMLCallback) {
	if c.xmlCallbacks == nil {
		c.xmlCallbacks = make([]*xmlCallbackContainer, 0, 4)
	}
	c.xmlCallbacks = append(c.xmlCallbacks, &xmlCallbackContainer{
		Query:    xpathQuery,
		Function: f,
	})
}

// OnHTMLDetach deregister a function. Function will not be execute after detached
func (c *Collector) OnHTMLDetach(goquerySelector string) {
	deleteIdx := -1
	for i, cc := range c.htmlCallbacks {
		if cc.Selector == goquerySelector {
			deleteIdx = i
			break
		}
	}
	if deleteIdx != -1 {
		c.htmlCallbacks = append(c.htmlCallbacks[:deleteIdx], c.htmlCallbacks[deleteIdx+1:]...)
	}
}

// OnXMLDetach deregister a function. Function will not be execute after detached
func (c *Collector) OnXMLDetach(xpathQuery string) {
	deleteIdx := -1
	for i, cc := range c.xmlCallbacks {
		if cc.Query == xpathQuery {
			deleteIdx = i
			break
		}
	}
	if deleteIdx != -1 {
		c.xmlCallbacks = append(c.xmlCallbacks[:deleteIdx], c.xmlCallbacks[deleteIdx+1:]...)
	}
}

// OnError registers a function. Function will be executed if an error
// occurs during the HTTP request.
func (c *Collector) OnError(f ErrorCallback) {
	if c.errorCallbacks == nil {
		c.errorCallbacks = make([]ErrorCallback, 0, 4)
	}
	c.errorCallbacks = append(c.errorCallbacks, f)
}

// OnScraped registers a function. Function will be executed after
// OnHTML, as a final part of the scraping.
func (c *Collector) OnScraped(f ScrapedCallback) {
	if c.scrapedCallbacks == nil {
		c.scrapedCallbacks = make([]ScrapedCallback, 0, 4)
	}
	c.scrapedCallbacks = append(c.scrapedCallbacks, f)
}

func (c *Collector) handleOnRequest(r *Request) {
	for _, f := range c.requestCallbacks {
		f(r)
	}
}

func (c *Collector) handleOnResponse(r *Response) {
	for _, f := range c.responseCallbacks {
		f(r)
	}
}

func (c *Collector) handleOnResponseHeaders(r *Response) {
	for _, f := range c.responseHeadersCallbacks {
		f(r)
	}
}

func (c *Collector) handleOnHTML(resp *Response) error {
	if len(c.htmlCallbacks) == 0 || !strings.Contains(strings.ToLower(resp.Headers.Get("Content-Type")), "html") {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewBuffer(resp.Body))
	if err != nil {
		return err
	}
	if href, found := doc.Find("base[href]").Attr("href"); found {
		u, err := urlParser.ParseRef(resp.Request.URL.String(), href)
		if err == nil {
			baseURL, err := url.Parse(u.Href(false))
			if err == nil {
				resp.Request.baseURL = baseURL
			}
		}

	}
	for _, cc := range c.htmlCallbacks {
		i := 0
		doc.Find(cc.Selector).Each(func(_ int, s *goquery.Selection) {
			for _, n := range s.Nodes {
				e := NewHTMLElementFromSelectionNode(resp, s, n, i)
				i++
				cc.Function(e)
			}
		})
	}
	return nil
}

func (c *Collector) handleOnXML(resp *Response) error {
	if len(c.xmlCallbacks) == 0 {
		return nil
	}
	contentType := strings.ToLower(resp.Headers.Get("Content-Type"))
	isXMLFile := strings.HasSuffix(strings.ToLower(resp.Request.URL.Path), ".xml") || strings.HasSuffix(strings.ToLower(resp.Request.URL.Path), ".xml.gz")
	if !strings.Contains(contentType, "html") && (!strings.Contains(contentType, "xml") && !isXMLFile) {
		return nil
	}

	if strings.Contains(contentType, "html") {
		doc, err := htmlquery.Parse(bytes.NewBuffer(resp.Body))
		if err != nil {
			return err
		}
		if e := htmlquery.FindOne(doc, "//base"); e != nil {
			for _, a := range e.Attr {
				if a.Key == "href" {
					baseURL, err := resp.Request.URL.Parse(a.Val)
					if err == nil {
						resp.Request.baseURL = baseURL
					}
					break
				}
			}
		}

		for _, cc := range c.xmlCallbacks {
			for _, n := range htmlquery.Find(doc, cc.Query) {
				e := NewXMLElementFromHTMLNode(resp, n)
				cc.Function(e)
			}
		}
	} else if strings.Contains(contentType, "xml") || isXMLFile {
		doc, err := xmlquery.Parse(bytes.NewBuffer(resp.Body))
		if err != nil {
			return err
		}

		for _, cc := range c.xmlCallbacks {
			xmlquery.FindEach(doc, cc.Query, func(i int, n *xmlquery.Node) {
				e := NewXMLElementFromXMLNode(resp, n)
				cc.Function(e)
			})
		}
	}
	return nil
}

func (c *Collector) handleOnError(response *Response, err error, request *Request, ctx *Context) error {
	if err == nil && response.StatusCode == 304 {
		return nil
	}
	if err == nil && (c.ParseHTTPErrorResponse || response.StatusCode < 203) {
		return nil
	}
	if err == nil && response.StatusCode >= 203 {
		err = errors.New(http.StatusText(response.StatusCode))
	}
	if response == nil {
		response = &Response{
			Request: request,
			Ctx:     ctx,
		}
	}
	if response.Request == nil {
		response.Request = request
	}
	if response.Ctx == nil {
		response.Ctx = request.Ctx
	}
	for _, f := range c.errorCallbacks {
		f(response, err)
	}
	return err
}

func (c *Collector) handleOnScraped(r *Response) {
	for _, f := range c.scrapedCallbacks {
		f(r)
	}
}
