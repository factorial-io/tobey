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
	"os"
	"path/filepath"
	"slices"
	"strings"

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

type Request struct {
	// We accept either a valid UUID as a string, or as an integer. If left
	// empty, we'll generate one.
	Run string `json:"run"`

	URL  string   `json:"url"`
	URLs []string `json:"urls"`

	AliasedDomains []string `json:"aliases"`
	IgnorePaths    []string `json:"ignores"`

	UserAgent string `json:"ua"`

	// A list of authentication configurations, that are used in the run.
	AuthConfigs []*AuthConfig `json:"auth"`
}

// GetRun returns the run's ID, if provided in the request. Please call req.Validate
// to make sure a provided ID is correctly formed.
func (req *Request) GetRun() string {
	if req.Run != "" {
		return uuid.MustParse(req.Run).String()
	}
	return uuid.New().String()
}

func (req *Request) GetURLs(clean bool) []string {
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
				urls = slices.Delete(urls, i, i+1)
				continue
			}
			p.User = nil
			urls[i] = p.String()
		}
	}
	return urls
}

func (req *Request) GetAllowDomains() []string {
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

func (req *Request) GetIgnorePaths() []string {
	if req.IgnorePaths == nil {
		return make([]string, 0)
	}
	return req.IgnorePaths
}

func (req *Request) GetAuthConfigs() []*AuthConfig {
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

func (req *Request) GetUserAgent() string {
	if req.UserAgent != "" {
		return req.UserAgent
	}
	return UserAgent
}

func (req *Request) Validate() (bool, []error) {
	var violations []error

	if req.Run != "" {
		_, err := uuid.Parse(req.Run)
		if err != nil {
			return false, append(violations, err)
		}
	}

	if req.URL != "" {
		_, err := url.ParseRequestURI(req.URL)
		if err != nil {
			return false, append(violations, err)
		}
	}
	if req.URLs != nil {
		for _, u := range req.URLs {
			_, err := url.ParseRequestURI(u)
			if err != nil {
				return false, append(violations, err)
			}
		}
	}
	return true, violations
}

type APIRequest struct {
	Request

	// Metadata associated with this run that will be included in all results
	RunMetadata interface{} `json:"run_metadata,omitempty"`

	// The DSN of the result reporter to use as dynamic configuration.
	ResultReporterDSN string `json:"result_reporter_dsn"`
}

type APIResponse struct {
	Run string `json:"run"`
}

type APIError struct {
	Message string `json:"message"`
}

type ConsoleRequest struct {
	Request

	OutputDir         string
	OutputContentOnly bool
}

func (req *ConsoleRequest) Validate() (bool, []error) {
	if ok, violations := req.Request.Validate(); !ok {
		return ok, violations
	}
	var violations []error

	if req.OutputDir != "" {
		abs, err := filepath.Abs(req.OutputDir)
		if err != nil {
			return false, append(violations, err)
		}
		wd, err := os.Getwd()
		if err != nil {
			return false, append(violations, err)
		}
		if !strings.HasPrefix(abs, wd) {
			return false, append(violations, fmt.Errorf("output directory (%s) must be below the current working directory (%s)", abs, wd))
		}
	}

	return true, nil
}

func (req *ConsoleRequest) GetOutputDir() string {
	var p string

	if req.OutputDir != "" {
		p = req.OutputDir
	} else {
		p = "."
	}

	abs, _ := filepath.Abs(p)
	return abs
}
