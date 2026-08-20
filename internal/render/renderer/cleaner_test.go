package renderer

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
)

func TestCleanerVisibleTrailShrinksWhileBlocked(t *testing.T) {
	cleaner := component.CleanerComponent{
		TrailLen:       10,
		Blocked:        true,
		DrainRemaining: 5,
		DrainTotal:     10,
	}
	if got := cleanerVisibleTrailLen(&cleaner); got != 5 {
		t.Fatalf("half-drained trail length = %d, want 5", got)
	}

	cleaner.DrainRemaining = 0.01
	if got := cleanerVisibleTrailLen(&cleaner); got != 1 {
		t.Fatalf("nearly-drained trail length = %d, want 1", got)
	}

	cleaner.DrainRemaining = 0
	if got := cleanerVisibleTrailLen(&cleaner); got != 0 {
		t.Fatalf("drained trail length = %d, want 0", got)
	}
}
