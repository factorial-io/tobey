package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// RunStore stores metadata about runs.
type RunStore interface {
	HasSeen(context.Context, uint32, string) bool
	MarkSeen(context.Context, uint32, string)
	Clear(context.Context, uint32)
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

	// data maps a run to a list of URLs that have been seen. Clear must be used
	// to evict old runs. This is not done automatically.
	data map[uint32][]string
}

func (s *MemoryRunStore) HasSeen(ctx context.Context, run uint32, url string) bool {
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

func (s *MemoryRunStore) MarkSeen(ctx context.Context, run uint32, url string) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.data[run]; !ok {
		s.data[run] = make([]string, 0)
	}
	s.data[run] = append(s.data[run], url)
}

func (s *MemoryRunStore) Clear(ctx context.Context, run uint32) {
	s.Lock()
	defer s.Unlock()

	delete(s.data, run)
}

type RedisRunStore struct {
	conn *redis.Client
}

func (s *RedisRunStore) HasSeen(ctx context.Context, run uint32, url string) bool {
	reply := s.conn.SIsMember(ctx, fmt.Sprintf("%d:seen", run), url)
	return reply.Val()
}

func (s *RedisRunStore) MarkSeen(ctx context.Context, run uint32, url string) {
	s.conn.SAdd(ctx, fmt.Sprintf("%d:seen", run), url)
}

func (s *RedisRunStore) Clear(ctx context.Context, run uint32) {
	s.conn.Del(ctx, fmt.Sprintf("%d:seen", run))
}
