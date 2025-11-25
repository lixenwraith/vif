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
	if ErrorBlinkTimeout != 200*time.Millisecond {
		t.Errorf("Expected ErrorBlinkTimeout to be 200ms, got %v", ErrorBlinkTimeout)
	}

	if EnergyBlinkTimeout != 200*time.Millisecond {
		t.Errorf("Expected EnergyBlinkTimeout to be 200ms, got %v", EnergyBlinkTimeout)
	}

	if BoostExtensionDuration != 500*time.Millisecond {
		t.Errorf("Expected BoostExtensionDuration to be 500ms, got %v", BoostExtensionDuration)
	}

	if DecayRowAnimationDuration != 100*time.Millisecond {
		t.Errorf("Expected DecayRowAnimationDuration to be 100ms, got %v", DecayRowAnimationDuration)
	}
}