package main

type APIRequest struct {
	// We accept either a valid UUID as a string, or as an integer. If left
	// empty, we'll generate one.
	Run           string         `json:"run_uuid"`
	URL           string         `json:"url"`
	Domains       []string       `json:"domains"`
	WebhookConfig *WebhookConfig `json:"webhook"`

	// If true, we'll ignore robots.txt check on the host.
	SkipRobots bool `json:"skip_robots"`

	// If true we'll not try to fetch the sitemap.xml file.
	SkipSitemap bool `json:"skip_sitemap"`
}

type APIResponse struct {
	Run uint32 `json:"run_uuid"`
}

type APIError struct {
	Message string `json:"message"`
}
