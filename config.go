package main

type CollectorConfig struct {
	Root           string
	AllowedDomains []string
}

type WebhookConfig struct {
	Endpoint string      `json:"endpoint"`
	Data     interface{} `json:"data"` // Accept arbitrary data here.
}
