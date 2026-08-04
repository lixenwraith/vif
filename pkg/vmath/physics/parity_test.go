package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// posTolerance bounds accumulated Q32.32 truncation over a multi-thousand-step run
const posTolerance = 5e-3

// --- homing trajectories ---

type trace struct {
	settleTick int // -1 if never settled
	path       [][2]float64
}

func runHomingFixed(p *HomingProfile, tx, ty float64, steps int) trace {
	k := newKin(0, 0, 0, 0)
	dt := vmath.FromFloat(tickDt)
	fx, fy := vmath.FromFloat(tx), vmath.FromFloat(ty)
	tr := trace{settleTick: -1, path: make([][2]float64, 0, steps)}
	for i := range steps {
		if ApplyHoming(&k, fx, fy, p, dt) {
			tr.settleTick = i
			return tr
		}
		Integrate(&k, dt)
		x, y := posOf(&k)
		tr.path = append(tr.path, [2]float64{x, y})
	}
	return tr
}

func runHomingFloat(p *HomingProfileF, tx, ty float64, steps int) trace {
	var k KineticF
	tr := trace{settleTick: -1, path: make([][2]float64, 0, steps)}
	for i := range steps {
		if ApplyHomingF(&k, tx, ty, p, tickDt) {
			tr.settleTick = i
			return tr
		}
		IntegrateF(&k, tickDt)
		tr.path = append(tr.path, [2]float64{k.PreciseX, k.PreciseY})
	}
	return tr
}

func comparePaths(t *testing.T, name string, a, b trace) {
	t.Helper()
	if d := a.settleTick - b.settleTick; d < -1 || d > 1 {
		t.Fatalf("%s: settle tick %d (fixed) vs %d (float)", name, a.settleTick, b.settleTick)
	}
	n := min(len(a.path), len(b.path))
	worst, worstAt := 0.0, 0
	for i := range n {
		d := math.Hypot(a.path[i][0]-b.path[i][0], a.path[i][1]-b.path[i][1])
		if d > worst {
			worst, worstAt = d, i
		}
	}
	if worst > posTolerance {
		t.Fatalf("%s: paths diverged %.6f cells at step %d", name, worst, worstAt)
	}
	t.Logf("%s: settle %d/%d, max divergence %.2e cells over %d steps",
		name, a.settleTick, b.settleTick, worst, n)
}

func TestApplyHomingTrajectoryParity(t *testing.T) {
	cases := []struct {
		name   string
		fixed  *HomingProfile
		float_ *HomingProfileF
		tx, ty float64
	}{
		{"braked/axis", &profBraked, &profBrakedF, 20, 0},
		{"braked/diag", &profBraked, &profBrakedF, -17.5, 9.25},
		{"cruise/axis", &profCruise, &profCruiseF, 20, 0},
		{"cruise/far", &profCruise, &profCruiseF, 120, -60},
	}
	for _, c := range cases {
		comparePaths(t, c.name,
			runHomingFixed(c.fixed, c.tx, c.ty, 3000),
			runHomingFloat(c.float_, c.tx, c.ty, 3000))
	}
}

func TestApplyHomingScaledTrajectoryParity(t *testing.T) {
	dtF := vmath.FromFloat(tickDt)
	k := newKin(0, 0, 0, 0)
	var kf KineticF

	for range 600 {
		ApplyHomingScaled(&k, vmath.FromFloat(500), 0, &profCruise, vmath.Scale*2, dtF, false)
		Integrate(&k, dtF)
		ApplyHomingScaledF(&kf, 500, 0, &profCruiseF, 2.0, tickDt, false)
		IntegrateF(&kf, tickDt)
	}
	x, _ := posOf(&k)
	if math.Abs(x-kf.PreciseX) > posTolerance {
		t.Fatalf("drag-disabled multiplier diverged: %v vs %v", x, kf.PreciseX)
	}
}

// --- bounce trajectories ---

