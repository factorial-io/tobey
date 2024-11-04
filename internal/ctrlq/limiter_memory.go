// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"sync/atomic"
	"time"

	xrate "golang.org/x/time/rate"
)

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{
		Limiter: xrate.NewLimiter(xrate.Limit(MinHostRPS), 1),
	}
}

// MemoryLimiter wraps xrate.Limiter to satisfy the Reserver interface and
// provide management around held reservations. It is safe to use concurrently.
type MemoryLimiter struct {
	Limiter        *xrate.Limiter
	hasReservation atomic.Bool
}

func (l *MemoryLimiter) GetLimit() float64 {
	return float64(l.Limiter.Limit())
}

func (l *MemoryLimiter) SetLimit(limit float64) {
	l.Limiter.SetLimit(xrate.Limit(limit))
}

func (l *MemoryLimiter) Reserve() (bool, time.Duration) {
	r := l.Limiter.ReserveN(time.Now(), 1)

	if r.OK() {
		l.hasReservation.Store(true)
	}
	return r.OK(), r.Delay()
}

func (l *MemoryLimiter) HoldsReservation() bool {
	return l.hasReservation.Load()
}

func (l *MemoryLimiter) ReleaseReservation() {
	l.hasReservation.Store(false)
}
