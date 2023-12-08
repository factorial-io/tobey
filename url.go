package main

import (
	"crypto/sha1"
	"fmt"
	"net/url"
	"strings"
)

func generateSiteIDFromURL(u string) (string, error) {
	h := sha1.New()

	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	domain := strings.TrimPrefix(parsed.Hostname(), "www.")

	return fmt.Sprintf("%x", h.Sum([]byte(domain))), nil
}
