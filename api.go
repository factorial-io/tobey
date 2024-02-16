package main

type APIRequest struct {
	// We accept either a valid UUID as a string, or as an integer. Or any other unsigned integer.
	RunID         string         `json:"run_id"`
	URL           string         `json:"url"`
	Domains       []string       `json:"domains"`
	WebhookConfig *WebhookConfig `json:"webhook"`
}

type APIResponse struct {
	RunID uint32 `json:"run_id"`
}

type APIError struct {
	Message string `json:"message"`
}
