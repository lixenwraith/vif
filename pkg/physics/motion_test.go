package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

const tickDt = 1.0 / 60.0

func TestApplyHomingDeadZoneSnap(t *testing.T) {
	tx, ty := vmath.FromFloat(10), vmath.FromFloat(10)
	k := newKin(10.2, 10.1, 0.1, 0)

	if !ApplyHoming(&k, tx, ty, &profBraked, vmath.FromFloat(tickDt)) {
		t.Fatal("inside dead zone at low speed must settle")
	}
	if k.PreciseX != tx || k.PreciseY != ty {
		t.Fatal("settle must snap to the exact target")
	}
	if k.VelX != 0 || k.VelY != 0 {
		t.Fatal("settle must zero velocity")
	}
}

func TestApplyHomingDeadZoneRequiresLowSpeed(t *testing.T) {
	tx, ty := vmath.FromFloat(10), vmath.FromFloat(10)
	k := newKin(10.2, 10.0, 20, 0) // inside dead zone but fast
	if ApplyHoming(&k, tx, ty, &profBraked, vmath.FromFloat(tickDt)) {
		t.Fatal("fast transit through the dead zone must not settle")
	}
}

func TestApplyHomingBrakedProfileSettles(t *testing.T) {
	tx, ty := vmath.FromFloat(20), vmath.FromFloat(0)
	k := newKin(0, 0, 0, 0)
	dt := vmath.FromFloat(tickDt)

	settled := false
	steps := 0
	for i := range 3000 {
		if ApplyHoming(&k, tx, ty, &profBraked, dt) {
			settled, steps = true, i
			break
		}
		Integrate(&k, dt)
	}
	if !settled {
		t.Fatalf("continuous-drag profile failed to settle; final distance %.3f cells, speed %.3f",
			distTo(&k, 20, 0), speedOf(&k))
	}
	t.Logf("settled after %d steps (%.2fs)", steps, float64(steps)*tickDt)
}

func TestApplyHomingBrakedProfileDoesNotOvershoot(t *testing.T) {
	tx, ty := vmath.FromFloat(20), vmath.FromFloat(0)
	k := newKin(0, 0, 0, 0)
	dt := vmath.FromFloat(tickDt)

	maxX := 0.0
	for range 3000 {
		if ApplyHoming(&k, tx, ty, &profBraked, dt) {
			break
		}
		Integrate(&k, dt)
		x, _ := posOf(&k)
		maxX = math.Max(maxX, x)
	}
	if maxX > 21.0 {
		t.Fatalf("arrival steering overshot to %.3f cells, want <= 21", maxX)
	}
}

// TestApplyHomingCruiseProfileOrbits characterizes the shipped species pattern:
// drag engages only above BaseSpeed, so nothing decelerates the actor inside the
// arrival radius and it settles into a residual orbit instead of stopping.
// The assertion is the bound (no divergence), which survives a future fix.
func TestApplyHomingCruiseProfileStaysBounded(t *testing.T) {
	tx, ty := vmath.FromFloat(20), vmath.FromFloat(0)
	k := newKin(0, 0, 0, 0)
	dt := vmath.FromFloat(tickDt)

	settled := false
	lateMax := 0.0
	for i := range 3000 {
		if ApplyHoming(&k, tx, ty, &profCruise, dt) {
			settled = true
			break
		}
		Integrate(&k, dt)
		if i >= 2000 {
			lateMax = math.Max(lateMax, distTo(&k, 20, 0))
		}
	}
	if lateMax > 3.0 {
		t.Fatalf("cruise homing diverged: residual distance %.3f cells", lateMax)
	}
	t.Logf("cruise profile: settled=%v residual=%.3f cells", settled, lateMax)
}

func TestApplyHomingScaledSpeedMultiplier(t *testing.T) {
	tx := vmath.FromFloat(50)
	dt := vmath.FromFloat(tickDt)

	slow := newKin(0, 0, 0, 0)
	fast := newKin(0, 0, 0, 0)
	for range 30 {
		ApplyHomingScaled(&slow, tx, 0, &profCruise, vmath.Scale, dt, true)
		ApplyHomingScaled(&fast, tx, 0, &profCruise, vmath.Scale*2, dt, true)
		Integrate(&slow, dt)
		Integrate(&fast, dt)
	}
	if speedOf(&fast) <= speedOf(&slow) {
		t.Fatalf("speed multiplier had no effect: %.3f vs %.3f", speedOf(&fast), speedOf(&slow))
	}
}

