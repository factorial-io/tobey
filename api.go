package main

import (
	"net/url"

	"github.com/google/uuid"
)

type APIRequest struct {
	// We accept either a valid UUID as a string, or as an integer. If left
	// empty, we'll generate one.
	Run           string         `json:"run_uuid"`
	URL           string         `json:"url"`
	URLs          []string       `json:"urls"`
	Domains       []string       `json:"domains"`
	WebhookConfig *WebhookConfig `json:"webhook"`

	// If true, we'll ignore robots.txt check on the host.
	SkipRobots bool `json:"skip_robots"`

	// If true we'll not try to fetch the sitemap.xml file.
	SkipSitemap bool `json:"skip_sitemap"`
}

func (req *APIRequest) Validate() bool {
	if req.Run != "" {
		_, err := uuid.Parse(req.Run)
		if err != nil {
			return false
		}
	}

	if req.URL != "" {
		_, err := url.ParseRequestURI(req.URL)
		if err != nil {
			return false
		}
	}
	if req.URLs != nil {
		for _, u := range req.URLs {
			_, err := url.ParseRequestURI(u)
			if err != nil {
				return false
			}
		}
	}
	return true
}

type APIResponse struct {
	Run string `json:"run_uuid"`
}

type APIError struct {
	Message string `json:"message"`
}

type CloudEventJson struct {
	Specversion     string `json:"specversion"`
	Event_type      string `json:"type"`
	Source          string `json:"source"`
	Subject         string `json:"subject"`
	Id              string `json:"id"`
	Time            string `json:"time"`
	Datacontenttype string `json:"datacontenttype"`
	Data            string `json:"data"`
}
