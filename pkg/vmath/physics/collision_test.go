package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func TestApplyCollisionImpulseMassRatio(t *testing.T) {
	ix, iy := ApplyCollisionImpulse(1, 0, 0.5, 0, 10, 10, nil)
	if math.Abs(ix-5) > 1e-6 || iy != 0 {
		t.Fatalf("impulse = (%v,%v), want (5,0)", ix, iy)
	}
}

func TestApplyCollisionImpulseNormalizesDirection(t *testing.T) {
	// Magnitude must depend on the profile, not the impactor's speed.
	slow, _ := ApplyCollisionImpulse(1, 0, 1, 0, 10, 10, nil)
	fast, _ := ApplyCollisionImpulse(500, 0, 1, 0, 10, 10, nil)
	if slow != fast {
		t.Fatalf("impulse varied with impactor speed: %v vs %v", slow, fast)
	}
}

func TestApplyCollisionImpulseZeroVelocity(t *testing.T) {
	ix, iy := ApplyCollisionImpulse(0, 0, 1, 0, 10, 10, nil)
	if ix != 0 || iy != 0 {
		t.Fatalf("zero impactor produced impulse (%v,%v)", ix, iy)
	}
}

func TestApplyCollisionImpulseDeterministic(t *testing.T) {
	run := func() (float64, float64) {
		rng := vmath.NewFastRand(0x5EED)
		return ApplyCollisionImpulse(1, 1, 1, 0.35, 15, 40, rng)
	}
	x1, y1 := run()
	x2, y2 := run()
	if x1 != x2 || y1 != y2 {
		t.Fatalf("same seed diverged: (%v,%v) vs (%v,%v)", x1, y1, x2, y2)
	}
}

func TestApplyCollisionImpulseMagnitudeBounds(t *testing.T) {
	rng := vmath.NewFastRand(0x5EEE)
	const lo, hi = 15.0, 40.0
	for range 5000 {
		ix, iy := ApplyCollisionImpulse(1, -2, 1, 0.35, lo, hi, rng)
		m := math.Hypot(ix, iy)
		if m < lo-1e-3 || m > hi+1e-3 {
			t.Fatalf("impulse magnitude %v outside [%v,%v]", m, lo, hi)
		}
	}
}

func TestApplyCollisionZeroDirectionFallback(t *testing.T) {
	k := kin{}
	ApplyCollision(&k, 0, 0, &profImpulse, nil)
	vx, vy := velOf(&k)
	if math.Abs(vx-10) > 1e-6 || vy != 0 {
		t.Fatalf("fallback impulse = (%v,%v), want (10,0)", vx, vy)
	}
}

func TestApplyCollisionModes(t *testing.T) {
	additive := profImpulse
	additive.Mode = ImpulseAdditive
	k := newKin(0, 0, 3, 0)
	ApplyCollision(&k, 1, 0, &additive, nil)
	if vx, _ := velOf(&k); math.Abs(vx-13) > 1e-6 {
		t.Fatalf("additive = %v, want 13", vx)
	}

	override := profImpulse
	override.Mode = ImpulseOverride
	k = newKin(0, 0, 3, 0)
	ApplyCollision(&k, 1, 0, &override, nil)
	if vx, _ := velOf(&k); math.Abs(vx-10) > 1e-6 {
		t.Fatalf("override = %v, want 10", vx)
	}
}

func TestApplyOffsetCollisionImpulsePureOffset(t *testing.T) {
	// An influence of 1.0 collapses the blend onto the offset direction:
	// the impulse must push away from the hit point.
	ix, iy := ApplyOffsetCollisionImpulse(
		1, 0,
		2, 0,
		1, 1, 0,
		10, 10,
		nil,
	)
	if ix >= 0 || iy != 0 {
		t.Fatalf("impulse = (%v,%v), want negative X", ix, iy)
	}
	if m := math.Abs(ix); math.Abs(m-10) > 1e-5 {
		t.Fatalf("magnitude = %v, want 10", m)
	}
}

func TestApplyOffsetCollisionImpulseZeroOffsetMatchesBase(t *testing.T) {
	base, _ := ApplyCollisionImpulse(1, 1, 1, 0, 10, 10, nil)
	off, _ := ApplyOffsetCollisionImpulse(1, 1, 0, 0, 1, 1, 0, 10, 10, nil)
	if base != off {
		t.Fatalf("zero offset diverged from base: %v vs %v", base, off)
	}
}

func TestCheckSoftCollision(t *testing.T) {
	invRx, invRy := vmath.EllipseInvRadiiSqF(3, 1.5)

	if _, _, hit := CheckSoftCollision(11, 10, 10, 10, invRx, invRy); !hit {
		t.Error("adjacent cell must collide")
	}
	if _, _, hit := CheckSoftCollision(14, 10, 10, 10, invRx, invRy); hit {
		t.Error("cell beyond the semi-axis must not collide")
	}

	rx, ry, hit := CheckSoftCollision(10, 10, 10, 10, invRx, invRy)
	if !hit {
		t.Fatal("co-located entities must collide")
	}
	if rx == 0 && ry == 0 {
		t.Fatal("co-located collision must produce a fallback direction")
	}

	// Radial vector points source -> target.
	rx, ry, _ = CheckSoftCollision(12, 11, 10, 10, invRx, invRy)
	if rx <= 0 || ry <= 0 {
		t.Fatalf("radial = (%v,%v), want positive components", rx, ry)
	}
}
