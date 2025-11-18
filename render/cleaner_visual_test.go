package render

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCleanerTrailGradientInitialized verifies the pre-calculated gradient is properly initialized
func TestCleanerTrailGradientInitialized(t *testing.T) {
	// Verify array has correct length
	if len(CleanerTrailGradient) != 10 {
		t.Fatalf("Expected gradient length 10, got %d", len(CleanerTrailGradient))
	}

	// Verify gradient values decrease from bright to dark
	for i := 0; i < len(CleanerTrailGradient)-1; i++ {
		current := CleanerTrailGradient[i]
		next := CleanerTrailGradient[i+1]

		// Extract RGB values
		r1, g1, b1 := current.RGB()
		r2, g2, b2 := next.RGB()

		// RGB values should decrease (fade to black)
		if r1 < r2 || g1 < g2 {
			t.Errorf("Gradient index %d should be brighter than %d: RGB(%d,%d,%d) vs RGB(%d,%d,%d)",
				i, i+1, r1, g1, b1, r2, g2, b2)
		}

		// Blue should always be 0 (yellow gradient)
		if b1 != 0 || b2 != 0 {
			t.Errorf("Yellow gradient should have blue=0, got b1=%d, b2=%d", b1, b2)
		}
	}
}

// TestCleanerTrailGradientBrightness verifies brightness progression
func TestCleanerTrailGradientBrightness(t *testing.T) {
	// First element (index 0) should be brightest (full yellow: 255, 255, 0)
	first := CleanerTrailGradient[0]
	r, g, b := first.RGB()

	if r != 255 || g != 255 || b != 0 {
		t.Errorf("First gradient color should be bright yellow RGB(255,255,0), got RGB(%d,%d,%d)", r, g, b)
	}

	// Last element (index 9) should be faintest (opacity 0.1: ~25, ~25, 0)
	last := CleanerTrailGradient[9]
	r, g, b = last.RGB()

	// Allow for rounding: should be around 25 (255 * 0.1 = 25.5)
	expectedR := int32(25) // 255 * 0.1 = 25.5
	expectedG := int32(25)

	tolerance := int32(2) // Allow ±2 for rounding
	if abs32(r-expectedR) > tolerance || abs32(g-expectedG) > tolerance || b != 0 {
		t.Errorf("Last gradient color should be ~RGB(25,25,0), got RGB(%d,%d,%d)", r, g, b)
	}
}

// TestCleanerTrailGradientUniformFade verifies uniform fade distribution
func TestCleanerTrailGradientUniformFade(t *testing.T) {
	// Check that each step reduces brightness by approximately 10% (1/10th)
	for i := 0; i < len(CleanerTrailGradient)-1; i++ {
		current := CleanerTrailGradient[i]
		next := CleanerTrailGradient[i+1]

		r1, g1, _ := current.RGB()
		r2, g2, _ := next.RGB()

		// Expected step: 255 / 10 = 25.5 per step
		stepR := r1 - r2
		stepG := g1 - g2

		// Each step should reduce by approximately 25-26 units
		expectedStep := int32(25)
		tolerance := int32(3)

		if abs32(stepR-expectedStep) > tolerance || abs32(stepG-expectedStep) > tolerance {
			t.Errorf("Gradient step %d→%d should reduce by ~%d units, got R:%d G:%d",
				i, i+1, expectedStep, stepR, stepG)
		}
	}
}

// TestCleanerGradientIsYellow verifies all colors in gradient are shades of yellow
func TestCleanerGradientIsYellow(t *testing.T) {
	for i, color := range CleanerTrailGradient {
		r, g, b := color.RGB()

		// Yellow should have equal R and G, and zero B
		if r != g {
			t.Errorf("Gradient index %d should have equal R and G for yellow, got R=%d G=%d", i, r, g)
		}
		if b != 0 {
			t.Errorf("Gradient index %d should have B=0 for yellow, got B=%d", i, b)
		}
	}
}

// TestCleanerGradientNoAllocation verifies gradient is pre-allocated
func TestCleanerGradientNoAllocation(t *testing.T) {
	// Access gradient multiple times - should be the same array
	first := &CleanerTrailGradient
	second := &CleanerTrailGradient

	if first != second {
		t.Error("Gradient should be a single pre-allocated array")
	}

	// Verify it's an array, not a slice (compile-time check)
	var _ [10]tcell.Color = CleanerTrailGradient
}

// Helper function
func abs32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
