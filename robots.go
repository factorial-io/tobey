package main

import (
	"net/http"
	"net/url"
	"sync"

	"github.com/temoto/robotstxt"
)

// Robots is a simple wrapper around robotstxt.RobotsData that caches the
// robots.txt file for each host.
//
// TODO: Need to handle refreshing and expiry of robot.txt files.
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
	var robot *robotstxt.RobotsData

	r.RLock()
	robot, ok := r.data[u.Host]
	r.RUnlock()

	if !ok {
		res, err := r.client.Get(u.Scheme + "://" + u.Host + "/robots.txt")
		if err != nil {
			return false, err
		}
		defer res.Body.Close()

		robot, err = robotstxt.FromResponse(res)
		if err != nil {
			return false, err
		}

		r.Lock()
		r.data[u.Host] = robot
		r.Unlock()
	}

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
