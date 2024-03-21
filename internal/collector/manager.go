// Copyright 2024 Factorial GmbH. All rights reserved.

package collector

import (
	"log/slog"

	lru "github.com/hashicorp/golang-lru/v2"
)

type ManagerEntry struct {
	Collector  *Collector
	onEviction func(string)
}

type Manager struct {
	entries *lru.Cache[string, *ManagerEntry] // Cannot grow unbound.
}

func NewManager(maxCollectors int) *Manager {
	entries, _ := lru.NewWithEvict(maxCollectors, func(id string, entry *ManagerEntry) {
		entry.onEviction(id)

		slog.Info("Evicted collector.", "id", id)
	})

	return &Manager{
		entries,
	}
}

func (cm *Manager) Add(c *Collector, onEvict func(id string)) bool {
	return cm.entries.Add(c.Run, &ManagerEntry{
		c,
		onEvict,
	})
}

func (cm *Manager) Get(id string) (*Collector, bool) {
	entry, ok := cm.entries.Get(id)
	// Otherwise you use collector that does not exist.
	if !ok {
		return nil, ok
	}
	return entry.Collector, ok
}
