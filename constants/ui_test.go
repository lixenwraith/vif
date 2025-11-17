package constants

import (
	"testing"
	"time"
)

// TestDecayIntervalFormula verifies the decay interval formula matches expected values
func TestDecayIntervalFormula(t *testing.T) {
	tests := []struct {
		name     string
		heat     float64
		expected time.Duration
	}{
		{
			name:     "Zero heat",
			heat:     0.0,
			expected: 60 * time.Second,
		},
		{
			name:     "Half heat",
			heat:     0.5,
			expected: 35 * time.Second,
		},
		{
			name:     "Max heat",
			heat:     1.0,
			expected: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply formula: base - range * heat_percentage
			intervalSeconds := DecayIntervalBaseSeconds - DecayIntervalRangeSeconds*tt.heat
			actual := time.Duration(intervalSeconds * float64(time.Second))

			if actual != tt.expected {
				t.Errorf("Expected interval %v, got %v", tt.expected, actual)
			}
		})
	}
}

// TestDecayIntervalBounds verifies min and max decay intervals
func TestDecayIntervalBounds(t *testing.T) {
	if DecayIntervalMinSeconds != 10 {
		t.Errorf("Expected DecayIntervalMinSeconds to be 10, got %d", DecayIntervalMinSeconds)
	}

	maxInterval := DecayIntervalBaseSeconds
	if maxInterval != 60 {
		t.Errorf("Expected max interval to be 60 seconds, got %d", maxInterval)
	}

	if DecayIntervalMinSeconds >= DecayIntervalBaseSeconds {
		t.Error("Min interval should be less than base interval")
	}
}

// TestUITimingConstants verifies UI timing constants are reasonable
func TestUITimingConstants(t *testing.T) {
	if ErrorCursorTimeout != 200*time.Millisecond {
		t.Errorf("Expected ErrorCursorTimeout to be 200ms, got %v", ErrorCursorTimeout)
	}

	if ScoreBlinkTimeout != 300*time.Millisecond {
		t.Errorf("Expected ScoreBlinkTimeout to be 300ms, got %v", ScoreBlinkTimeout)
	}

	if BoostExtensionDuration != 1*time.Second {
		t.Errorf("Expected BoostExtensionDuration to be 1s, got %v", BoostExtensionDuration)
	}

	if DecayRowAnimationDuration != 100*time.Millisecond {
		t.Errorf("Expected DecayRowAnimationDuration to be 100ms, got %v", DecayRowAnimationDuration)
	}
}

// TestHeatBarIndicatorWidth verifies the heat bar indicator width
func TestHeatBarIndicatorWidth(t *testing.T) {
	if HeatBarIndicatorWidth != 6 {
		t.Errorf("Expected HeatBarIndicatorWidth to be 6, got %d", HeatBarIndicatorWidth)
	}

	// Verify it's large enough for a 4-digit number plus spacing
	// Format: " 9999 " = 6 characters
	if HeatBarIndicatorWidth < 6 {
		t.Error("HeatBarIndicatorWidth too small for 4-digit heat value display")
	}
}
