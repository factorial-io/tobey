// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"crypto/sha256"
	"net/url"
	"strings"
)

// getHostnameFromURL returns the normalized host name from the URL. It will return the
// naked version of the domain, without a "www." prefix. This can be used for
// host-specific work queues.
//
// TODO: Reconsider usage of this function as it might impose a security risk when used
//
//	in security sensitive contexts, i.e. when generating cache keys for caches that
//	might allow access to private hosts or similar.
func getHostnameFromURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimLeft(p.Hostname(), "www.")
}

func isPrivateHost(h *Host, getAuth GetAuthFn) bool {
	_, ok := getAuth(h)
	return ok
}

func NewHostFromURL(u *url.URL) *Host {
	return &Host{
		SerializableHost: SerializableHost{
			Name: u.Hostname(),
			Port: u.Port(),

			PreferredScheme: u.Scheme,
		},
	}
}

// Host holds information that is scoped to a single host and is shared among
// runs, like per host rate limits, robots.txt data, sitemaps, etc.
type Host struct {
	SerializableHost
}

// SerializableHost is a serializable version of the Host struct. It must
// not include information that must not be shared beetween runs, such as
// authentication information.
type SerializableHost struct {
	Name string // The Hostname, a FQDN of the host.
	Port string // The port number, if any.

	PreferredScheme string // Either http or https
}

type LiveHost struct {
}

func (h *Host) String() string {
	if h.Port != "" {
		return h.Name + ":" + h.Port
	}
	return h.Name
}

// Hash is an alias to HashWithAuth.
func (h *Host) Hash(getAuth GetAuthFn) []byte {
	return h.HashWithAuth(getAuth)
}

// HashWithAuth returns a hash of the host including authentication information, if
// available. It is used to uniquely identify a host and its configuration. You
// should usually use this hash to identify a host over Hash() as it is more
// secure.
func (h *Host) HashWithAuth(getAuth GetAuthFn) []byte {
	hash := sha256.New()

	hash.Write(h.HashWithoutAuth())

	if ac, ok := getAuth(h); ok {
		hash.Write(ac.Hash())
	}
	return hash.Sum(nil)
}

// Hash returns a hash of the host *without* taking into account possible
// existing authentication information. Do only use this hash, i.e. as a cache
// key or ID if it is okay to have the stored data shared between different
// runs.
func (h *Host) HashWithoutAuth() []byte {
	hash := sha256.New()

	hash.Write([]byte(h.Name))
	// Scheme is not taken into account as it would still be the same host.
	hash.Write([]byte(h.Port))

	return hash.Sum(nil)
}
