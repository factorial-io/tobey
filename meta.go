// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// MetaStore stores metadata about runs.
type MetaStore interface {
	SawURL(context.Context, string, string)
	HasSeenURL(context.Context, string, string) bool
	CountSeenURLs(context.Context, string) uint32
	Clear(context.Context, string)
}

func CreateMetaStore(redis *redis.Client) MetaStore {
	if redis != nil {
		return &RedisMetaStore{conn: redis}
	} else {
		return &MemoryMetaStore{data: make(map[string][]string)}
	}
}

type MemoryMetaStore struct {
	sync.RWMutex

	// data maps a run to a list of URLs that have been seen. Clear must be used
	// to evict old runs. This is not done automatically.
	data map[string][]string
}

func (s *MemoryMetaStore) SawURL(ctx context.Context, run string, url string) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.data[run]; !ok {
		s.data[run] = make([]string, 0)
	}
	s.data[run] = append(s.data[run], url)
}

func (s *MemoryMetaStore) HasSeenURL(ctx context.Context, run string, url string) bool {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.data[run]; !ok {
		return false
	}
	for _, v := range s.data[run] {
		if v == url {
			return true
		}
	}
	return false
}

func (s *MemoryMetaStore) CountSeenURLs(ctx context.Context, run string) uint32 {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.data[run]; !ok {
		return 0
	}
	return uint32(len(s.data[run]))
}

func (s *MemoryMetaStore) Clear(ctx context.Context, run string) {
	s.Lock()
	defer s.Unlock()

	delete(s.data, run)
}

type RedisMetaStore struct {
	conn *redis.Client
}

func (s *RedisMetaStore) SawURL(ctx context.Context, run string, url string) {
	s.conn.SAdd(ctx, fmt.Sprintf("%s:seen", run), url)
}

func (s *RedisMetaStore) HasSeenURL(ctx context.Context, run string, url string) bool {
	reply := s.conn.SIsMember(ctx, fmt.Sprintf("%s:seen", run), url)
	return reply.Val()
}

func (s *RedisMetaStore) CountSeenURLs(ctx context.Context, run string) uint32 {
	reply := s.conn.SCard(ctx, fmt.Sprintf("%s:seen", run))
	return uint32(reply.Val())
}

func (s *RedisMetaStore) Clear(ctx context.Context, run string) {
	s.conn.Del(ctx, fmt.Sprintf("%s:seen", run))
}
