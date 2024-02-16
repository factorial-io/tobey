package collector

import (
	lru "github.com/hashicorp/golang-lru/v2"
)

type Manager struct {
	collectors *lru.Cache[uint32, *Collector] // Cannot grow unbound.
}

func NewManager(maxCollectors int) *Manager {
	collectors, _ := lru.New[uint32, *Collector](maxCollectors)

	return &Manager{
		collectors,
	}
}

func (cm *Manager) Add(id uint32, v *Collector) bool {
	return cm.collectors.Add(id, v)
}

func (cm *Manager) Get(id uint32) (*Collector, bool) {
	return cm.collectors.Get(id)
}
