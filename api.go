// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"slices"

	"github.com/google/uuid"
)

const (
	AuthMethodBasic = "basic"
)

type AuthConfig struct {
	Host   *Host  `json:"host"`
	Method string `json:"method"`

	// If method is "basic"
	Username string `json:"username"`
	Password string `json:"password"`
}

// GetHeader returns the value of the Authorization header for the given
// authentication configuration.
func (ac *AuthConfig) GetHeader() (string, bool) {
	switch ac.Method {
	case AuthMethodBasic:
		token := fmt.Sprintf("%s:%s", ac.Username, ac.Password)
		token = base64.StdEncoding.EncodeToString([]byte(token))

		return fmt.Sprintf("Basic %s", token), true
	default:
		slog.Warn("Unknown auth method.", "method", ac.Method)
		return "", false
	}
}

// Hash returns a hash of the authentication configuration. This is used to
// uniquely identify the configuration.
func (ac *AuthConfig) Hash() []byte {
	return sha256.New().Sum([]byte(fmt.Sprintf("%#v", ac)))
}

// Matches checks if this AuthConfig should be used for the given Host.
func (ac *AuthConfig) Matches(h *Host) bool {
	return h.Name == ac.Host.Name && h.Port == ac.Host.Port
}

type APIRequest struct {
	// We accept either a valid UUID as a string, or as an integer. If left
	// empty, we'll generate one.
	Run string `json:"run_uuid"`

	// Metadata associated with this run that will be included in all results
	RunMetadata interface{} `json:"run_metadata,omitempty"`

	URL  string   `json:"url"`
	URLs []string `json:"urls"`

	AliasedDomains []string `json:"aliases"`
	IgnorePaths    []string `json:"ignores"`

	UserAgent string `json:"ua"`

	// A list of authentication configurations, that are used in the run.
	AuthConfigs []*AuthConfig `json:"auth"`

	// The DSN of the result reporter to use as dynamic configuration.
	ResultReporterDSN string `json:"result_reporter_dsn"`
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
			p, err := url.Parse(u)
			if err != nil {
				slog.Warn("Failed to parse URL while cleaning, skipping.", "url", u, "error", err)
				slices.Delete(urls, i, i+1)
				continue
			}
			p.User = nil
			urls[i] = p.String()
		}
	}
	return urls
}

func (req *APIRequest) GetAllowDomains() []string {
	var domains []string

	// The domains of the targets are allways allowed.
	for _, u := range req.GetURLs(false) {
		p, err := url.Parse(u)
		if err != nil {
			slog.Error("Failed to parse URL from request, not allowing that domain.", "url", u, "error", err)
			continue
		}
		domains = append(domains, p.Hostname())
	}

	if req.AliasedDomains != nil {
		domains = append(domains, req.AliasedDomains...)
	}

	return domains
}

func (req *APIRequest) GetIgnorePaths() []string {
	var paths []string

	if req.IgnorePaths == nil {
		return paths
	}
	for _, p := range req.IgnorePaths {
		paths = append(paths, p)
	}
	return paths
}

func (req *APIRequest) GetAuthConfigs() []*AuthConfig {
	var configs []*AuthConfig

	if req.AuthConfigs != nil {
		configs = req.AuthConfigs
	}

	for _, u := range req.GetURLs(false) {
		p, err := url.Parse(u)
		if err != nil {
			slog.Warn("Failed to parse URL while building auth config, skipping.", "url", u, "error", err)
			continue
		}
		if p.User != nil {
			pass, _ := p.User.Password()

			config := &AuthConfig{
				Method:   "basic",
				Host:     NewHostFromURL(p),
				Username: p.User.Username(),
				Password: pass,
			}
			configs = append(configs, config)
		}
	}
	return configs
}

func (req *APIRequest) GetUserAgent() string {
	if req.UserAgent != "" {
		return req.UserAgent
	}
	return UserAgent
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
