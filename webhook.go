package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gocolly/colly/v2"
)

type WebhookConfig struct {
	Endpoint string      `json:"endpoint"`
	Data     interface{} `json:"data"` // Accept arbitrary data here.
}

type WebhookPayload struct {
	Action string `json:"action"`

	Data interface{} `json:"data"` // Pass through arbitrary data here.

	// TODO: Figure out if we want to use "Standard Webhook" and/or if
	// we than want to nest all results data under Data as to prevent
	// collisions with Action and other fields.
	RequestURL   string `json:"request_url"`
	ResponseBody []byte `json:"response_body"` // Will be base64 encoded once marshalled.
}

type WebhookDispatcher struct {
	requests chan string
	wg       sync.WaitGroup
	client   *http.Client
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{client: &http.Client{}}
}

func (wd *WebhookDispatcher) Send(webhook *WebhookConfig, res *colly.Response) error {

	payload := &WebhookPayload{
		Action: "collector.response",

		// We pass through the data we received taking in the
		// initial crawl request, verbatim.
		Data: webhook,

		RequestURL:   res.Request.URL.String(),
		ResponseBody: res.Body[:100],
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	log.Printf("Sending webhook got for crawl response (%s), body length (%d)... %s", res.Request.URL, len(res.Body), webhook.Endpoint)

	req, err := http.NewRequest("POST", webhook.Endpoint, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	wd.wg.Add(1)
	go func() {
		err := backoff.RetryNotify(func() error {
			_, err := wd.client.Do(req)
			return err
		}, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 0), func(err error, t time.Duration) {
			log.Printf("Retrying to send webhook in %s: %s", t, err)
		})
		if err != nil {
			log.Printf("Sending webhook ultimately failed: %s", err)
		} else {
			// log.Printf("Webhook succesfully sent: %s", webhook.Endpoint)
		}
		wd.wg.Done()
	}()

	// log.Printf("Got result: for %v with %d", s, res.StatusCode)
	return nil
}

func (wd *WebhookDispatcher) Close() error {
	wd.wg.Wait()
	return nil
}
