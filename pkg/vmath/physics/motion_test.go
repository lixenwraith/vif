package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

const tickDt = 1.0 / 60.0

func TestApplyHomingDeadZoneSnap(t *testing.T) {
	const tx, ty = 10.0, 10.0
	k := newKin(10.2, 10.1, 0.1, 0)

	if !ApplyHoming(&k, tx, ty, &profBraked, tickDt) {
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
	k := newKin(10.2, 10.0, 20, 0)
	if ApplyHoming(&k, 10, 10, &profBraked, tickDt) {
		t.Fatal("fast transit through the dead zone must not settle")
	}
}

func TestApplyHomingBrakedProfileSettles(t *testing.T) {
	k := newKin(0, 0, 0, 0)

	settled := false
	steps := 0
	for i := range 3000 {
		if ApplyHoming(&k, 20, 0, &profBraked, tickDt) {
			settled, steps = true, i
			break
		}
		Integrate(&k, tickDt)
	}
	if !settled {
		t.Fatalf("continuous-drag profile failed to settle; final distance %.3f cells, speed %.3f",
			distTo(&k, 20, 0), speedOf(&k))
	}
	t.Logf("settled after %d steps (%.2fs)", steps, float64(steps)*tickDt)
}

func TestApplyHomingBrakedProfileDoesNotOvershoot(t *testing.T) {
	k := newKin(0, 0, 0, 0)

	maxX := 0.0
	for range 3000 {
		if ApplyHoming(&k, 20, 0, &profBraked, tickDt) {
			break
		}
		Integrate(&k, tickDt)
		x, _ := posOf(&k)
		maxX = math.Max(maxX, x)
	}
	if maxX > 21.0 {
		t.Fatalf("arrival steering overshot to %.3f cells, want <= 21", maxX)
	}
}

func TestApplyHomingCruiseProfileSettles(t *testing.T) {
	k := newKin(0, 0, 0, 0)

	for i := range 3000 {
		if ApplyHoming(&k, 20, 0, &profCruise, tickDt) {
			t.Logf("settled after %d steps (%.2fs)", i, float64(i)*tickDt)
			return
		}
		Integrate(&k, tickDt)
	}
	t.Fatalf("cruise profile failed to settle; distance %.3f cells, speed %.3f",
		distTo(&k, 20, 0), speedOf(&k))
}

func TestApplyHomingScaledSpeedMultiplier(t *testing.T) {
	slow := newKin(0, 0, 0, 0)
	fast := newKin(0, 0, 0, 0)
	for range 30 {
		ApplyHomingScaled(&slow, 50, 0, &profCruise, 1, tickDt, true)
		ApplyHomingScaled(&fast, 50, 0, &profCruise, 2, tickDt, true)
		Integrate(&slow, tickDt)
		Integrate(&fast, tickDt)
	}
	if speedOf(&fast) <= speedOf(&slow) {
		t.Fatalf("speed multiplier had no effect: %.3f vs %.3f", speedOf(&fast), speedOf(&slow))
	}
}

func TestApplyHomingScaledDragDisabled(t *testing.T) {
	dragged := newKin(0, 0, 0, 0)
	free := newKin(0, 0, 0, 0)
	for range 600 {
		ApplyHomingScaled(&dragged, 500, 0, &profCruise, 1, tickDt, true)
		ApplyHomingScaled(&free, 500, 0, &profCruise, 1, tickDt, false)
		Integrate(&dragged, tickDt)
		Integrate(&free, tickDt)
	}
	if speedOf(&free) <= speedOf(&dragged) {
		t.Fatalf("applyDrag=false did not raise terminal speed: %.3f vs %.3f",
			speedOf(&free), speedOf(&dragged))
	}
}

// --- IntegrateWithBounce ---

func TestIntegrateWithBounceBoundaryRestitution(t *testing.T) {
	k := newKin(5.5, 5.5, 100, 0)
	_, _, hit := IntegrateWithBounce(&k, 0.1, 0, 0, 0, 10, 0, 10, 1, noWall)
	if !hit {
		t.Fatal("boundary collision not reported")
	}
	if vx, _ := velOf(&k); vx >= 0 {
		t.Fatalf("velocity not reflected: %v", vx)
	}
	if m := math.Abs(k.VelX); math.Abs(m-100) > 1 {
		t.Fatalf("elastic restitution changed speed: %v", m)
	}
}

func TestIntegrateWithBounceZeroRestitution(t *testing.T) {
	k := newKin(5.5, 5.5, 100, 0)
	_, _, hit := IntegrateWithBounce(&k, 0.1, 0, 0, 0, 10, 0, 10, 0, noWall)
	if !hit {
		t.Fatal("boundary collision not reported")
	}
	if k.VelX != 0 {
		t.Fatalf("zero restitution left velocity %v", k.VelX)
	}
}

func TestIntegrateWithBounceNoWallTunneling(t *testing.T) {
	wall := func(x, _ int) bool { return x == 7 }
	k := newKin(2.5, 5.5, 50, 0)

	gx, _, hit := IntegrateWithBounce(&k, 0.2, 0, 0, 0, 40, 0, 20, 1, wall)
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
	// Footprint top-left is 2 cells left of the header; the wall test receives
	// the top-left, so the header must stop 2 cells earlier.
	wall := func(x, _ int) bool { return x == 7 }
	k := newKin(4.5, 5.5, 50, 0)

	gx, _, hit := IntegrateWithBounce(&k, 0.2, 2, 1, 0, 40, 0, 20, 1, wall)
	if !hit {
		t.Fatal("offset wall collision not reported")
	}
	if gx >= 9 {
		t.Fatalf("header reached %d, footprint would overlap the wall", gx)
	}
}

func TestIntegrateWithBounceStaysInBounds(t *testing.T) {
	k := newKin(5.5, 5.5, 4000, -3000)
	gx, gy, _ := IntegrateWithBounce(&k, 0.05, 0, 0, 0, 10, 0, 10, 0.5, noWall)
	if gx < 0 || gx >= 10 || gy < 0 || gy >= 10 {
		t.Fatalf("extreme velocity escaped bounds: (%d,%d)", gx, gy)
	}
}

// --- orbital ---

func TestOrbitalVelocity(t *testing.T) {
	// v = sqrt(a*r): a = 2, r = 4 -> v = sqrt(8)
	got := OrbitalVelocity(2, 4)
	if math.Abs(got-math.Sqrt(8)) > 1e-5 {
		t.Fatalf("OrbitalVelocity = %v, want %v", got, math.Sqrt(8))
	}
}

func TestOrbitalInsertIsTangential(t *testing.T) {
	const dx, dy = 4.0, 0.0
	vx, vy := OrbitalInsert(dx, dy, 2, false)

	if d := vmath.DotProductF(dx, dy, vx, vy); math.Abs(d) > 1e-6 {
		t.Fatalf("insertion velocity not tangential (dot = %v)", d)
	}
	if m := math.Hypot(vx, vy); math.Abs(m-math.Sqrt(8)) > 1e-4 {
		t.Fatalf("insertion speed = %v, want %v", m, math.Sqrt(8))
	}

	cwX, cwY := OrbitalInsert(dx, dy, 2, true)
	if cwX != -vx || cwY != -vy {
		t.Fatal("clockwise insertion must invert the tangent")
	}

	if x, y := OrbitalInsert(0, 0, 2, false); x != 0 || y != 0 {
		t.Fatal("zero radius must yield zero velocity")
	}
}

func TestOrbitalAttractionPointsInward(t *testing.T) {
	ax, ay := OrbitalAttraction(3, 4, 10)
	if ax >= 0 || ay >= 0 {
		t.Fatalf("attraction = (%v,%v), want inward", ax, ay)
	}
	if m := math.Hypot(ax, ay); math.Abs(m-10) > 1e-4 {
		t.Fatalf("attraction magnitude = %v, want 10 (linear, not inverse square)", m)
	}
	if x, y := OrbitalAttraction(0, 0, 10); x != 0 || y != 0 {
		t.Fatal("zero offset must yield zero attraction")
	}
}

func TestOrbitalDampReducesRadialComponent(t *testing.T) {
	// Pure radial velocity, damping*dt = 0.5 -> radial halved.
	vx, vy := OrbitalDamp(1, 0, 4, 0, 1, 0.5)
	if math.Abs(vx-0.5) > 1e-6 || vy != 0 {
		t.Fatalf("damped velocity = (%v,%v), want (0.5,0)", vx, vy)
	}
}

func TestOrbitalDampPreservesTangential(t *testing.T) {
	// Velocity perpendicular to the radius has no radial component to damp.
	vx, vy := OrbitalDamp(0, 3, 4, 0, 1, 0.5)
	if math.Abs(vy-3) > 1e-5 || math.Abs(vx) > 1e-5 {
		t.Fatalf("tangential velocity altered: (%v,%v)", vx, vy)
	}
}

func TestOrbitalEquilibriumSign(t *testing.T) {
	// Outside the target radius: pull inward.
	ax, _ := OrbitalEquilibrium(10, 0, 5, 1)
	if ax >= 0 {
		t.Fatalf("outside target: ax = %v, want inward", ax)
	}
	// Inside the target radius: push outward.
	ax, _ = OrbitalEquilibrium(2, 0, 5, 1)
	if ax <= 0 {
		t.Fatalf("inside target: ax = %v, want outward", ax)
	}
	// At the target radius: no force.
	ax, _ = OrbitalEquilibrium(5, 0, 5, 1)
	if math.Abs(ax) > 0.001 {
		t.Fatalf("at target: ax = %v, want ~0", ax)
	}
}