func TestApplyHomingScaledDragDisabled(t *testing.T) {
	tx := vmath.FromFloat(500)
	dt := vmath.FromFloat(tickDt)

	dragged := newKin(0, 0, 0, 0)
	free := newKin(0, 0, 0, 0)
	for range 600 {
		ApplyHomingScaled(&dragged, tx, 0, &profCruise, vmath.Scale, dt, true)
		ApplyHomingScaled(&free, tx, 0, &profCruise, vmath.Scale, dt, false)
		Integrate(&dragged, dt)
		Integrate(&free, dt)
	}
	if speedOf(&free) <= speedOf(&dragged) {
		t.Fatalf("applyDrag=false did not raise terminal speed: %.3f vs %.3f",
			speedOf(&free), speedOf(&dragged))
	}
}

// --- IntegrateWithBounce ---

func TestIntegrateWithBounceBoundaryRestitution(t *testing.T) {
	k := newKin(5.5, 5.5, 100, 0)
	_, _, hit := IntegrateWithBounce(&k, vmath.FromFloat(0.1), 0, 0, 0, 10, 0, 10,
		vmath.Scale, noWall)
	if !hit {
		t.Fatal("boundary collision not reported")
	}
	if vx, _ := velOf(&k); vx >= 0 {
		t.Fatalf("velocity not reflected: %v", vx)
	}
	if m := math.Abs(func() float64 { vx, _ := velOf(&k); return vx }()); math.Abs(m-100) > 1 {
		t.Fatalf("elastic restitution changed speed: %v", m)
	}
}

func TestIntegrateWithBounceZeroRestitution(t *testing.T) {
	k := newKin(5.5, 5.5, 100, 0)
	_, _, hit := IntegrateWithBounce(&k, vmath.FromFloat(0.1), 0, 0, 0, 10, 0, 10,
		0, noWall)
	if !hit {
		t.Fatal("boundary collision not reported")
	}
	if k.VelX != 0 {
		t.Fatalf("zero restitution left velocity %d", k.VelX)
	}
}

func TestIntegrateWithBounceNoWallTunneling(t *testing.T) {
	wall := func(x, _ int) bool { return x == 7 }
	k := newKin(2.5, 5.5, 50, 0)

	gx, _, hit := IntegrateWithBounce(&k, vmath.FromFloat(0.2), 0, 0, 0, 40, 0, 20,
		vmath.Scale, wall)
	if !hit {
		t.Fatal("wall collision not reported")
	}
	if gx >= 7 {
		t.Fatalf("tunneled through the wall to cell %d", gx)
	}
	if vx, _ := velOf(&k); vx >= 0 {
		t.Fatalf("velocity not reflected off the wall: %v", vx)
	}
}

func TestIntegrateWithBounceRespectsHeaderOffset(t *testing.T) {
	// footprint top-left is 2 cells left of the header; the wall test receives
	// the top-left, so the header must stop 2 cells earlier
	wall := func(x, _ int) bool { return x == 7 }
	k := newKin(4.5, 5.5, 50, 0)

	gx, _, hit := IntegrateWithBounce(&k, vmath.FromFloat(0.2), 2, 1, 0, 40, 0, 20,
		vmath.Scale, wall)
	if !hit {
		t.Fatal("offset wall collision not reported")
	}
	if gx >= 9 {
		t.Fatalf("header reached %d, footprint would overlap the wall", gx)
	}
}

func TestIntegrateWithBounceStaysInBounds(t *testing.T) {
	k := newKin(5.5, 5.5, 4000, -3000)
	gx, gy, _ := IntegrateWithBounce(&k, vmath.FromFloat(0.05), 0, 0, 0, 10, 0, 10,
		vmath.FromFloat(0.5), noWall)
	if gx < 0 || gx >= 10 || gy < 0 || gy >= 10 {
		t.Fatalf("extreme velocity escaped bounds: (%d,%d)", gx, gy)
	}
}

// --- orbital ---

func TestOrbitalVelocity(t *testing.T) {
	// v = sqrt(a*r): a = 2, r = 4 -> v = sqrt(8)
	got := vmath.ToFloat(OrbitalVelocity(vmath.FromFloat(2), vmath.FromFloat(4)))
	if math.Abs(got-math.Sqrt(8)) > 1e-5 {
		t.Fatalf("OrbitalVelocity = %v, want %v", got, math.Sqrt(8))
	}
}

