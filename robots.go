package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"sync"

	"github.com/temoto/robotstxt"
)

// Robots is a simple wrapper around robotstxt.RobotsData that caches the
// robots.txt file for each host.
//
// TODO: Need to handle refreshing and expiry of robot.txt files.
// TODO: This is not shared across workers, so we might end up fetching the
//
//	same robots.txt file multiple times.
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

func (r *Robots) Check(agent string, u *url.URL) (bool, error) {
	robot, _ := r.get(u)

	group := robot.FindGroup(agent)
	if group == nil {
		return true, nil
	}

	eu := u.EscapedPath()
	if u.RawQuery != "" {
		eu += "?" + u.Query().Encode()
	}
	return group.Test(eu), nil
}

// Sitemaps returns available sitemap URLs for the given host.
func (r *Robots) Sitemaps(u *url.URL) ([]string, error) {
	robot, err := r.get(u)
	if err != nil {
		return nil, err
	}
	return robot.Sitemaps, nil
}

// get ensures that the robots.txt file for the given host is fetched.
func (r *Robots) get(u *url.URL) (*robotstxt.RobotsData, error) {
	var robot *robotstxt.RobotsData

	r.RLock()
	robot, ok := r.data[u.Host]
	r.RUnlock()

	if ok {
		return robot, nil
	}

	slog.Debug("Fetching missing robots.txt file...", "host", u.Host)
	res, err := r.client.Get(u.Scheme + "://" + u.Host + "/robots.txt")
	if err != nil {
		return robot, err
	}
	defer res.Body.Close()

	robot, err = robotstxt.FromResponse(res)
	if err != nil {
		return robot, err
	}

	r.Lock()
	r.data[u.Host] = robot
	r.Unlock()

	return robot, nil
}
