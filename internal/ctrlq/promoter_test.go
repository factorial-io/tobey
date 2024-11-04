// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"context"
	"testing"
	"time"
)

func TestPromoteSucceeds(t *testing.T) {
	a := make(chan bool, 1)
	b := make(chan bool, 1)

	a <- true

	if promote(context.Background(), a, b) != true {
		t.Errorf("Expected true, got false")
	}
}

func TestPromoteNothingToRead(t *testing.T) {
	a := make(chan bool)
	b := make(chan bool, 1)

	if promote(context.Background(), a, b) != false {
		t.Errorf("Expected false, got true")
	}
}

func TestCandidatesNothingToDo(t *testing.T) {
	hqueues := make(map[uint32]*ControlledQueue)

	hqueues[1] = &ControlledQueue{
		ID:         1,
		Name:       "example.org",
		IsAdaptive: false,
		Queue:      make(chan *VisitMessage, 10),
		Limiter:    NewMemoryLimiter(),
	}

	candidates, retry := candidates(hqueues)

	if len(candidates) != 0 {
		t.Errorf("Expected 0 candidates, got %d", len(candidates))
	}
	if retry.IsZero() != true {
		t.Errorf("Expected zero time, got %v", retry)
	}
}

func TestCandidatesSingle(t *testing.T) {
	hqueues := make(map[uint32]*ControlledQueue)

	hqueues[1] = &ControlledQueue{
		ID:         1,
		Name:       "example.org",
		IsAdaptive: false,
		Queue:      make(chan *VisitMessage, 10),
		Limiter:    NewMemoryLimiter(),
	}
	hqueues[1].Queue <- &VisitMessage{ID: 1}

	candidates, _ := candidates(hqueues)

	if len(candidates) != 1 {
		t.Errorf("Expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0] != 1 {
		t.Errorf("Expected ID 1, got %d", candidates[0])
	}
}

func TestCandidatesAcquireReservation(t *testing.T) {
	hqueues := make(map[uint32]*ControlledQueue)

	hqueues[1] = &ControlledQueue{
		ID:         1,
		Name:       "example1.org",
		IsAdaptive: false,
		Queue:      make(chan *VisitMessage, 10),
		Limiter:    NewMemoryLimiter(),
	}
	hqueues[1].Queue <- &VisitMessage{ID: 1}

	hqueues[2] = &ControlledQueue{
		ID:         2,
		Name:       "example2.org",
		IsAdaptive: false,
		Queue:      make(chan *VisitMessage, 10),
		Limiter:    NewMemoryLimiter(),
	}
	hqueues[2].Queue <- &VisitMessage{ID: 2}

	if hqueues[1].Limiter.HoldsReservation() != false {
		t.Errorf("Expected no reservation to be held.")
	}
	if hqueues[2].Limiter.HoldsReservation() != false {
		t.Errorf("Expected no reservation to be held.")
	}

	candidates, _ := candidates(hqueues)

	if len(candidates) != 2 {
		t.Errorf("Expected 2 candidates, got %d", len(candidates))
	}

	if hqueues[1].Limiter.HoldsReservation() != true {
		t.Errorf("Expected reservation to be held.")
	}
	if hqueues[2].Limiter.HoldsReservation() != true {
		t.Errorf("Expected reservation to be held.")
	}
}

func TestCandidatesPausedQueueDoesNotHitLimiterCalcShortest(t *testing.T) {
	hqueues := make(map[uint32]*ControlledQueue)

	d1, _ := time.ParseDuration("1s")
	d2, _ := time.ParseDuration("2s")

	t1 := time.Now().Add(d1).Unix()
	t2 := time.Now().Add(d2).Unix()

	hqueues[1] = &ControlledQueue{
		ID:         1,
		Name:       "example1.org",
		IsAdaptive: false,
		Queue:      make(chan *VisitMessage, 10),
		Limiter:    NewMemoryLimiter(),
	}
	hqueues[1].Queue <- &VisitMessage{ID: 1}
	hqueues[1].pausedUntil.Store(t1)

	hqueues[2] = &ControlledQueue{
		ID:         2,
		Name:       "example2.org",
		IsAdaptive: false,
		Queue:      make(chan *VisitMessage, 10),
		Limiter:    NewMemoryLimiter(),
	}
	hqueues[2].Queue <- &VisitMessage{ID: 2}
	hqueues[2].pausedUntil.Store(t2)

	candidates, retry := candidates(hqueues)

	if retry.Unix() != t1 {
		t.Errorf("Expected %v, got %v", t1, retry.Unix())
	}
	if len(candidates) != 0 {
		t.Errorf("Expected 0 candidates, got %d", len(candidates))
	}

	// Should not have hit rate limiter as we paused the queue and
	// the queue's pause has not yet passed.
	if hqueues[1].Limiter.HoldsReservation() != false {
		t.Errorf("Expected no reservation to be held.")
	}
	if hqueues[2].Limiter.HoldsReservation() != false {
		t.Errorf("Expected no reservation to be held.")
	}
}
