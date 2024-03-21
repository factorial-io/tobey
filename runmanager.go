// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/redis/go-redis/v9"
)

type RunManager struct {
	entries *lru.LRU[string, *Run] // Cannot grow unbound.
	store   RunStore
}

func NewRunManager(redis *redis.Client) *RunManager {
	m := &RunManager{}

	m.entries = lru.NewLRU(MaxParallelRuns, m.onEviction, RunTTL)
	m.store = CreateRunStore(redis)

	return m
}

func (m *RunManager) Add(ctx context.Context, run *Run) bool {
	m.store.Save(ctx, run)

	run.ConfigureRobots()
	run.ConfigureStore(m.store)

	return m.entries.Add(run.ID, run)
}

func (m *RunManager) Get(ctx context.Context, id string) (*Run, bool) {
	entry, ok := m.entries.Get(id)

	if !ok {
		run, ok := m.store.Load(ctx, id)
		if !ok {
			return nil, ok
		}

		run.ConfigureRobots()
		run.ConfigureStore(m.store)

		m.entries.Add(run.ID, run)

		return run, ok
	}
	return entry, ok
}

func (m *RunManager) onEviction(id string, v *Run) {
	m.store.Clear(context.Background(), id)
}
