// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/temoto/robotstxt"
)

func isProbablyRobots(url string) bool {
	return strings.HasSuffix(url, "/robots.txt")
}

type RobotCheckFn func(agent string, u string) (bool, error)

var (
	// ErrRobotsUnavailable is returned when the robots.txt file is unavailable. Callee should
	// decide themselves whether to allow or disallow the URL in this case.
	ErrRobotsUnavailable = errors.New("robots.txt file is unavailable")
)

// Robots is a simple wrapper around robotstxt.RobotsData that caches the
// robots.txt file for each host in memory. It is meant to be setup once
// and the instance passed into each Run.
//
// Robots is a store that provides information on what URLs are allowed to be
// fetched by a given user agent on a per host basis.
//
// Results from a fetch are not shared across workers, so we might end up
// fetching the same robots.txt file multiple times. This compromise is made in
// order to keep the implementation simple and to avoid the need for dedicated
// worker pools and work queues.
//
// Robots will blindly issue fetch requests for the control file, and not check
// rate limit information prior. When a fetch requests is denied by rate limit
// Robots will retry until it succeeds. It is assumed that such fetch requests
// have a high priority. Other requests such as regular visit requests to the
// same host will be delayed until the rate limit is lifted.
//
// It expects the provided HTTP client to perform any necessary authentication
// and caching. In addition to caching at HTTP layer Robots caches the parsed
// robots.txt files in memory for a certain time.
//
// FIXME: Need to handle forced expiry of robots.txt files from memory.
type Robots struct {
	sync.RWMutex

	// data is a map of Host IDs to the parsed robots.txt data.
	data map[string]*robotstxt.RobotsData
}

func NewRobots() *Robots {
	return &Robots{
		data: make(map[string]*robotstxt.RobotsData),
	}
}

// Check checks whether the given URL is allowed to be fetched by the given user agent.
func (r *Robots) Check(u string, getAuth GetAuthFn, ua string) (bool, error) {
	p, err := url.Parse(u)
	if err != nil {
		return false, err
	}

	robot, err := r.get(NewHostFromURL(p), getAuth, ua)
	if err != nil {
		slog.Error("Robots: Failed to fetch robots.txt file.", "url", u, "error", err)
	}
	return robot.TestAgent(ua, u), err
}

// Sitemaps returns available sitemap URLs for the given host.
func (r *Robots) Sitemaps(u string, getAuth GetAuthFn, ua string) ([]string, error) {
	p, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	robot, err := r.get(NewHostFromURL(p), getAuth, ua)
	if err != nil {
		return nil, err
	}
	return robot.Sitemaps, nil
}

// get ensures that the robots.txt file for the given host is fetched. It will block until.
func (r *Robots) get(h *Host, getAuth GetAuthFn, ua string) (*robotstxt.RobotsData, error) {
	var robot *robotstxt.RobotsData
	var err error
	var res *http.Response

	// We need to ensure that we don't retrieve a robots.txt that was earlier
	// retrieved from a private host, to a request that doesn't provide
	// authentication and treats it as a public host.
	key := fmt.Sprintf("%x", h.Hash(getAuth))

	r.RLock()
	robot, ok := r.data[key]
	r.RUnlock()

	if ok {
		return robot, nil
	}

	client := CreateRetryingHTTPClient(getAuth, ua)

	rurl := fmt.Sprintf("%s://%s/robots.txt", h.PreferredScheme, h.String())
	hlogger := slog.With("url", rurl, "host.name", h.Name, "host.port", h.Port)

	hlogger.Debug("Robots: Fetching missing robots.txt file...")

	res, err = client.Get(rurl)
	if err != nil {
		// An HTTP error is handled inside robotstxt.FromResponse, so it is save
		// to not handle it here.
		defer res.Body.Close()
		hlogger.Debug("Robots: Fetched missing robots.txt file.")
	}

	robot, err = robotstxt.FromResponse(res)

	// Always cache the result, even if it is an error. This is to avoid
	// fetching the same robots.txt file multiple times.
	//
	// FIXME: Errored robots.txt and empty files should be cached for a shorter
	//        time, currently they are cached forever.
	r.Lock()
	r.data[key] = robot
	r.Unlock()

	return robot, err
}
