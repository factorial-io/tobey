// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type Store interface {
	RunStore
	HostStore
}

// RunStore stores transient metadata about runs.
type RunStore interface {
	SaveRun(context.Context, *Run) error
	LoadRun(context.Context, string) (*Run, bool)
	DeleteRun(context.Context, string)

	SawURL(context.Context, string, string)
	HasSeenURL(context.Context, string, string) bool
}

// HostStore holds transient information about a host that is okay to be shared
// between runs.
type HostStore interface {
	SaveHost(ctx context.Context, host *Host) bool
	LoadHost(ctx context.Context, name string) *Host
	DeleteHost(ctx context.Context, name string)
}

func CreateStore(redis *redis.Client) Store {
	//	if redis != nil {
	//		return &RedisStore{conn: redis}
	//	} else {
	return &MemoryStore{
		MemoryRunStore: MemoryRunStore{
			rstatic: make(map[string]SerializableRun),
			rlive:   make(map[string]LiveRun),
		},
		MemoryHostStore: MemoryHostStore{
			hstatic: make(map[string]SerializableHost),
			hlive:   make(map[string]LiveHost),
		},
	}
	// }
}
