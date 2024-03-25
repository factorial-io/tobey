// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// RunStore stores transient metadata about runs.
type RunStore interface {
	Save(context.Context, *Run) error
	Load(context.Context, string) (*Run, bool)

	SawURL(context.Context, string, string)
	HasSeenURL(context.Context, string, string) bool

	Clear(context.Context, string)
}

func CreateRunStore(redis *redis.Client) RunStore {
	if redis != nil {
		return &RedisRunStore{conn: redis}
	} else {
		return &MemoryRunStore{
			static: make(map[string]SerializableRun),
			live:   make(map[string]LiveRun),
		}
	}
}

type MemoryRunStore struct {
	sync.RWMutex

	static map[string]SerializableRun
	live   map[string]LiveRun
}

func (s *MemoryRunStore) Save(ctx context.Context, run *Run) error {
	s.Lock()
	defer s.Unlock()

	s.static[run.ID] = run.SerializableRun
	return nil
}

func (s *MemoryRunStore) Load(ctx context.Context, id string) (*Run, bool) {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.static[id]; !ok {
		return nil, false
	}
	return &Run{
		SerializableRun: s.static[id],
	}, true
}

func (s *MemoryRunStore) SawURL(ctx context.Context, run string, url string) {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.live[run]; !ok {
		s.live[run] = LiveRun{
			Seen: make([]string, 0),
		}
	}
	entry := s.live[run]
	entry.Seen = append(entry.Seen, url)

	s.live[run] = entry
}

func (s *MemoryRunStore) HasSeenURL(ctx context.Context, run string, url string) bool {
	s.RLock()
	defer s.RUnlock()

	if _, ok := s.live[run]; !ok {
		return false
	}
	for _, v := range s.live[run].Seen {
		if v == url {
			return true
		}
	}
	return false
}

func (s *MemoryRunStore) Clear(ctx context.Context, id string) {
	s.Lock()
	defer s.Unlock()

	delete(s.static, id)
	delete(s.live, id)
}

type RedisRunStore struct {
	conn *redis.Client
}

func (s *RedisRunStore) Save(ctx context.Context, run *Run) error {
	b, _ := json.Marshal(run.SerializableRun)
	s.conn.Set(ctx, fmt.Sprintf("%s:static", run.ID), string(b), RunTTL)

	return nil
}

// Load loads a Run from Redis.
func (s *RedisRunStore) Load(ctx context.Context, id string) (*Run, bool) {
	var run *Run

	reply := s.conn.Get(ctx, fmt.Sprintf("%s:static", id))
	if err := reply.Err(); err != nil {
		return nil, false
	}

	json.Unmarshal([]byte(reply.Val()), &run)
	return run, true
}

func (s *RedisRunStore) SawURL(ctx context.Context, run string, url string) {
	s.conn.SAdd(ctx, fmt.Sprintf("%s:live:seen", run), url)
}

func (s *RedisRunStore) HasSeenURL(ctx context.Context, run string, url string) bool {
	reply := s.conn.SIsMember(ctx, fmt.Sprintf("%s:live:seen", run), url)
	return reply.Val()
}

func (s *RedisRunStore) Clear(ctx context.Context, run string) {
	s.conn.Del(
		ctx,
		fmt.Sprintf("%s:static", run),
		fmt.Sprintf("%s:live:seen", run),
	)
}
