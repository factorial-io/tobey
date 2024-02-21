package main

type CollectorConfig struct {
	Run            uint32
	AllowedDomains []string
}

type WebhookConfig struct {
	Endpoint string      `json:"endpoint"`
	Data     interface{} `json:"data"` // Accept arbitrary data here.
}
