package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func TestIntegrateConstantVelocity(t *testing.T) {
	k := newKin(0, 0, 10, -4)
	gx, gy := Integrate(&k, vmath.FromFloat(0.5))

	x, y := posOf(&k)
	if math.Abs(x-5) > 1e-6 || math.Abs(y+2) > 1e-6 {
		t.Fatalf("position = (%v,%v), want (5,-2)", x, y)
	}
	if gx != 5 || gy != -2 {
		t.Fatalf("grid = (%d,%d), want (5,-2)", gx, gy)
	}
	if vx, vy := velOf(&k); math.Abs(vx-10) > 1e-6 || math.Abs(vy+4) > 1e-6 {
		t.Fatalf("velocity changed without acceleration: (%v,%v)", vx, vy)
	}
}

func TestIntegrateSemiImplicitEuler(t *testing.T) {
	k := newKin(0, 0, 10, 0)
	k.AccelX = vmath.FromFloat(2)
	Integrate(&k, vmath.FromFloat(1.0))

	// velocity updates before position: v = 12, p = 0 + 12*1
	if vx, _ := velOf(&k); math.Abs(vx-12) > 1e-6 {
		t.Fatalf("velocity = %v, want 12", vx)
	}
	if x, _ := posOf(&k); math.Abs(x-12) > 1e-6 {
		t.Fatalf("position = %v, want 12 (semi-implicit)", x)
	}
}

func TestApplyAndSetImpulse(t *testing.T) {
	k := newKin(0, 0, 3, 4)
	ApplyImpulse(&k, vmath.FromFloat(1), vmath.FromFloat(2))
	if vx, vy := velOf(&k); math.Abs(vx-4) > 1e-6 || math.Abs(vy-6) > 1e-6 {
		t.Fatalf("ApplyImpulse = (%v,%v), want (4,6)", vx, vy)
	}
	SetImpulse(&k, vmath.FromFloat(-1), 0)
	if vx, vy := velOf(&k); math.Abs(vx+1) > 1e-6 || vy != 0 {
		t.Fatalf("SetImpulse = (%v,%v), want (-1,0)", vx, vy)
	}
}

func TestReflectBoundsX(t *testing.T) {
	k := kin{PreciseX: vmath.FromFloat(-0.5), VelX: vmath.FromFloat(-3)}
	if !ReflectBoundsX(&k, 0, 10) {
		t.Fatal("left boundary not detected")
	}
	if x, _ := posOf(&k); math.Abs(x-0.5) > 1e-6 {
		t.Fatalf("clamped to %v, want cell center 0.5", x)
	}
	if vx, _ := velOf(&k); vx <= 0 {
		t.Fatalf("velocity not reflected: %v", vx)
	}

	k = kin{PreciseX: vmath.FromFloat(10.2), VelX: vmath.FromFloat(3)}
	if !ReflectBoundsX(&k, 0, 10) {
		t.Fatal("right boundary not detected")
	}
	if x, _ := posOf(&k); math.Abs(x-9.5) > 1e-6 {
		t.Fatalf("clamped to %v, want cell center 9.5", x)
	}

	k = kin{PreciseX: vmath.FromFloat(5.5)}
	if ReflectBoundsX(&k, 0, 10) {
		t.Fatal("in-bounds position reported a reflection")
	}
}

func TestReflectBoundsY(t *testing.T) {
	k := kin{PreciseY: vmath.FromFloat(-0.1), VelY: vmath.FromFloat(-2)}
	if !ReflectBoundsY(&k, 0, 8) {
		t.Fatal("top boundary not detected")
	}
	if _, y := posOf(&k); math.Abs(y-0.5) > 1e-6 {
		t.Fatalf("clamped to %v, want 0.5", y)
	}
}

func TestReflectBoundsCombined(t *testing.T) {
	k := kin{PreciseX: vmath.FromFloat(-1), PreciseY: vmath.FromFloat(20)}
	if !ReflectBounds(&k, 10, 10) {
		t.Fatal("corner overflow not detected")
	}
	x, y := posOf(&k)
	if math.Abs(x-0.5) > 1e-6 || math.Abs(y-9.5) > 1e-6 {
		t.Fatalf("clamped to (%v,%v), want (0.5,9.5)", x, y)
	}
}

func TestGridPosRoundTrip(t *testing.T) {
	var k kin
	SetGridPos(&k, 7, -3)
	if x, y := GridPos(&k); x != 7 || y != -3 {
		t.Fatalf("GridPos = (%d,%d), want (7,-3)", x, y)
	}
	// SetGridPos must land on the cell center, not the cell origin
	if x, y := posOf(&k); math.Abs(x-7.5) > 1e-6 || math.Abs(y+2.5) > 1e-6 {
		t.Fatalf("precise = (%v,%v), want (7.5,-2.5)", x, y)
	}
}

func TestCapSpeed(t *testing.T) {
	maxSpeed := vmath.FromFloat(5)
	vx, vy := CapSpeed(vmath.FromFloat(1), vmath.FromFloat(2), maxSpeed)
	if vx != vmath.FromFloat(1) || vy != vmath.FromFloat(2) {
		t.Error("under-limit velocity was modified")
	}

	vx, vy = CapSpeed(vmath.FromFloat(30), vmath.FromFloat(40), maxSpeed)
	if m := math.Hypot(vmath.ToFloat(vx), vmath.ToFloat(vy)); math.Abs(m-5) > 1e-5 {
		t.Errorf("capped speed = %v, want 5", m)
	}
}