// Fixed truncates dtStep and Mul toward zero, so its sub-step positions run
// ~1e-7 cells behind the float path. A sub-step that lands exactly on a cell
// boundary therefore crosses one sub-step later in fixed, and the discrete
// wall/bounds test amplifies that into a half-cell trajectory split.
// Constants below are chosen so every sub-step position is an odd multiple of
// 1/170 cells, i.e. never closer than 1/170 (~0.006) to a boundary:
// with dt=0.2 and |Vel|=37 the step count pins at 17 under restitution 1.0,
// and 10*(pos*17) stays odd across wall reverts and boundary clamps alike.
const alignmentHint = "\n(if this appeared after a constant change: a sub-step " +
	"landed on a cell boundary — see the note above this test)"

func TestIntegrateWithBounceWallParity(t *testing.T) {
	wall := func(x, _ int) bool { return x == 7 }
	const dt = 0.2
	dtF := vmath.FromFloat(dt)

	k := newKin(2.5, 5.5, 37, 3)
	kf := KineticF{PreciseX: 2.5, PreciseY: 5.5, VelX: 37, VelY: 3}

	for i := range 12 {
		gx, gy, hit := IntegrateWithBounce(&k, dtF, 0, 0, 0, 40, 0, 20, vmath.Scale, wall)
		fx, fy, fhit := IntegrateWithBounceF(&kf, dt, 0, 0, 0, 40, 0, 20, 1.0, wall)

		if gx != fx || gy != fy || hit != fhit {
			t.Fatalf("step %d: fixed (%d,%d,%v) vs float (%d,%d,%v)%s",
				i, gx, gy, hit, fx, fy, fhit, alignmentHint)
		}
		px, py := posOf(&k)
		if math.Abs(px-kf.PreciseX) > posTolerance || math.Abs(py-kf.PreciseY) > posTolerance {
			t.Fatalf("step %d: precise (%v,%v) vs (%v,%v)", i, px, py, kf.PreciseX, kf.PreciseY)
		}
	}
}

func TestIntegrateWithBounceBoundaryParity(t *testing.T) {
	const dt = 0.2
	dtF := vmath.FromFloat(dt)

	k := newKin(2.5, 5.5, 37, 23)
	kf := KineticF{PreciseX: 2.5, PreciseY: 5.5, VelX: 37, VelY: 23}

	for i := range 10 {
		gx, gy, hit := IntegrateWithBounce(&k, dtF, 0, 0, 0, 10, 0, 10,
			vmath.FromFloat(0.5), noWall)
		fx, fy, fhit := IntegrateWithBounceF(&kf, dt, 0, 0, 0, 10, 0, 10, 0.5, noWall)

		if gx != fx || gy != fy || hit != fhit {
			t.Fatalf("step %d: fixed (%d,%d,%v) vs float (%d,%d,%v)%s",
				i, gx, gy, hit, fx, fy, fhit, alignmentHint)
		}
		vx, vy := velOf(&k)
		if math.Abs(vx-kf.VelX) > posTolerance || math.Abs(vy-kf.VelY) > posTolerance {
			t.Fatalf("step %d: velocity (%v,%v) vs (%v,%v)", i, vx, vy, kf.VelX, kf.VelY)
		}
	}
}

// --- single-call parity ---

func TestKineticFParity(t *testing.T) {
	k := newKin(3.25, -1.5, 10, -4)
	k.AccelX, k.AccelY = vmath.FromFloat(2), vmath.FromFloat(-1)
	kf := KineticF{PreciseX: 3.25, PreciseY: -1.5, VelX: 10, VelY: -4, AccelX: 2, AccelY: -1}

	gx, gy := Integrate(&k, vmath.FromFloat(0.5))
	fx, fy := IntegrateF(&kf, 0.5)
	if gx != fx || gy != fy {
		t.Fatalf("grid (%d,%d) vs (%d,%d)", gx, gy, fx, fy)
	}
	px, py := posOf(&k)
	if math.Abs(px-kf.PreciseX) > 1e-6 || math.Abs(py-kf.PreciseY) > 1e-6 {
		t.Fatalf("precise (%v,%v) vs (%v,%v)", px, py, kf.PreciseX, kf.PreciseY)
	}

	// SetGridPos must land on the cell center on both paths
	SetGridPos(&k, 7, -3)
	SetGridPosF(&kf, 7, -3)
	px, py = posOf(&k)
	if px != kf.PreciseX || py != kf.PreciseY {
		t.Fatalf("SetGridPos (%v,%v) vs (%v,%v)", px, py, kf.PreciseX, kf.PreciseY)
	}
	if x, y := GridPosF(&kf); x != 7 || y != -3 {
		t.Fatalf("GridPosF = (%d,%d), want (7,-3)", x, y)
	}
}

