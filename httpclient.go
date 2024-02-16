package main

import (
	"net/http"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
)

func NewCachingHTTPClient(cachedisk *diskv.Diskv) *http.Client {
	t := httpcache.NewTransport(diskcache.NewWithDiskv(cachedisk))

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: t,
	}
}
