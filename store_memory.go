// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"fmt"
	"sync"
)

type MemoryStore struct {
	sync.RWMutex

	MemoryRunStore
	MemoryHostStore
}

type MemoryRunStore struct {
	rstatic map[string]SerializableRun
	rlive   map[string]LiveRun
}

type MemoryHostStore struct {
	hstatic map[string]SerializableHost
	hlive   map[string]LiveHost
}

func (s *MemoryStore) SaveRun(ctx context.Context, run *Run) error {
	s.Lock()
	defer s.Unlock()

	s.rstatic[run.ID] = run.SerializableRun
	return nil
}

func (s *MemoryStore) LoadRun(ctx context.Context, id string) (*Run, bool) {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.rstatic[id]; !ok {
		return nil, false
	}
	return &Run{
		SerializableRun: s.rstatic[id],
	}, true
}

func (s *MemoryStore) DeleteRun(ctx context.Context, id string) {
	s.Lock()
	defer s.Unlock()

	delete(s.rstatic, id)
	delete(s.rlive, id)
}

func (s *MemoryStore) SawURL(ctx context.Context, run string, url string) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.rlive[run]; !ok {
		s.rlive[run] = LiveRun{
			Seen: make([]string, 0),
		}
	}
	entry := s.rlive[run]
	entry.Seen = append(entry.Seen, url)

	s.rlive[run] = entry
}

func (s *MemoryStore) HasSeenURL(ctx context.Context, run string, url string) bool {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.rlive[run]; !ok {
		return false
	}
	for _, v := range s.rlive[run].Seen {
		if v == url {
			return true
		}
	}
	return false
}

// SaveHost adds a host without authentcation to the store, if the host already
// exists it will be ignored. So this function is idempotent and can be called
// even not using HasHost to verify the preconditions.
func (s *MemoryStore) SaveHost(ctx context.Context, host *Host) bool {
	s.Lock()
	defer s.Unlock()

	key := fmt.Sprintf("%x", host.HashWithoutAuth())

	if _, ok := s.hstatic[key]; ok {
		return false // Already exists.
	}

	s.hstatic[key] = host.SerializableHost
	return true
}

// LoadHost returns a host from the store, if the host does not exist it will
// return nil.
func (s *MemoryStore) LoadHost(ctx context.Context, id string) *Host {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.hstatic[id]; !ok {
		return nil
	}
	return &Host{
		SerializableHost: s.hstatic[id],
	}
}

func (s *MemoryStore) DeleteHost(ctx context.Context, id string) {
	s.Lock()
	defer s.Unlock()

	delete(s.hstatic, id)
}
