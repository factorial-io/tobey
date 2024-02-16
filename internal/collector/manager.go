package collector

import (
	"log/slog"

	lru "github.com/hashicorp/golang-lru/v2"
)

type ManagerEntry struct {
	Collector  *Collector
	onEviction func(uint32)
}

type Manager struct {
	entries *lru.Cache[uint32, *ManagerEntry] // Cannot grow unbound.
}

func NewManager(maxCollectors int) *Manager {
	entries, _ := lru.NewWithEvict(maxCollectors, func(id uint32, entry *ManagerEntry) {
		entry.onEviction(id)

		slog.Info("Evicted collector.", "id", id)
	})

	return &Manager{
		entries,
	}
}

func (cm *Manager) Add(id uint32, c *Collector, onEvict func(id uint32)) bool {
	return cm.entries.Add(id, &ManagerEntry{
		c,
		onEvict,
	})
}

func (cm *Manager) Get(id uint32) (*Collector, bool) {
	entry, ok := cm.entries.Get(id)
	return entry.Collector, ok
}
