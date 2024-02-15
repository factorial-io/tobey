package main

type APIRequest struct {
	URL           string         `json:"url"`
	Domains       []string       `json:"domains"`
	WebhookConfig *WebhookConfig `json:"webhook"`
}

type APIResponse struct {
	CrawlRequestID string `json:"crawl_request_id"`
}

type APIError struct {
	Message string `json:"message"`
}
