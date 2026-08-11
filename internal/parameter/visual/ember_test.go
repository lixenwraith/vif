package visual

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func TestEmberEllipseUsesTerminalAspectRatio(t *testing.T) {
	if EmberRadiusX != 2*EmberRadiusY {
		t.Fatalf("ember radii = %v×%v, want a 2:1 terminal ellipse", EmberRadiusX, EmberRadiusY)
	}
	if !vmath.EllipseContainsF(EmberRadiusX, 0, EmberInvRxSq, EmberInvRySq) {
		t.Fatal("horizontal semi-axis must be inside the ember ellipse")
	}
	if !vmath.EllipseContainsF(0, EmberRadiusY, EmberInvRxSq, EmberInvRySq) {
		t.Fatal("vertical semi-axis must be inside the ember ellipse")
	}
	if vmath.EllipseContainsF(0, EmberRadiusY+1, EmberInvRxSq, EmberInvRySq) {
		t.Fatal("point beyond the vertical semi-axis must be outside the ember ellipse")
	}
}

func TestInterpolateEmberParamsUsesRadians(t *testing.T) {
	low := InterpolateEmberParams(0)
	high := InterpolateEmberParams(100)

	if low.HeatFactor != 0.0 || high.HeatFactor != 1.0 {
		t.Fatalf("heat factors = %v, %v; want 0, 1", low.HeatFactor, high.HeatFactor)
	}
	if math.Abs(low.RingSpeed-3.0*vmath.TwoPi) > 1e-12 {
		t.Fatalf("low-heat ring speed = %v, want 3 turns/sec in radians", low.RingSpeed)
	}
	if math.Abs(high.JaggedSpeed-6.0*vmath.TwoPi) > 1e-12 {
		t.Fatalf("high-heat jagged speed = %v, want 6 turns/sec in radians", high.JaggedSpeed)
	}
}
