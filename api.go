package main

type APIRequest struct {
	Site     *Site  `json:"site"`
	SiteRoot string `json:"url"` // May be used instead of Site. The root URL of the site to crawl.

	Webhook struct {
		URL  string      `json:"url"`
		Data interface{} `json:"data"` // Accept arbitrary data here.
	} `json:"webhook"`
}

type APIResponse struct {
	Site *Site `json:"site"`
}

type APIError struct {
	Message string `json:"message"`
}
