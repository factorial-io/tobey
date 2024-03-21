// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"sync"

	"github.com/temoto/robotstxt"
)

type RobotCheckFn func(agent string, u string) (bool, error)

// Robots is a simple wrapper around robotstxt.RobotsData that caches the
// robots.txt file for each host.
//
// TODO: Need to handle refreshing and expiry of robot.txt files.
// TODO: This is not shared across workers, so we might end up fetching the same robots.txt file multiple times.
type Robots struct {
	sync.RWMutex

	client *http.Client

	data map[string]*robotstxt.RobotsData
}

func NewRobots(client *http.Client) *Robots {
	return &Robots{
		client: client,
		data:   make(map[string]*robotstxt.RobotsData),
	}
}

func (r *Robots) Check(agent string, u string) (bool, error) {
	robot, err := r.get(u)
	if err != nil {
		return false, err
	}

	p, err := url.Parse(u)
	if err != nil {
		return false, err
	}

	group := robot.FindGroup(agent)
	if group == nil {
		return true, nil
	}

	eu := p.EscapedPath()
	if p.RawQuery != "" {
		eu += "?" + p.Query().Encode()
	}
	return group.Test(eu), nil
}

// Sitemaps returns available sitemap URLs for the given host.
func (r *Robots) Sitemaps(u string) ([]string, error) {
	robot, err := r.get(u)
	if err != nil {
		return nil, err
	}
	return robot.Sitemaps, nil
}

// get ensures that the robots.txt file for the given host is fetched.
func (r *Robots) get(u string) (*robotstxt.RobotsData, error) {
	var robot *robotstxt.RobotsData

	p, err := url.Parse(u)
	if err != nil {
		return robot, err
	}

	r.RLock()
	robot, ok := r.data[p.Host]
	r.RUnlock()

	if ok {
		return robot, nil
	}

	rurl := p.Scheme + "://" + p.Host + "/robots.txt"
	slog.Debug("Fetching missing robots.txt file...", "url", rurl, "host", p.Host)
	res, err := r.client.Get(rurl)
	if err != nil {
		return robot, err
	}
	defer res.Body.Close()

	robot, err = robotstxt.FromResponse(res)
	if err != nil {
		return robot, err
	}

	r.Lock()
	r.data[p.Host] = robot
	r.Unlock()

	return robot, nil
}
