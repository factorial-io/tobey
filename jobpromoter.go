// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// The job promoter looks at host queues in a round-robin fashion and promotes
// messages from a host queue to the default queue.

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// startPromoter starts a the promoter goroutine that shovels message from host
// queue into the default queue. The promoter is responsible for load balancing
// the host queues.
func (wq *MemoryVisitWorkQueue) startPromoter(ctx context.Context) {
	go func() {
		slog.Debug("Work Queue: Starting promoter...")

		// Load balancing queue querying is local to each tobey instance. This should wrap
		// around at 2^32.
		var next uint32

		for {
		immediatecalc:
			// slog.Debug("Work Queue: Calculating immediate queue candidates.")
			immediate, shortestPauseUntil := wq.calcImmediateHostQueueCandidates()

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
				case <-wq.shoudlRecalc:
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
			hq, _ := wq.hqueues[key]

			// FIXME: The host queue might haven gone poof in the meantime, we should
			//        check if the host queue is still there.

			slog.Debug("Work Queue: Querying host queue for messages.", "queue.name", hq.Name)
			select {
			case pkg := <-hq.Queue:
				// Now promote the pkg to the default queue.
				select {
				case wq.dqueue <- pkg:
					slog.Debug("Work Queue: Message promoted.", "msg.id", pkg.Message.ID)

					wq.mu.Lock()
					wq.hqueues[hq.ID].HasReservation = false
					wq.mu.Unlock()
				default:
					slog.Warn("Work Queue: full, dropping to-be-promoted message!", "msg", pkg.Message)
				}
			default:
				// The channel may became empty in the meantime, we've checked at top of the loop.
				// slog.Debug("Work Queue: Host queue empty, nothing to promote.", "queue.name", hq.Hostname)
			case <-ctx.Done():
				slog.Debug("Work Queue: Context cancelled, stopping promoter.")
				return
			}
		}
	}()
}

// calcImmediateHostQueueCandidates calculates which host queues are candidates
// to be queried for message to be promoted. The function modifies the host queue
// properties, so it requires a lock to be held.
//
// The second return value if non zero indicates the shortest time until a host
// queue is paused (non only the candidates). When no candidate is found, the
// callee should wait at least for that time and than try and call this function
// again.
func (wq *MemoryVisitWorkQueue) calcImmediateHostQueueCandidates() ([]uint32, time.Time) {
	// Host queue candidates that can be queried immediately.
	immediate := make([]uint32, 0, len(wq.hqueues))
	var shortestPauseUntil time.Time

	wq.mu.Lock()
	defer wq.mu.Unlock()

	// First calculate which host queues are candidates for immediate
	// querying. This is to unnecesarily hitting the rate limiter for
	// that host, as this can be expensive. More than checking the
	// PausedUntil time, or the length of the queue.
	//
	// FIXME: It might be less expensive to first check for the PausedUntil
	//        time and then check the length of the queue, depending on the
	//        underlying implementation of the work queue.
	for k, hq := range wq.hqueues {
		hlogger := slog.With("queue.name", hq.Name)
		hlogger.Debug("Work Queue: Checking if is candidate.", "len", len(hq.Queue), "now", time.Now(), "pausedUntil", hq.PausedUntil)

		if len(hq.Queue) == 0 {
			// This host queue is empty, no message to process for that queue,
			// so don't include it in the immediate list.
			continue
		}
		// hlogger.Debug("Work Queue: Host queue has messages to process.")

		if hq.PausedUntil.IsZero() {
			// This host queue was never paused before.
			// hlogger.Debug("Work Queue: Host queue is not paused.")
		} else {
			if time.Now().Before(hq.PausedUntil) {
				// This host queue is *still* paused.
				// hlogger.Debug("Work Queue: Host queue is paused.")

				// Check if this host queue is paused shorter thant the shortest
				// pause we've seen so far.
				if shortestPauseUntil.IsZero() || hq.PausedUntil.Before(shortestPauseUntil) {
					shortestPauseUntil = hq.PausedUntil
				}

				// As this host queue is still paused we don't include
				// it in the imediate list. Continue to check if the
				// next host queue is paused or not.
				continue
			} else {
				// hlogger.Debug("Work Queue: Host queue is not paused anymore.")

				// This host queue is not paused anymore, include it in
				// the list. While not technically necessary, we reset
				// the pause until time to zero, for the sake of tidiness.
				wq.hqueues[k].PausedUntil = time.Time{}
			}
		}

		// If we get here, the current host queue was either never
		// paused, or it is now unpaused. This means we can try to get a
		// token from the rate limiter, if we haven't already.
		if !hq.HasReservation {
			// hlogger.Debug("Work Queue: Host queue needs a reservation, checking rate limiter.")
			res := hq.Limiter.Reserve()
			if !res.OK() {
				hlogger.Warn("Work Queue: Rate limiter cannot provide reservation in max wait time.")
				continue
			}

			if res.Delay() > 0 {
				hlogger.Debug("Work Queue: Host queue is rate limited, pausing the queue.", "delay", res.Delay())

				// Pause the tube for a while, the limiter wants us to retry later.
				wq.hqueues[k].PausedUntil = time.Now().Add(res.Delay())

				if shortestPauseUntil.IsZero() || wq.hqueues[k].PausedUntil.Before(shortestPauseUntil) {
					shortestPauseUntil = wq.hqueues[k].PausedUntil
				}

				wq.hqueues[k].HasReservation = true
				continue
			} else {
				// Got a token from the limiter, we may act immediately.
				// hlogger.Debug("Work Queue: Host queue is not rate limited, recording as candidate :)")
				wq.hqueues[k].HasReservation = true
			}
		} else {
			// hlogger.Debug("Work Queue: Host already has a reservation.")
		}
		immediate = append(immediate, hq.ID)
	}
	return immediate, shortestPauseUntil
}
