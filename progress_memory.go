package main

import (
	"context"
	"sync"
	"time"
)

// ConfidenceLevelSafetyDistance is the number of times the average update duration to wait before being fully confident.
const ConfidenceLevelSafetyDistance = 3

// MemoryProgressReporter stores progress updates in memory. It is a very simplified version
// of the Factorial Progress Service.
type MemoryProgressReporter struct {
	// mu protects access to the updates map
	mu sync.RWMutex

	// updates stores the most recent progress update by run ID and URL
	updates map[string]map[string]ProgressUpdate
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

// NewMemoryProgressReporter creates a new memory-based progress reporter
func NewMemoryProgressReporter() *MemoryProgressReporter {
	return &MemoryProgressReporter{
		updates: make(map[string]map[string]ProgressUpdate),
		stats:   make(map[string]*ProgressRunStats),
		updated: make(chan bool, 100), // Buffer size of 100 to avoid blocking
	}
}

// With creates a new Progress instance for the given run and URL
func (m *MemoryProgressReporter) With(run *Run, url string) *Progress {
	return &Progress{
		reporter: m,
		Stage:    "initial",
		Run:      run,
		URL:      url,
	}
}

// Call stores the progress update in memory
func (m *MemoryProgressReporter) Call(ctx context.Context, pu ProgressUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the update.
	if _, ok := m.updates[pu.Run.ID]; !ok {
		m.updates[pu.Run.ID] = make(map[string]ProgressUpdate)
	}

	m.updates[pu.Run.ID][pu.URL] = pu

	// Proceed with updating the stats.
	if _, ok := m.stats[pu.Run.ID]; !ok {
		m.stats[pu.Run.ID] = &ProgressRunStats{
			FirstSeen: pu.Created,
		}
	}

	stats := m.stats[pu.Run.ID]
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
func (m *MemoryProgressReporter) IsRunFinished(runID string) <-chan bool {
	result := make(chan bool)

	checkFinished := func() bool {
		m.mu.RLock()
		defer m.mu.RUnlock()

		if _, ok := m.updates[runID]; ok {
			if stats, ok := m.stats[runID]; ok {
				count, countDone := computeProgressCounts(m.updates[runID])
				confidence := computeProgressFinishedConfidence(stats.FirstSeen, stats.LastSeen, count, countDone)

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
func computeProgressCounts(updates map[string]ProgressUpdate) (int, int) {
	var done int

	for _, update := range updates {
		switch update.Status {
		case ProgressStateSucceeded, ProgressStateErrored, ProgressStateCancelled:
			done++
		}
	}
	return len(updates), done
}

// computeProgressFinishedConfidence calculates how confident we are that a run has finished.
func computeProgressFinishedConfidence(firstSeen, lastSeen time.Time, count, countDone int) float64 {
	now := time.Now()
	var confidence float64

	// We are not finished yet, so be inconfident
	if count != countDone || countDone == 0 {
		return confidence
	}

	// Calculate the average time an update takes, assumption being that
	// we should wait for the average time after the last update before
	// being confident that none will arrive.
	elapsedTime := lastSeen.Sub(firstSeen).Milliseconds()
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
