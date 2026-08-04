package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func TestApplyCollisionImpulseMassRatio(t *testing.T) {
	ix, iy := ApplyCollisionImpulse(
		vmath.FromFloat(1), 0,
		vmath.Scale/2, // half mass ratio
		0,
		vmath.FromFloat(10), vmath.FromFloat(10),
		nil,
	)
	if math.Abs(vmath.ToFloat(ix)-5) > 1e-6 || iy != 0 {
		t.Fatalf("impulse = (%v,%v), want (5,0)", vmath.ToFloat(ix), vmath.ToFloat(iy))
	}
}

func TestApplyCollisionImpulseNormalizesDirection(t *testing.T) {
	// magnitude must depend on the profile, not the impactor's speed
	slow, _ := ApplyCollisionImpulse(vmath.FromFloat(1), 0, vmath.Scale, 0,
		vmath.FromFloat(10), vmath.FromFloat(10), nil)
	fast, _ := ApplyCollisionImpulse(vmath.FromFloat(500), 0, vmath.Scale, 0,
		vmath.FromFloat(10), vmath.FromFloat(10), nil)
	if slow != fast {
		t.Fatalf("impulse varied with impactor speed: %d vs %d", slow, fast)
	}
}

func TestApplyCollisionImpulseZeroVelocity(t *testing.T) {
	ix, iy := ApplyCollisionImpulse(0, 0, vmath.Scale, 0,
		vmath.FromFloat(10), vmath.FromFloat(10), nil)
	if ix != 0 || iy != 0 {
		t.Fatalf("zero impactor produced impulse (%d,%d)", ix, iy)
	}
}

func TestApplyCollisionImpulseDeterministic(t *testing.T) {
	run := func() (int64, int64) {
		rng := vmath.NewFastRand(0x5EED)
		return ApplyCollisionImpulse(
			vmath.FromFloat(1), vmath.FromFloat(1),
			vmath.Scale,
			vmath.FromFloat(0.35),
			vmath.FromFloat(15), vmath.FromFloat(40),
			rng,
		)
	}
	x1, y1 := run()
	x2, y2 := run()
	if x1 != x2 || y1 != y2 {
		t.Fatalf("same seed diverged: (%d,%d) vs (%d,%d)", x1, y1, x2, y2)
	}
}

func TestApplyCollisionImpulseMagnitudeBounds(t *testing.T) {
	rng := vmath.NewFastRand(0x5EEE)
	lo, hi := 15.0, 40.0
	for range 5000 {
		ix, iy := ApplyCollisionImpulse(
			vmath.FromFloat(1), vmath.FromFloat(-2),
			vmath.Scale,
			vmath.FromFloat(0.35),
			vmath.FromFloat(lo), vmath.FromFloat(hi),
			rng,
		)
		m := math.Hypot(vmath.ToFloat(ix), vmath.ToFloat(iy))
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
	ApplyCollision(&k, vmath.FromFloat(1), 0, &additive, nil)
	if vx, _ := velOf(&k); math.Abs(vx-13) > 1e-6 {
		t.Fatalf("additive = %v, want 13", vx)
	}

	override := profImpulse
	override.Mode = ImpulseOverride
	k = newKin(0, 0, 3, 0)
	ApplyCollision(&k, vmath.FromFloat(1), 0, &override, nil)
	if vx, _ := velOf(&k); math.Abs(vx-10) > 1e-6 {
		t.Fatalf("override = %v, want 10", vx)
	}
}

func TestApplyOffsetCollisionImpulsePureOffset(t *testing.T) {
	// offsetInfluence = Scale collapses the blend onto the offset direction:
	// the impulse must push away from the hit point
	ix, iy := ApplyOffsetCollisionImpulse(
		vmath.FromFloat(1), 0,
		2, 0, // hit 2 cells right of the anchor
		vmath.Scale,
		vmath.Scale,
		0,
		vmath.FromFloat(10), vmath.FromFloat(10),
		nil,
	)
	if vmath.ToFloat(ix) >= 0 || iy != 0 {
		t.Fatalf("impulse = (%v,%v), want negative X", vmath.ToFloat(ix), vmath.ToFloat(iy))
	}
	if m := math.Abs(vmath.ToFloat(ix)); math.Abs(m-10) > 1e-5 {
		t.Fatalf("magnitude = %v, want 10", m)
	}
}

func TestApplyOffsetCollisionImpulseZeroOffsetMatchesBase(t *testing.T) {
	base, _ := ApplyCollisionImpulse(vmath.FromFloat(1), vmath.FromFloat(1),
		vmath.Scale, 0, vmath.FromFloat(10), vmath.FromFloat(10), nil)
	off, _ := ApplyOffsetCollisionImpulse(vmath.FromFloat(1), vmath.FromFloat(1),
		0, 0, vmath.Scale, vmath.Scale, 0,
		vmath.FromFloat(10), vmath.FromFloat(10), nil)
	if base != off {
		t.Fatalf("zero offset diverged from base: %d vs %d", base, off)
	}
}

func TestCheckSoftCollision(t *testing.T) {
	invRx, invRy := vmath.EllipseInvRadiiSq(vmath.FromFloat(3), vmath.FromFloat(1.5))

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

	// radial vector points source -> target
	rx, ry, _ = CheckSoftCollision(12, 11, 10, 10, invRx, invRy)
	if rx <= 0 || ry <= 0 {
		t.Fatalf("radial = (%d,%d), want positive components", rx, ry)
	}
}

func TestRadiansToRotationConstant(t *testing.T) {
	// one radian must map to 1/(2pi) of a rotation
	got := vmath.ToFloat(vmath.Mul(vmath.FromFloat(1.0), RadiansToRotation))
	if math.Abs(got-1.0/(2*math.Pi)) > 1e-6 {
		t.Errorf("1 rad = %v rotations, want %v", got, 1.0/(2*math.Pi))
	}
}
