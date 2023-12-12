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

	url, err := url.Parse(req.URL)
	if err != nil {
		return s, err
	}
	domain := strings.TrimPrefix(url.Hostname(), "www.")

	return &Site{
		ID:     GenerateSiteIDFromURL(url),
		Domain: domain,
		Root:   strings.TrimRight(req.URL, "/"),
	}, nil
}

func GenerateSiteIDFromURL(u *url.URL) string {
	h := sha1.New()

	domain := strings.TrimPrefix(u.Hostname(), "www.")

	return fmt.Sprintf("%x", h.Sum([]byte(domain)))
}
