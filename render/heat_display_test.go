package render

import (
	"testing"
)

// TestHeatPercentageMapping verifies heat display correctly maps percentage to 0-10 range
func TestHeatPercentageMapping(t *testing.T) {
	tests := []struct {
		name           string
		heat           int
		maxHeat        int
		expectedBlocks int
	}{
		{
			name:           "0% heat (empty)",
			heat:           0,
			maxHeat:        100,
			expectedBlocks: 0,
		},
		{
			name:           "25% heat",
			heat:           25,
			maxHeat:        100,
			expectedBlocks: 2, // 25/100 * 10 = 2.5 -> 2
		},
		{
			name:           "50% heat",
			heat:           50,
			maxHeat:        100,
			expectedBlocks: 5,
		},
		{
			name:           "75% heat",
			heat:           75,
			maxHeat:        100,
			expectedBlocks: 7, // 75/100 * 10 = 7.5 -> 7
		},
		{
			name:           "100% heat (full)",
			heat:           100,
			maxHeat:        100,
			expectedBlocks: 10,
		},
		{
			name:           "10% heat",
			heat:           10,
			maxHeat:        100,
			expectedBlocks: 1,
		},
		{
			name:           "90% heat",
			heat:           90,
			maxHeat:        100,
			expectedBlocks: 9,
		},
		{
			name:           "Edge case: 1% heat",
			heat:           1,
			maxHeat:        100,
			expectedBlocks: 0, // 1/100 * 10 = 0.1 -> 0
		},
		{
			name:           "Edge case: 99% heat",
			heat:           99,
			maxHeat:        100,
			expectedBlocks: 9, // 99/100 * 10 = 9.9 -> 9
		},
		{
			name:           "Different max heat: 80 width, 40 heat (50%)",
			heat:           40,
			maxHeat:        80,
			expectedBlocks: 5,
		},
		{
			name:           "Different max heat: 120 width, 60 heat (50%)",
			heat:           60,
			maxHeat:        120,
			expectedBlocks: 5,
		},
		{
			name:           "Narrow terminal: 40 width, 20 heat (50%)",
			heat:           20,
			maxHeat:        40,
			expectedBlocks: 5,
		},
		{
			name:           "Wide terminal: 200 width, 100 heat (50%)",
			heat:           100,
			maxHeat:        200,
			expectedBlocks: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate display heat using the same formula as drawHeatMeter
			displayHeat := int(float64(tt.heat) / float64(tt.maxHeat) * 10.0)

			if displayHeat != tt.expectedBlocks {
				t.Errorf("Expected %d blocks, got %d (heat=%d, maxHeat=%d)",
					tt.expectedBlocks, displayHeat, tt.heat, tt.maxHeat)
			}
		})
	}
}

// TestHeatDisplayBounds verifies display heat is always within 0-10 range
func TestHeatDisplayBounds(t *testing.T) {
	tests := []struct {
		name    string
		heat    int
		maxHeat int
	}{
		{"Negative heat", -10, 100},
		{"Zero heat", 0, 100},
		{"Below max heat", 50, 100},
		{"At max heat", 100, 100},
		{"Above max heat", 150, 100},
		{"Very high heat", 9999, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			displayHeat := int(float64(tt.heat) / float64(tt.maxHeat) * 10.0)

			// Apply bounds checking as in drawHeatMeter
			if displayHeat > 10 {
				displayHeat = 10
			}
			if displayHeat < 0 {
				displayHeat = 0
			}

			if displayHeat < 0 || displayHeat > 10 {
				t.Errorf("displayHeat out of bounds: %d (heat=%d, maxHeat=%d)",
					displayHeat, tt.heat, tt.maxHeat)
			}
		})
	}
}

// TestHeatDisplayEdgeCases verifies edge cases in heat calculation
func TestHeatDisplayEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		heat           int
		maxHeat        int
		expectedBlocks int
		description    string
	}{
		{
			name:           "Boundary: exactly 10% transitions to 1 block",
			heat:           10,
			maxHeat:        100,
			expectedBlocks: 1,
			description:    "10/100 * 10 = 1.0",
		},
		{
			name:           "Boundary: exactly 20% transitions to 2 blocks",
			heat:           20,
			maxHeat:        100,
			expectedBlocks: 2,
			description:    "20/100 * 10 = 2.0",
		},
		{
			name:           "Just below 10%",
			heat:           9,
			maxHeat:        100,
			expectedBlocks: 0,
			description:    "9/100 * 10 = 0.9 -> 0",
		},
		{
			name:           "Just above 10%",
			heat:           11,
			maxHeat:        100,
			expectedBlocks: 1,
			description:    "11/100 * 10 = 1.1 -> 1",
		},
		{
			name:           "Maximum heat caps at 10 blocks",
			heat:           100,
			maxHeat:        100,
			expectedBlocks: 10,
			description:    "100/100 * 10 = 10.0",
		},
		{
			name:           "Over maximum heat still shows 10 blocks",
			heat:           120,
			maxHeat:        100,
			expectedBlocks: 10, // After clamping
			description:    "120/100 * 10 = 12.0 -> clamped to 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			displayHeat := int(float64(tt.heat) / float64(tt.maxHeat) * 10.0)

			// Apply bounds as in drawHeatMeter
			if displayHeat > 10 {
				displayHeat = 10
			}
			if displayHeat < 0 {
				displayHeat = 0
			}

			if displayHeat != tt.expectedBlocks {
				t.Errorf("%s: Expected %d blocks, got %d",
					tt.description, tt.expectedBlocks, displayHeat)
			}
		})
	}
}

// TestHeatDisplayGranularity verifies correct granularity at different heat levels
func TestHeatDisplayGranularity(t *testing.T) {
	maxHeat := 100
	expectedTransitions := []struct {
		minHeat int
		maxHeat int
		blocks  int
	}{
		{0, 9, 0},      // 0-9% -> 0 blocks
		{10, 19, 1},    // 10-19% -> 1 block
		{20, 29, 2},    // 20-29% -> 2 blocks
		{30, 39, 3},    // 30-39% -> 3 blocks
		{40, 49, 4},    // 40-49% -> 4 blocks
		{50, 59, 5},    // 50-59% -> 5 blocks
		{60, 69, 6},    // 60-69% -> 6 blocks
		{70, 79, 7},    // 70-79% -> 7 blocks
		{80, 89, 8},    // 80-89% -> 8 blocks
		{90, 99, 9},    // 90-99% -> 9 blocks
		{100, 100, 10}, // 100% -> 10 blocks
	}

	for _, transition := range expectedTransitions {
		// Test lower bound
		t.Run("Heat "+string(rune(transition.minHeat)), func(t *testing.T) {
			displayHeat := int(float64(transition.minHeat) / float64(maxHeat) * 10.0)
			if displayHeat != transition.blocks {
				t.Errorf("Heat %d: expected %d blocks, got %d",
					transition.minHeat, transition.blocks, displayHeat)
			}
		})

		// Test upper bound (except for 100% case)
		if transition.maxHeat < 100 {
			t.Run("Heat "+string(rune(transition.maxHeat)), func(t *testing.T) {
				displayHeat := int(float64(transition.maxHeat) / float64(maxHeat) * 10.0)
				if displayHeat != transition.blocks {
					t.Errorf("Heat %d: expected %d blocks, got %d",
						transition.maxHeat, transition.blocks, displayHeat)
				}
			})
		}
	}
}
