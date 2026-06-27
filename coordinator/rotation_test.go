package coordinator

import (
	"os"
	"testing"
)

func TestPickRotationSubsetSize(t *testing.T) {
	tests := []struct {
		name         string
		offlineCount int
		expectedMin  int
		expectedMax  int
	}{
		{"zero offline", 0, 0, 0},
		{"one offline", 1, 1, 1},
		{"two offline", 2, 1, 1},
		{"three offline", 3, 1, 1},
		{"four offline", 4, 1, 1},
		{"five offline", 5, 2, 2},
		{"eight offline", 8, 2, 2},
		{"nine offline", 9, 3, 3},
		{"sixteen offline", 16, 4, 4},
		{"hundred offline", 100, 25, 25},
		{"one hundred one", 101, 26, 26},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickRotationSubsetSize(tt.offlineCount)
			if got < tt.expectedMin || got > tt.expectedMax {
				t.Errorf("pickRotationSubsetSize(%d) = %d, want between %d and %d",
					tt.offlineCount, got, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

// TestStartStopLoop ensures StartOfflineRotationLoop doesn't panic when
// called in isolated mode (no-op guard).
func TestStartStopOfflineRotationIsolated(t *testing.T) {
	os.Setenv("CHANNEL_POOL_MODE", "isolated")
	defer os.Unsetenv("CHANNEL_POOL_MODE")

	c := &Coordinator{
		NodeID: "test-rotator",
		Mode:   "isolated",
	}
	c.stopCh = make(chan struct{})

	// Should be a no-op — no panic.
	c.StartOfflineRotationLoop(nil)
}

// TestStartStopOfflineRotationNilClient ensures the loop exits early when
// Client is nil (no-op guard).
func TestStartStopOfflineRotationNilClient(t *testing.T) {
	os.Setenv("CHANNEL_POOL_MODE", "pooled")
	defer os.Unsetenv("CHANNEL_POOL_MODE")

	c := &Coordinator{
		NodeID: "test-rotator",
		Mode:   "pooled",
		Client: nil,
	}
	c.stopCh = make(chan struct{})

	// Should be a no-op — no panic.
	c.StartOfflineRotationLoop(nil)
}
