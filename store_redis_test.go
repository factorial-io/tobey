// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStoreLoadRun(t *testing.T) {
	ctx := context.Background()

	server := miniredis.RunT(t)
	t.Cleanup(server.Close)

	conn := redis.NewClient(&redis.Options{
		Addr: server.Addr(),
	})
	defer conn.Close()

	s := &RedisStore{conn}

	s.SaveRun(ctx, &Run{
		SerializableRun: SerializableRun{
			ID: "1",
		},
	})
	run, ok := s.LoadRun(ctx, "1")
	if !ok {
		t.Fatal("run not found")
	}
	if run.ID != "1" {
		t.Errorf("unexpected run ID: %s", run.ID)
	}
}
