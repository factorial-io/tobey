// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/redis/go-redis/v9"
)

type RunManager struct {
	entries *lru.LRU[string, *Run] // Cannot grow unbound.

	// Shared for all runs, materialized by the RunManager.
	store    RunStore
	robots   *Robots
	sitemaps *Sitemaps
}

func NewRunManager(redis *redis.Client, ro *Robots, si *Sitemaps) *RunManager {
	m := &RunManager{}

	m.entries = lru.NewLRU(MaxParallelRuns, m.onEviction, RunTTL)
	m.store = CreateStore(redis)
	m.robots = ro
	m.sitemaps = si

	return m
}

func (m *RunManager) Add(ctx context.Context, run *Run) bool {
	m.store.SaveRun(ctx, run)

	run.Configure(
		m.store,
		m.robots,
		m.sitemaps,
	)

	return m.entries.Add(run.ID, run)
}

func (m *RunManager) Get(ctx context.Context, id string) (*Run, bool) {
	entry, ok := m.entries.Get(id)

	if !ok {
		run, ok := m.store.LoadRun(ctx, id)
		if !ok {
			return nil, ok
		}

		run.Configure(
			m.store,
			m.robots,
			m.sitemaps,
		)

		m.entries.Add(run.ID, run)

		return run, ok
	}
	return entry, ok
}

func (m *RunManager) onEviction(id string, v *Run) {
	m.store.DeleteRun(context.Background(), id)
}