func TestReflectBoundsFParity(t *testing.T) {
	cases := []struct{ px, vx float64 }{{-0.5, -3}, {10.2, 3}, {5.5, 1}, {-0.001, -1}}
	for _, c := range cases {
		k := kin{PreciseX: vmath.FromFloat(c.px), VelX: vmath.FromFloat(c.vx)}
		kf := KineticF{PreciseX: c.px, VelX: c.vx}
		if ReflectBoundsX(&k, 0, 10) != ReflectBoundsXF(&kf, 0, 10) {
			t.Fatalf("px=%v: reflection disagreement", c.px)
		}
		x, _ := posOf(&k)
		if math.Abs(x-kf.PreciseX) > 1e-9 {
			t.Fatalf("px=%v: clamped to %v vs %v", c.px, x, kf.PreciseX)
		}
	}
}

func TestCapSpeedFParity(t *testing.T) {
	for _, c := range [][3]float64{{1, 2, 5}, {30, 40, 5}, {0, 0, 5}, {-9, 12, 5}} {
		gx, gy := CapSpeed(vmath.FromFloat(c[0]), vmath.FromFloat(c[1]), vmath.FromFloat(c[2]))
		fx, fy := CapSpeedF(c[0], c[1], c[2])
		if math.Abs(vmath.ToFloat(gx)-fx) > 1e-6 || math.Abs(vmath.ToFloat(gy)-fy) > 1e-6 {
			t.Fatalf("CapSpeed(%v) = (%v,%v) vs (%v,%v)", c, vmath.ToFloat(gx), vmath.ToFloat(gy), fx, fy)
		}
	}
}

func TestCollisionImpulseFParity(t *testing.T) {
	// nil rng: deterministic on both paths, so values must match directly
	dirs := [][2]float64{{1, 0}, {0, -1}, {3, 4}, {-2.5, 1.25}, {0, 0}}
	for _, d := range dirs {
		gx, gy := ApplyCollisionImpulse(vmath.FromFloat(d[0]), vmath.FromFloat(d[1]),
			vmath.Scale/2, 0, vmath.FromFloat(10), vmath.FromFloat(10), nil)
		fx, fy := ApplyCollisionImpulseF(d[0], d[1], 0.5, 0, 10, 10, nil)
		if math.Abs(vmath.ToFloat(gx)-fx) > 1e-5 || math.Abs(vmath.ToFloat(gy)-fy) > 1e-5 {
			t.Fatalf("dir %v: (%v,%v) vs (%v,%v)", d, vmath.ToFloat(gx), vmath.ToFloat(gy), fx, fy)
		}
	}

	// offset blend
	gx, gy := ApplyOffsetCollisionImpulse(vmath.FromFloat(1), 0, 2, -1,
		vmath.Scale/2, vmath.Scale, 0, vmath.FromFloat(10), vmath.FromFloat(10), nil)
	fx, fy := ApplyOffsetCollisionImpulseF(1, 0, 2, -1, 0.5, 1.0, 0, 10, 10, nil)
	if math.Abs(vmath.ToFloat(gx)-fx) > 1e-5 || math.Abs(vmath.ToFloat(gy)-fy) > 1e-5 {
		t.Fatalf("offset impulse: (%v,%v) vs (%v,%v)", vmath.ToFloat(gx), vmath.ToFloat(gy), fx, fy)
	}
}

