// Copyright 2024 Factorial GmbH. All rights reserved.

package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisRunStoreLoad(t *testing.T) {
	ctx := context.Background()

	server := miniredis.RunT(t)
	t.Cleanup(server.Close)

	conn := redis.NewClient(&redis.Options{
		Addr: server.Addr(),
	})
	defer conn.Close()

	s := &RedisRunStore{conn}

	s.Save(ctx, &Run{
		SerializableRun: SerializableRun{
			ID: "1",
		},
	})
	run, ok := s.Load(ctx, "1")
	if !ok {
		t.Fatal("run not found")
	}
	if run.ID != "1" {
		t.Errorf("unexpected run ID: %s", run.ID)
	}
}