func TestOrbitalInsertIsTangential(t *testing.T) {
	dx, dy := vmath.FromFloat(4), int64(0)
	vx, vy := OrbitalInsert(dx, dy, vmath.FromFloat(2), false)

	if d := vmath.DotProduct(dx, dy, vx, vy); vmath.Abs(d) > vmath.Scale {
		t.Fatalf("insertion velocity not tangential (dot = %v)", vmath.ToFloat(d))
	}
	if m := math.Hypot(vmath.ToFloat(vx), vmath.ToFloat(vy)); math.Abs(m-math.Sqrt(8)) > 1e-4 {
		t.Fatalf("insertion speed = %v, want %v", m, math.Sqrt(8))
	}

	cwX, cwY := OrbitalInsert(dx, dy, vmath.FromFloat(2), true)
	if cwX != -vx || cwY != -vy {
		t.Fatal("clockwise insertion must invert the tangent")
	}

	if x, y := OrbitalInsert(0, 0, vmath.FromFloat(2), false); x != 0 || y != 0 {
		t.Fatal("zero radius must yield zero velocity")
	}
}

func TestOrbitalAttractionPointsInward(t *testing.T) {
	ax, ay := OrbitalAttraction(vmath.FromFloat(3), vmath.FromFloat(4), vmath.FromFloat(10))
	if ax >= 0 || ay >= 0 {
		t.Fatalf("attraction = (%v,%v), want inward", vmath.ToFloat(ax), vmath.ToFloat(ay))
	}
	if m := math.Hypot(vmath.ToFloat(ax), vmath.ToFloat(ay)); math.Abs(m-10) > 1e-4 {
		t.Fatalf("attraction magnitude = %v, want 10 (linear, not inverse square)", m)
	}
	if x, y := OrbitalAttraction(0, 0, vmath.FromFloat(10)); x != 0 || y != 0 {
		t.Fatal("zero offset must yield zero attraction")
	}
}

func TestOrbitalDampReducesRadialComponent(t *testing.T) {
	// pure radial velocity, damping*dt = 0.5 -> radial halved
	vx, vy := OrbitalDamp(vmath.FromFloat(1), 0, vmath.FromFloat(4), 0,
		vmath.FromFloat(1.0), vmath.FromFloat(0.5))
	if math.Abs(vmath.ToFloat(vx)-0.5) > 1e-6 || vy != 0 {
		t.Fatalf("damped velocity = (%v,%v), want (0.5,0)", vmath.ToFloat(vx), vmath.ToFloat(vy))
	}
}

func TestOrbitalDampPreservesTangential(t *testing.T) {
	// velocity perpendicular to the radius has no radial component to damp
	vx, vy := OrbitalDamp(0, vmath.FromFloat(3), vmath.FromFloat(4), 0,
		vmath.FromFloat(1.0), vmath.FromFloat(0.5))
	if math.Abs(vmath.ToFloat(vy)-3) > 1e-5 || math.Abs(vmath.ToFloat(vx)) > 1e-5 {
		t.Fatalf("tangential velocity altered: (%v,%v)", vmath.ToFloat(vx), vmath.ToFloat(vy))
	}
}

func TestOrbitalEquilibriumSign(t *testing.T) {
	// outside the target radius: pull inward
	ax, _ := OrbitalEquilibrium(vmath.FromFloat(10), 0, vmath.FromFloat(5), vmath.Scale)
	if ax >= 0 {
		t.Fatalf("outside target: ax = %v, want inward", vmath.ToFloat(ax))
	}
	// inside the target radius: push outward
	ax, _ = OrbitalEquilibrium(vmath.FromFloat(2), 0, vmath.FromFloat(5), vmath.Scale)
	if ax <= 0 {
		t.Fatalf("inside target: ax = %v, want outward", vmath.ToFloat(ax))
	}
	// at the target radius: no force
	ax, _ = OrbitalEquilibrium(vmath.FromFloat(5), 0, vmath.FromFloat(5), vmath.Scale)
	if vmath.Abs(ax) > vmath.Scale/1000 {
		t.Fatalf("at target: ax = %v, want ~0", vmath.ToFloat(ax))
	}
}
