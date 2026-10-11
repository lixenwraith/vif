package renderer

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
)

func TestTracerVisibleTrailShrinksWhileBlocked(t *testing.T) {
	tracer := component.TracerComponent{
		TrailLen:       10,
		Blocked:        true,
		DrainRemaining: 5,
		DrainTotal:     10,
	}
	if got := tracerVisibleTrailLen(&tracer); got != 5 {
		t.Fatalf("half-drained trail length = %d, want 5", got)
	}

	tracer.DrainRemaining = 0.01
	if got := tracerVisibleTrailLen(&tracer); got != 1 {
		t.Fatalf("nearly-drained trail length = %d, want 1", got)
	}

	tracer.DrainRemaining = 0
	if got := tracerVisibleTrailLen(&tracer); got != 0 {
		t.Fatalf("drained trail length = %d, want 0", got)
	}
}
