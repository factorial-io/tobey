// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
)

type AuthConfig struct {
	Host   string `json:"host"`
	Method string `json:"method"`

	// If method is "basic"
	Username string `json:"username"`
	Password string `json:"password"`
}

// GetHeader returns the value of the Authorization header for the given
// authentication configuration.
func (auth *AuthConfig) GetHeader() (string, bool) {
	switch auth.Method {
	case "basic":
		token := fmt.Sprintf("%s:%s", auth.Username, auth.Password)
		token = base64.StdEncoding.EncodeToString([]byte(token))

		return fmt.Sprintf("Basic %s", token), true
	default:
		slog.Warn("Unknown auth method.", "method", auth.Method)
		return "", false
	}
}

type APIRequest struct {
	// We accept either a valid UUID as a string, or as an integer. If left
	// empty, we'll generate one.
	Run string `json:"run_uuid"`

	URL  string   `json:"url"`
	URLs []string `json:"urls"`

	AllowedDomains []string `json:"domains"`

	WebhookConfig *WebhookConfig `json:"webhook"`

	// If true, we'll bypass the robots.txt check, however we'll still
	// download the file to look for sitemaps.
	SkipRobots bool `json:"skip_robots"`

	// If true we'll not use any sitemaps found automatically, only those that
	// have been explicitly provided.
	SkipSitemapDiscovery bool `json:"skip_sitemap_discovery"`

	// A list of authentication configurations, that are used in the run.
	AuthConfigs []*AuthConfig `json:"auth"`
}

func (req *APIRequest) GetRun() (string, error) {
	if req.Run != "" {
		v, err := uuid.Parse(req.Run)
		if err != nil {
			return "", err
		}
		return v.String(), nil
	}
	return uuid.New().String(), nil
}

func (req *APIRequest) GetURLs(clean bool) []string {
	var urls []string
	if req.URL != "" {
		urls = append(urls, req.URL)
	}
	if req.URLs != nil {
		urls = append(urls, req.URLs...)
	}

	if clean {
		// Remove basic auth information from each URL. Otherwise net/http will
		// use the information to authenticate the request. But we want to do this
		// explicitly.
		for i, u := range urls {
			p, _ := url.Parse(u)
			p.User = nil
			urls[i] = p.String()
		}
	}
	return urls
}

func (req *APIRequest) GetAllowedDomains() []string {
	// Ensure at least the URL host is in allowed domains.
	var domains []string
	if req.AllowedDomains != nil {
		domains = req.AllowedDomains
	} else {
		p, _ := url.Parse(req.URL)
		domains = append(domains, p.Hostname())
	}
	return domains
}

func (req *APIRequest) GetAuthConfigs() []*AuthConfig {
	var configs []*AuthConfig

	if req.AuthConfigs != nil {
		configs = req.AuthConfigs
	}

	for _, u := range req.GetURLs(false) {
		p, _ := url.Parse(u)

		if p.User != nil {
			pass, _ := p.User.Password()

			config := &AuthConfig{
				Method:   "basic",
				Host:     p.Hostname(),
				Username: p.User.Username(),
				Password: pass,
			}
			configs = append(configs, config)
		}
	}
	return configs
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
	if req.WebhookConfig != nil {
		if req.WebhookConfig.Endpoint == "" {
			return false
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
