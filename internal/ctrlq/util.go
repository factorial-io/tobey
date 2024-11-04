// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"hash/fnv"
	"net/url"
	"strings"
)

// guessHost heuristically identifies a host for the given URL. The function
// doesn't return the host name directly, as it might not exist, but an ID.
//
// It does by by ignoring a www. prefix, leading to www.example.org and
// example.org being considered the same host. It also ignores the port number,
// so example.org:8080 and example.org:9090 are considered the same host as
// well.
//
// Why FNV? https://softwareengineering.stackexchange.com/questions/49550
func guessHost(u string) uint32 {
	p, err := url.Parse(u)
	if err != nil {
		return 0
	}
	h := fnv.New32a()

	h.Write([]byte(strings.TrimLeft(p.Hostname(), "www.")))
	return h.Sum32()
}