func TestApplyCollisionFModes(t *testing.T) {
	additive := profImpulseF
	kf := KineticF{VelX: 3}
	ApplyCollisionF(&kf, 1, 0, &additive, nil)
	if math.Abs(kf.VelX-13) > 1e-9 {
		t.Fatalf("additive = %v, want 13", kf.VelX)
	}

	override := profImpulseF
	override.Mode = ImpulseOverride
	kf = KineticF{VelX: 3}
	ApplyCollisionF(&kf, 1, 0, &override, nil)
	if math.Abs(kf.VelX-10) > 1e-9 {
		t.Fatalf("override = %v, want 10", kf.VelX)
	}

	// zero direction falls back to +X on both paths
	kf = KineticF{}
	ApplyCollisionF(&kf, 0, 0, &profImpulseF, nil)
	if math.Abs(kf.VelX-10) > 1e-9 || kf.VelY != 0 {
		t.Fatalf("fallback = (%v,%v), want (10,0)", kf.VelX, kf.VelY)
	}
}

func TestCheckSoftCollisionFParity(t *testing.T) {
	invRx, invRy := vmath.EllipseInvRadiiSq(vmath.FromFloat(3), vmath.FromFloat(1.5))
	fInvRx, fInvRy := vmath.EllipseInvRadiiSqF(3, 1.5)
	for dy := -3; dy <= 3; dy++ {
		for dx := -5; dx <= 5; dx++ {
			_, _, a := CheckSoftCollision(10+dx, 10+dy, 10, 10, invRx, invRy)
			rx, ry, b := CheckSoftCollisionF(10+dx, 10+dy, 10, 10, fInvRx, fInvRy)
			if a != b {
				t.Fatalf("offset (%d,%d): fixed %v, float %v", dx, dy, a, b)
			}
			if b && dx == 0 && dy == 0 && rx == 0 && ry == 0 {
				t.Fatal("co-located collision must produce a fallback direction")
			}
		}
	}
}

// --- orbital ---

func TestOrbitalFParity(t *testing.T) {
	check := func(name string, gx, gy int64, fx, fy float64) {
		t.Helper()
		if math.Abs(vmath.ToFloat(gx)-fx) > 1e-5 || math.Abs(vmath.ToFloat(gy)-fy) > 1e-5 {
			t.Fatalf("%s: (%v,%v) vs (%v,%v)", name, vmath.ToFloat(gx), vmath.ToFloat(gy), fx, fy)
		}
	}

	gx, gy := OrbitalInsert(vmath.FromFloat(4), vmath.FromFloat(-3), vmath.FromFloat(2), false)
	fx, fy := OrbitalInsertF(4, -3, 2, false)
	check("OrbitalInsert", gx, gy, fx, fy)

	gx, gy = OrbitalAttraction(vmath.FromFloat(3), vmath.FromFloat(4), vmath.FromFloat(10))
	fx, fy = OrbitalAttractionF(3, 4, 10)
	check("OrbitalAttraction", gx, gy, fx, fy)

	gx, gy = OrbitalDamp(vmath.FromFloat(1), vmath.FromFloat(2), vmath.FromFloat(4), 0,
		vmath.FromFloat(1.0), vmath.FromFloat(0.5))
	fx, fy = OrbitalDampF(1, 2, 4, 0, 1.0, 0.5)
	check("OrbitalDamp", gx, gy, fx, fy)

	for _, d := range []float64{10, 5, 2, 0} {
		gx, gy = OrbitalEquilibrium(vmath.FromFloat(d), 0, vmath.FromFloat(5), vmath.Scale)
		fx, fy = OrbitalEquilibriumF(d, 0, 5, 1.0)
		check("OrbitalEquilibrium", gx, gy, fx, fy)
	}
}

// --- 3D ---

func TestGravitational3DFParity(t *testing.T) {
	a, b := v3(0, 0, 0), v3(4, -2, 1)
	fixed := GravitationalAccel3D(a, b, vmath.Scale*10, vmath.Scale)
	flt := GravitationalAccel3DF(v3f(a), v3f(b), 10, 1)
	if math.Abs(vmath.ToFloat(fixed.X)-flt.X) > 1e-4 ||
		math.Abs(vmath.ToFloat(fixed.Y)-flt.Y) > 1e-4 ||
		math.Abs(vmath.ToFloat(fixed.Z)-flt.Z) > 1e-4 {
		t.Fatalf("gravity %v vs %v", vmath.V3ToFloat(fixed), flt)
	}

	for _, dx := range []float64{1, 3, 8} {
		fixed = GravitationalAccelWithRepulsion3D(a, v3(dx, 0, 0),
			vmath.Scale*10, vmath.Scale, vmath.FromFloat(3), vmath.FromFloat(5))
		flt = GravitationalAccelWithRepulsion3DF(v3f(a), vmath.Vec3F{X: dx}, 10, 1, 3, 5)
		if math.Abs(vmath.ToFloat(fixed.X)-flt.X) > 1e-4 {
			t.Fatalf("dx=%v: repulsion %v vs %v", dx, vmath.ToFloat(fixed.X), flt.X)
		}
	}
}

