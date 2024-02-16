package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

type RunStore interface {
	HasSeen(context.Context, uint32, string) bool
	Seen(context.Context, uint32, string)
}

func CreateRunStore(redis *redis.Client) RunStore {
	if redis != nil {
		return &RedisRunStore{conn: redis}
	} else {
		return &MemoryRunStore{data: make(map[uint32][]string)}
	}
}

type MemoryRunStore struct {
	sync.RWMutex
	data map[uint32][]string
}

func (s *MemoryRunStore) HasSeen(ctx context.Context, runID uint32, url string) bool {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.data[runID]; !ok {
		return false
	}
	for _, v := range s.data[runID] {
		if v == url {
			return true
		}
	}
	return false
}

func (s *MemoryRunStore) Seen(ctx context.Context, runID uint32, url string) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.data[runID]; !ok {
		s.data[runID] = make([]string, 0)
	}
	s.data[runID] = append(s.data[runID], url)
}

type RedisRunStore struct {
	conn *redis.Client
}

func (s *RedisRunStore) HasSeen(ctx context.Context, runID uint32, url string) bool {
	reply := s.conn.SIsMember(ctx, fmt.Sprintf("%d:seen", runID), url)
	return reply.Val()
}

func (s *RedisRunStore) Seen(ctx context.Context, runID uint32, url string) {
	s.conn.SAdd(ctx, fmt.Sprintf("%d:seen", runID), url)
}
