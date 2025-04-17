// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package progress

import (
	"context"
	"sync"
	"time"
)

// ConfidenceLevelSafetyDistance is the number of times the average update duration to wait before being fully confident.
const ConfidenceLevelSafetyDistance = 3

// MemoryReporter stores progress updates in memory. It is a very simplified version
// of the Factorial Progress Service.
type MemoryReporter struct {
	// mu protects access to the updates map
	mu sync.RWMutex

	// updates stores the most recent progress update by run ID and URL
	updates map[string]map[string]Update
	// stats tracks statistics for each run
	stats map[string]*ProgressRunStats

	// updated is a channel that receives notifications when updates occur
	updated chan bool
}

// ProgressRunStats tracks statistics for a run
type ProgressRunStats struct {
	// FirstSeen is when the first update for this run was received
	FirstSeen time.Time
	// LastSeen is when the last update for this run was received
	LastSeen time.Time
}

// NewMemoryReporter creates a new memory-based progress reporter
func NewMemoryReporter() *MemoryReporter {
	return &MemoryReporter{
		updates: make(map[string]map[string]Update),
		stats:   make(map[string]*ProgressRunStats),
		updated: make(chan bool, 100), // Buffer size of 100 to avoid blocking
	}
}

// With creates a new Progress instance for the given run and URL
func (m *MemoryReporter) With(runID string, url string) *Progress {
	return &Progress{
		reporter: m,
		Stage:    "initial",
		RunID:    runID,
		URL:      url,
	}
}

// Call stores the progress update in memory
func (m *MemoryReporter) Call(ctx context.Context, pu Update) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the update.
	if _, ok := m.updates[pu.RunID]; !ok {
		m.updates[pu.RunID] = make(map[string]Update)
	}

	m.updates[pu.RunID][pu.URL] = pu

	// Proceed with updating the stats.
	if _, ok := m.stats[pu.RunID]; !ok {
		m.stats[pu.RunID] = &ProgressRunStats{
			FirstSeen: pu.Created,
		}
	}

	stats := m.stats[pu.RunID]
	stats.LastSeen = pu.Created

	// Notify listeners that an update has occurred
	select {
	case m.updated <- true:
		// Notification sent successfully
	default:
		// Channel is full, skip notification to avoid blocking
	}

	return nil
}

// IsRunFinished returns whether a run has finished processing with high confidence
func (m *MemoryReporter) IsRunFinished(runID string) <-chan bool {
	result := make(chan bool)

	checkFinished := func() bool {
		m.mu.RLock()
		defer m.mu.RUnlock()

		now := time.Now() // Provide consistent time, over iterations.

		if _, ok := m.updates[runID]; ok {
			if stats, ok := m.stats[runID]; ok {
				count, countDone := computeProgressCounts(m.updates[runID])
				confidence := computeProgressFinishedConfidence(now, stats.FirstSeen, stats.LastSeen, count, countDone)

				return count == countDone && countDone > 0 && confidence >= 1.0
			}
		}
		return false
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-m.updated:
				ticker.Reset(500 * time.Millisecond)

				if checkFinished() {
					result <- true
					return
				}
			case <-ticker.C:
				if checkFinished() {
					result <- true
					return
				}
			}
		}
	}()

	return result
}

// computeProgressCountDone calculates how many URLs have completed processing
func computeProgressCounts(updates map[string]Update) (int, int) {
	var done int

	for _, update := range updates {
		switch update.Status {
		case StateSucceeded, StateErrored, StateCancelled:
			done++
		}
	}
	return len(updates), done
}

// computeProgressFinishedConfidence calculates how confident we are that a run has finished.
func computeProgressFinishedConfidence(now time.Time, firstSeen, lastSeen time.Time, count, countDone int) float64 {
	var confidence float64

	// We are not finished yet, so be inconfident
	if count != countDone || countDone == 0 {
		return confidence
	}

	// If there's no elapsed time, we can't be confident
	elapsedTime := lastSeen.Sub(firstSeen).Milliseconds()
	if elapsedTime == 0 {
		return 0.0
	}

	// Calculate the average time an update takes, assumption being that
	// we should wait for the average time after the last update before
	// being confident that none will arrive.
	avgUpdateDuration := elapsedTime / int64(count)

	// Time to wait for i.e. 3x times the average update duration before being confident.
	delta := now.Sub(lastSeen).Milliseconds()
	safetyDuration := ConfidenceLevelSafetyDistance * avgUpdateDuration

	if delta >= safetyDuration {
		// We have waited more than the safety duration, so we are highly confident.
		confidence = 1.0
	} else {
		confidence = float64(delta) / float64(safetyDuration)
	}

	// Clamp the confidence to the range [0.0, 1.0]
	if confidence < 0.0 {
		return 0.0
	}
	if confidence >= 1.0 {
		return 1.0
	}
	return confidence
}