func TestReflectAxis3DFParity(t *testing.T) {
	for _, c := range [][2]float64{{-5, -3}, {15, 3}, {5, 3}} {
		p, v := vmath.FromFloat(c[0]), vmath.FromFloat(c[1])
		pf, vf := c[0], c[1]
		a := ReflectAxis3D(&p, &v, 0, vmath.FromFloat(10), vmath.Scale/2)
		b := ReflectAxis3DF(&pf, &vf, 0, 10, 0.5)
		if a != b || math.Abs(vmath.ToFloat(p)-pf) > 1e-9 || math.Abs(vmath.ToFloat(v)-vf) > 1e-9 {
			t.Fatalf("case %v: (%v,%v,%v) vs (%v,%v,%v)", c, vmath.ToFloat(p), vmath.ToFloat(v), a, pf, vf, b)
		}
	}
}

func TestSteeringHelperParity(t *testing.T) {
	dtF := vmath.FromFloat(tickDt)

	k := newKin(2.5, -3.25, 9, -4)
	kf := KineticF{PreciseX: 2.5, PreciseY: -3.25, VelX: 9, VelY: -4}

	for range 200 {
		ApplyQuadraticDrag(&k, vmath.FromFloat(0.02), dtF)
		ApplyQuadraticDragF(&kf, 0.02, tickDt)
		ApplyLinearDrag(&k, vmath.FromFloat(1.5), dtF)
		ApplyLinearDragF(&kf, 1.5, tickDt)
		gx, gy := IntegratePosition(&k, dtF)
		fx, fy := IntegratePositionF(&kf, tickDt)
		if gx != fx || gy != fy {
			t.Fatalf("grid (%d,%d) vs (%d,%d)", gx, gy, fx, fy)
		}
	}
	px, py := posOf(&k)
	if math.Abs(px-kf.PreciseX) > posTolerance || math.Abs(py-kf.PreciseY) > posTolerance {
		t.Fatalf("precise (%v,%v) vs (%v,%v)", px, py, kf.PreciseX, kf.PreciseY)
	}

	for _, tgt := range [][2]float64{{20, 0}, {-5, -5}, {2.5, -3.25}} {
		a := vmath.ToFloat(TurnSeverity(&k, vmath.FromFloat(tgt[0]), vmath.FromFloat(tgt[1]),
			vmath.FromFloat(3.0), vmath.Scale))
		b := TurnSeverityF(&kf, tgt[0], tgt[1], 3.0, 1.0)
		if math.Abs(a-b) > 1e-5 {
			t.Fatalf("TurnSeverity toward %v: %v vs %v", tgt, a, b)
		}
	}

	sk := newKin(1, 1, 0, 0)
	skf := KineticF{PreciseX: 1, PreciseY: 1}
	for range 100 {
		SpringToRest(&sk, vmath.FromFloat(4), vmath.FromFloat(-2),
			vmath.FromFloat(18), vmath.FromFloat(0.82), vmath.FromFloat(40), dtF)
		Integrate(&sk, dtF)
		SpringToRestF(&skf, 4, -2, 18, 0.82, 40, tickDt)
		IntegrateF(&skf, tickDt)
	}
	px, py = posOf(&sk)
	if math.Abs(px-skf.PreciseX) > posTolerance || math.Abs(py-skf.PreciseY) > posTolerance {
		t.Fatalf("spring (%v,%v) vs (%v,%v)", px, py, skf.PreciseX, skf.PreciseY)
	}
}
