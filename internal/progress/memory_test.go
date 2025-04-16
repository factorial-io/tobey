package progress

import (
	"testing"
	"time"
)

func TestComputeProgressConfidenceLevel(t *testing.T) {
	// Create a fixed reference time for consistent testing
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		firstSeen time.Time
		lastSeen  time.Time
		count     int
		countDone int
		want      float64
	}{
		{
			name:      "not finished - count not equal to countDone",
			firstSeen: baseTime,
			lastSeen:  baseTime.Add(10 * time.Second),
			count:     10,
			countDone: 8,
			want:      0.0,
		},
		{
			name:      "not finished - countDone is 0",
			firstSeen: baseTime,
			lastSeen:  baseTime.Add(10 * time.Second),
			count:     10,
			countDone: 0,
			want:      0.0,
		},
		{
			name:      "zero elapsed time",
			firstSeen: baseTime,
			lastSeen:  baseTime,
			count:     10,
			countDone: 10,
			want:      0.0,
		},
		{
			name:      "finished - one second elapsed",
			firstSeen: baseTime,
			lastSeen:  baseTime.Add(1 * time.Second),
			count:     1,
			countDone: 1,
			want:      1.0,
		},
		{
			name:      "finished - one milliseconds elapsed",
			firstSeen: baseTime,
			lastSeen:  baseTime.Add(1 * time.Millisecond),
			count:     1,
			countDone: 1,
			want:      1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeProgressFinishedConfidence(tt.firstSeen, tt.lastSeen, tt.count, tt.countDone)

			// Use a small epsilon for float comparison
			epsilon := 0.001
			if abs(got-tt.want) > epsilon {
				t.Errorf("computeProgressConfidenceLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to calculate absolute difference between floats
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
