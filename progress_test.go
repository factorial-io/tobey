package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	/*
		ctx := context.Background()
		webhookQueue := make(chan WebhookPayload, 100)

		//go ManageProcessWebhooks(ctx, webhookQueue)

			element := WebhookPayload{}
			webhookQueue <- element

			webhookQueue <- element
			webhookQueue <- element
			webhookQueue <- element*/

}

func ATestClientUpperCase(t *testing.T) {

	//error
	//invalid https
	// different codes
	ctx := context.Background()
	ctx, cancelCtx := context.WithCancel(ctx)

	webhookQueue := make(chan WebhookPayload, 100)

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("test it")
		fmt.Fprintf(w, "nice")
	}))
	defer svr.Close()

	webhook := NewProcessWebhooksManager()
	webhook.Start(ctx, webhookQueue)

	element := WebhookPayload{Url: svr.URL}
	webhookQueue <- element
	cancelCtx()
	time.Sleep(time.Second * 10)

}

func ATestClientUpperCase4(t *testing.T) {

	//error
	//invalid https
	// different codes
	//close channel

	ctx := context.Background()
	ctx, cancelCtx := context.WithCancel(ctx)
	webhookQueue := make(chan WebhookPayload, 100)

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("test it")
		fmt.Fprintf(w, "nice")
	}))
	defer svr.Close()

	webhook := NewProcessWebhooksManager()
	webhook.Start(ctx, webhookQueue)

	element := WebhookPayload{Url: svr.URL}
	webhookQueue <- element
	close(webhookQueue)
	cancelCtx()
	time.Sleep(time.Hour * 10)

}

func ATestClientUpperCase2(t *testing.T) {

	//error
	//invalid https
	// different codes

	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	ctx := context.Background()
	webhookQueue := make(chan WebhookPayload, 100)

	webhook := ProcessWebhooksManager{
		&client,
	}
	webhook.Start(ctx, webhookQueue)

	svr := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `
        {
          "id": "123",
          "email": "blah",
          "first_name": "Ben",
          "last_name": "Botwin"
        }
        `

		w.Write([]byte(resp))
	}))
	defer svr.Close()

	element := WebhookPayload{Url: svr.URL}
	webhookQueue <- element
	ctx.Done()
	time.Sleep(time.Second * 10)

}
