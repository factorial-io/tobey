// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

// The job promoter looks at host queues in a round-robin fashion and promotes
// messages from a host queue to the default queue.

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Load balancing queue querying is local to each tobey instance. This should wrap
// around at 2^32.
var next uint32

// promoteLoop starts a loop that shovels message from host queue into the
// default queue. The promoter is responsible for load balancing the host
// queues.
func promoteLoop(ctx context.Context, dqueue chan *VisitMessage, hqueues map[uint32]*ControlledQueue, shouldRecalc chan bool) {
	slog.Debug("Work Queue: Starting promoter...")

	for {
	immediatecalc:
		// slog.Debug("Work Queue: Calculating immediate queue candidates.")
		immediate, shortestPauseUntil := candidates(hqueues)

		// Check how long we have to wait until we can recalc immidiate candidates.
		if len(immediate) == 0 {
			// Nothin to do yet, try again whenever
			// 1. a new message is published,
			// 2. the rate limited is adjusted
			// 3. a queue is now unpaused.
			var delay time.Duration

			if !shortestPauseUntil.IsZero() { // None may be paused.
				delay = shortestPauseUntil.Sub(time.Now())
			} else {
				delay = 60 * time.Second
			}

			slog.Debug("Work Queue: No immediate queue candidates, waiting for a while...", "delay", delay)
			select {
			case <-time.After(delay):
				// slog.Debug("Work Queue: Pause time passed.")
				goto immediatecalc
			case <-shouldRecalc:
				slog.Debug("Work Queue: Got notification to re-calc immediate queue candidates.")
				goto immediatecalc
			case <-ctx.Done():
				slog.Debug("Work Queue: Context cancelled, stopping promoter.")
				return
			}
		}

		// When we get here we have a at least one host queue that can be
		// queried, if we have multiple candidates we load balance over
		// them.
		slog.Debug("Work Queue: Final immediate queue candidates calculated.", "count", len(immediate))

		n := atomic.AddUint32(&next, 1)
		key := immediate[(int(n)-1)%len(immediate)]

		hq, _ := hqueues[key]
		// FIXME: The host queue might haven gone poof in the meantime, we should
		//        check if the host queue is still there.

		if promote(ctx, hq.Queue, dqueue) {
			hq.Limiter.ReleaseReservation()
		}
	}
}

// candidates calculates which host queues are candidates to be queried for
// message to be promoted, it will check for a reservation and if none is held
// already will try to acquire one.
//
// The second return value if non zero indicates the shortest time until a host
// queue is paused (non only the candidates). When no candidate is found, the
// callee should wait at least for that time and than try and call this function
// again.
func candidates(hqueues map[uint32]*ControlledQueue) ([]uint32, time.Time) {
	// Host queue candidates that can be queried immediately.
	immediate := make([]uint32, 0, len(hqueues))
	var shortestPauseUntil time.Time

	// First calculate which host queues are candidates for immediate
	// querying. This is to unnecesarily hitting the rate limiter for
	// that host, as this can be expensive. More than checking the
	// PausedUntil time, or the length of the queue.
	//
	// FIXME: It might be less expensive to first check for the PausedUntil
	//        time and then check the length of the queue, depending on the
	//        underlying implementation of the work queue.
	for _, hq := range hqueues {
		hlogger := slog.With("queue.name", hq.Name)
		hlogger.Debug("Work Queue: Checking if is candidate.", "Queue", hq.String())

		if len(hq.Queue) == 0 {
			// This host queue is empty, no message to process for that queue,
			// so don't include it in the immediate list.
			continue
		}
		// hlogger.Debug("Work Queue: Host queue has messages to process.")

		if paused, until := hq.IsPaused(); paused {
			// Check if this host queue is paused shorter then the shortest
			// pause we've seen so far.
			if shortestPauseUntil.IsZero() || until.Before(shortestPauseUntil) {
				shortestPauseUntil = until
			}

			// As this host queue is still paused we don't include
			// it in the imediate list. Continue to check if the
			// next host queue is paused or not.
			continue
		}

		// If we get here, the current host queue was either never
		// paused, or it is now unpaused. This means we can try to get a
		// token from the rate limiter, if we haven't already.
		if !hq.Limiter.HoldsReservation() {
			// hlogger.Debug("Work Queue: Host queue needs a reservation, checking rate limiter.")
			ok, delay := hq.Limiter.Reserve()
			if !ok {
				hlogger.Warn("Work Queue: Rate limiter cannot provide reservation in max wait time.")
				continue
			}

			if delay > 0 {
				hlogger.Debug("Work Queue: Host queue is rate limited, pausing the queue.", "delay", delay)

				// Pause the tube for a while, the limiter wants us to retry later.
				until := hq.Pause(delay)

				if shortestPauseUntil.IsZero() || until.Before(shortestPauseUntil) {
					shortestPauseUntil = until
				}
				continue
			}
		}
		immediate = append(immediate, hq.ID)
	}
	return immediate, shortestPauseUntil
}

// promote promotes a message from one channel (a) to another (b). The function returns
// true when the message was successfully promoted, false otherwise. The
// function will not block on an empty source channel or on an overful target
// channel, even when these are not buffered.
func promote[V any](ctx context.Context, a <-chan V, b chan<- V) bool {
	select {
	case msg := <-a:
		select {
		case b <- msg:
			return true
		default:
			return false
		}
	case <-ctx.Done():
		return false
	default:
		return false
	}
}
