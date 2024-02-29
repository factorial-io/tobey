package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// MetaStore stores metadata about runs.
type MetaStore interface {
	MarkSeen(context.Context, string, string)
	HasSeen(context.Context, string, string) bool
	CountSeen(context.Context, string) uint32
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

func (s *MemoryMetaStore) MarkSeen(ctx context.Context, run string, url string) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.data[run]; !ok {
		s.data[run] = make([]string, 0)
	}
	s.data[run] = append(s.data[run], url)
}

func (s *MemoryMetaStore) HasSeen(ctx context.Context, run string, url string) bool {
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

func (s *MemoryMetaStore) CountSeen(ctx context.Context, run string) uint32 {
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

func (s *RedisMetaStore) MarkSeen(ctx context.Context, run string, url string) {
	s.conn.SAdd(ctx, fmt.Sprintf("%d:seen", run), url)
}

func (s *RedisMetaStore) HasSeen(ctx context.Context, run string, url string) bool {
	reply := s.conn.SIsMember(ctx, fmt.Sprintf("%d:seen", run), url)
	return reply.Val()
}

func (s *RedisMetaStore) CountSeen(ctx context.Context, run string) uint32 {
	reply := s.conn.SCard(ctx, fmt.Sprintf("%d:seen", run))
	return uint32(reply.Val())
}

func (s *RedisMetaStore) Clear(ctx context.Context, run string) {
	s.conn.Del(ctx, fmt.Sprintf("%d:seen", run))
}
