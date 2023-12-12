package main

import (
	"crypto/sha1"
	"fmt"
	"net/url"
	"strings"
)

type Site struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Root   string `json:"root"`
}

func DeriveSiteFromAPIRequest(req *APIRequest) (*Site, error) {
	var s *Site
	var url *url.URL
	var err error

	if req.Site != nil {
		s = req.Site
	} else {
		s = &Site{}
	}

	if req.SiteRoot != "" {
		url, err = url.Parse(req.SiteRoot)
		if err != nil {
			return s, err
		}
		if s.ID == "" {
			s.ID = GenerateSiteIDFromURL(url)
		}
		if s.Root == "" {
			s.Root = strings.TrimRight(req.SiteRoot, "/")
		}
	} else if s.Root != "" {
		url, err = url.Parse(s.Root)
		if err != nil {
			return s, err
		}
	} else {
		return s, fmt.Errorf("Missing root URL of site, and not enough information to guess.")
	}
	if s.Domain == "" {
		s.Domain = strings.TrimPrefix(url.Hostname(), "www.")
	}
	return s, nil
}

func GenerateSiteIDFromURL(u *url.URL) string {
	h := sha1.New()

	domain := strings.TrimPrefix(u.Hostname(), "www.")

	return fmt.Sprintf("%x", h.Sum([]byte(domain)))
}
