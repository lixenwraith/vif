package visual

import (
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
