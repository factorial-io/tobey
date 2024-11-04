// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"fmt"
	"sync/atomic"
	"time"
)

// ControlledQueue is a wrapped queue that is flow controllable, by
// pausing and resuming. It also has a rate limiter attached to it.
type ControlledQueue struct {
	ID   uint32
	Name string // For debugging purposes only.

	Queue chan *VisitMessage

	Limiter Limiter

	// Holds an Unix timestamp, instead of a time.Time so
	// that we can operate on this without locks.
	pausedUntil atomic.Int64

	IsAdaptive bool
}

func (cq *ControlledQueue) String() string {
	_, until := cq.IsPaused()
	return fmt.Sprintf("ControlledQueue(%d, %s, %d, %s, %v)", cq.ID, cq.Name, len(cq.Queue), until, cq.Limiter.HoldsReservation())
}

func (cq *ControlledQueue) IsPaused() (bool, time.Time) {
	now := time.Now().Unix()
	until := cq.pausedUntil.Load()

	if until == 0 {
		return false, time.Time{}
	}
	return now < until, time.Unix(until, 0)
}

func (cq *ControlledQueue) Pause(d time.Duration) time.Time {
	v := time.Now().Add(d).Unix()
	o := cq.pausedUntil.Load()

	if o == 0 || v > o { // Pause can only increase.
		cq.pausedUntil.Store(v)
		return time.Unix(v, 0)
	}
	return time.Unix(o, 0)
}

func (cq *ControlledQueue) Unpause() {
	cq.pausedUntil.Store(0)
}
