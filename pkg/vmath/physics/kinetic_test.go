package physics

import (
	"math"
	"testing"
)

func TestIntegrateConstantVelocity(t *testing.T) {
	k := newKin(0, 0, 10, -4)
	gx, gy := Integrate(&k, 0.5)

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
	k.AccelX = 2
	Integrate(&k, 1.0)

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
	ApplyImpulse(&k, 1, 2)
	if vx, vy := velOf(&k); math.Abs(vx-4) > 1e-6 || math.Abs(vy-6) > 1e-6 {
		t.Fatalf("ApplyImpulse = (%v,%v), want (4,6)", vx, vy)
	}
	SetImpulse(&k, -1, 0)
	if vx, vy := velOf(&k); math.Abs(vx+1) > 1e-6 || vy != 0 {
		t.Fatalf("SetImpulse = (%v,%v), want (-1,0)", vx, vy)
	}
}

func TestReflectBoundsX(t *testing.T) {
	k := kin{PreciseX: -0.5, VelX: -3}
	if !ReflectBoundsX(&k, 0, 10) {
		t.Fatal("left boundary not detected")
	}
	if x, _ := posOf(&k); math.Abs(x-0.5) > 1e-6 {
		t.Fatalf("clamped to %v, want cell center 0.5", x)
	}
	if vx, _ := velOf(&k); vx <= 0 {
		t.Fatalf("velocity not reflected: %v", vx)
	}

	k = kin{PreciseX: 10.2, VelX: 3}
	if !ReflectBoundsX(&k, 0, 10) {
		t.Fatal("right boundary not detected")
	}
	if x, _ := posOf(&k); math.Abs(x-9.5) > 1e-6 {
		t.Fatalf("clamped to %v, want cell center 9.5", x)
	}

	k = kin{PreciseX: 5.5}
	if ReflectBoundsX(&k, 0, 10) {
		t.Fatal("in-bounds position reported a reflection")
	}
}

func TestReflectBoundsY(t *testing.T) {
	k := kin{PreciseY: -0.1, VelY: -2}
	if !ReflectBoundsY(&k, 0, 8) {
		t.Fatal("top boundary not detected")
	}
	if _, y := posOf(&k); math.Abs(y-0.5) > 1e-6 {
		t.Fatalf("clamped to %v, want 0.5", y)
	}
}

func TestReflectBoundsCombined(t *testing.T) {
	k := kin{PreciseX: -1, PreciseY: 20}
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
	// SetGridPos must land on the cell center, not the cell origin.
	if x, y := posOf(&k); math.Abs(x-7.5) > 1e-6 || math.Abs(y+2.5) > 1e-6 {
		t.Fatalf("precise = (%v,%v), want (7.5,-2.5)", x, y)
	}
}

func TestCapSpeed(t *testing.T) {
	vx, vy := CapSpeed(1, 2, 5)
	if vx != 1 || vy != 2 {
		t.Error("under-limit velocity was modified")
	}

	vx, vy = CapSpeed(30, 40, 5)
	if m := math.Hypot(vx, vy); math.Abs(m-5) > 1e-5 {
		t.Errorf("capped speed = %v, want 5", m)
	}
}
