// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"time"
)

// Limiter is an interface that allows to make a reservation and check if it is
// being held.
type Limiter interface {
	GetLimit() float64
	SetLimit(float64)

	Reserve() (ok bool, delay time.Duration)
	HoldsReservation() bool
	ReleaseReservation()
}

type Pauser interface {
	IsPaused() bool
	Pause(d time.Duration)
	Unpause()
}
