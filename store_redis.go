// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	conn *redis.Client
}

func (s *RedisStore) SaveRun(ctx context.Context, run *Run) error {
	b, _ := json.Marshal(run.SerializableRun)
	s.conn.Set(ctx, fmt.Sprintf("%s:static", run.ID), string(b), RunTTL)

	return nil
}

// Load loads a Run from Redis.
func (s *RedisStore) LoadRun(ctx context.Context, id string) (*Run, bool) {
	var run *Run

	reply := s.conn.Get(ctx, fmt.Sprintf("%s:static", id))
	if err := reply.Err(); err != nil {
		return nil, false
	}

	json.Unmarshal([]byte(reply.Val()), &run)
	return run, true
}

func (s *RedisStore) DeleteRun(ctx context.Context, run string) {
	s.conn.Del(
		ctx,
		fmt.Sprintf("%s:static", run),
		fmt.Sprintf("%s:live:seen", run),
	)
}

func (s *RedisStore) SawURL(ctx context.Context, run string, url string) {
	s.conn.SAdd(ctx, fmt.Sprintf("%s:live:seen", run), url)
}

func (s *RedisStore) HasSeenURL(ctx context.Context, run string, url string) bool {
	reply := s.conn.SIsMember(ctx, fmt.Sprintf("%s:live:seen", run), url)
	return reply.Val()
}
