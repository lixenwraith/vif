package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

func v3(x, y, z float64) vmath.Vec3 {
	return vmath.Vec3{X: x, Y: y, Z: z}
}

func TestElasticCollision3DEqualMassSwap(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)
	velA, velB := v3(5, 0, 0), v3(0, 0, 0)

	if !ElasticCollision3D(&posA, &posB, &velA, &velB, 1, 1, 1) {
		t.Fatal("approaching bodies must collide")
	}
	if math.Abs(velA.X) > 1e-4 {
		t.Fatalf("A retained velocity %v, want ~0", velA.X)
	}
	if math.Abs(velB.X-5) > 1e-4 {
		t.Fatalf("B velocity %v, want 5", velB.X)
	}
}

func TestElasticCollision3DConservesMomentum(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 2, 0)
	velA, velB := v3(4, 1, -2), v3(-1, 0, 1)
	const massA, massB = 2.0, 5.0

	pBefore := vmath.V3Add(vmath.V3Scale(velA, massA), vmath.V3Scale(velB, massB))
	if !ElasticCollision3D(&posA, &posB, &velA, &velB, massA, massB, 1) {
		t.Skip("configuration is separating")
	}
	pAfter := vmath.V3Add(vmath.V3Scale(velA, massA), vmath.V3Scale(velB, massB))

	for _, d := range []float64{pAfter.X - pBefore.X, pAfter.Y - pBefore.Y, pAfter.Z - pBefore.Z} {
		if math.Abs(d) > 1e-9 {
			t.Fatalf("momentum drift %v", d)
		}
	}
}

func TestElasticCollision3DSeparatingIsNoop(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)
	velA, velB := v3(-5, 0, 0), v3(0, 0, 0)
	wantA, wantB := velA, velB
	if ElasticCollision3D(&posA, &posB, &velA, &velB, 1, 1, 1) {
		t.Fatal("separating bodies must not collide")
	}
	if velA != wantA || velB != wantB {
		t.Fatal("no-op collision altered velocities")
	}
}

func TestElasticCollision3DCoincidentIsNoop(t *testing.T) {
	p := v3(1, 1, 1)
	velA, velB := v3(1, 0, 0), v3(-1, 0, 0)
	if ElasticCollision3D(&p, &p, &velA, &velB, 1, 1, 1) {
		t.Fatal("coincident bodies must not resolve")
	}
}

func TestElasticCollision3DRejectsNonPositiveMass(t *testing.T) {
	for _, masses := range [][2]float64{{0, 1}, {1, 0}, {-1, 1}} {
		posA, posB := v3(0, 0, 0), v3(1, 0, 0)
		velA, velB := v3(1, 0, 0), v3(0, 0, 0)
		if ElasticCollision3D(&posA, &posB, &velA, &velB, masses[0], masses[1], 1) {
			t.Fatalf("masses %v unexpectedly resolved", masses)
		}
		if velA != v3(1, 0, 0) || velB != v3(0, 0, 0) {
			t.Fatalf("masses %v changed velocity", masses)
		}
	}
}

func TestSeparateOverlap3D(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)

	if !SeparateOverlap3D(&posA, &posB, 1, 1, 1, 1) {
		t.Fatal("overlapping spheres must separate")
	}
	d := vmath.V3Mag(vmath.V3Sub(posB, posA))
	if d < 2.0 {
		t.Fatalf("separation %v, want >= 2", d)
	}
	// Equal masses split the correction evenly.
	if math.Abs(posA.X+posB.X-1) > 1e-9 {
		t.Fatal("equal masses did not split the correction evenly")
	}

	posA, posB = v3(0, 0, 0), v3(5, 0, 0)
	if SeparateOverlap3D(&posA, &posB, 1, 1, 1, 1) {
		t.Fatal("non-overlapping spheres must not separate")
	}
}

func TestSeparateOverlap3DRejectsNonPositiveMass(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)
	wantA, wantB := posA, posB
	if SeparateOverlap3D(&posA, &posB, 1, 1, 0, 1) {
		t.Fatal("zero-mass overlap unexpectedly separated")
	}
	if posA != wantA || posB != wantB {
		t.Fatal("zero-mass overlap changed positions")
	}
}

func TestGravitationalAccel3DDirectionAndFalloff(t *testing.T) {
	a := GravitationalAccel3D(v3(0, 0, 0), v3(4, 0, 0), 10, 1)
	if a.X <= 0 {
		t.Fatalf("acceleration = %v, want toward B", a.X)
	}
	near := GravitationalAccel3D(v3(0, 0, 0), v3(2, 0, 0), 10, 1)
	if near.X <= a.X {
		t.Fatal("gravity did not increase with proximity")
	}
}

func TestGravitationalAccelWithRepulsion3D(t *testing.T) {
	const repulsionR = 3.0

	// Inside the repulsion radius: pushed away from B.
	in := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(1, 0, 0), 10, 1, repulsionR, 5)
	if in.X >= 0 {
		t.Fatalf("inside repulsion: ax = %v, want negative", in.X)
	}

	// Outside: attracted toward B.
	out := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(8, 0, 0), 10, 1, repulsionR, 5)
	if out.X <= 0 {
		t.Fatalf("outside repulsion: ax = %v, want positive", out.X)
	}

	if z := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(0, 0, 0), 1, 1, repulsionR, 5); z != (vmath.Vec3{}) {
		t.Fatal("coincident bodies must yield zero acceleration")
	}
}

func TestReflectAxis3D(t *testing.T) {
	pos, vel := -5.0, -3.0
	if !ReflectAxis3D(&pos, &vel, 0, 10, 0.5) {
		t.Fatal("below-low reflection not reported")
	}
	if pos != 0 || vel <= 0 {
		t.Fatalf("pos = %v, vel = %v after low reflection", pos, vel)
	}

	pos, vel = 15, 3
	if !ReflectAxis3D(&pos, &vel, 0, 10, 0.5) {
		t.Fatal("above-high reflection not reported")
	}
	if pos != 10 || vel >= 0 {
		t.Fatalf("pos = %v, vel = %v after high reflection", pos, vel)
	}

	pos, vel = 5, 3
	if ReflectAxis3D(&pos, &vel, 0, 10, 1) {
		t.Fatal("in-range value reported a reflection")
	}
}

func TestV3NormalizeUnit(t *testing.T) {
	n := vmath.V3Normalize(v3(3, 4, 12))
	if m := vmath.V3Mag(n); math.Abs(m-1) > 1e-9 {
		t.Fatalf("normalized magnitude = %v", m)
	}
	if vmath.V3Normalize(vmath.Vec3{}) != (vmath.Vec3{}) {
		t.Fatal("zero vector must normalize to zero")
	}
}

func TestV3ClampMagnitude(t *testing.T) {
	v := v3(30, 40, 0)
	c := vmath.V3ClampMagnitude(v, 5)
	if m := vmath.V3Mag(c); math.Abs(m-5) > 1e-9 {
		t.Fatalf("clamped magnitude = %v, want 5", m)
	}
	small := v3(1, 1, 1)
	if vmath.V3ClampMagnitude(small, 100) != small {
		t.Fatal("under-limit vector was modified")
	}
}

func TestV3DampDtBounds(t *testing.T) {
	v := v3(10, 0, 0)
	// dt large enough to over-decay must clamp at zero, not invert.
	d := vmath.V3DampDt(v, 0, 10)
	if d.X != 0 {
		t.Fatalf("over-decay produced %v, want 0", d.X)
	}
	// No decay.
	if n := vmath.V3DampDt(v, 1, 1); n != v {
		t.Fatal("factor=1 must not change the vector")
	}
}

// near reports two values equal to a part in 1e9, which leaves a twin its own
// order of operations
func near(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func nearV3(a, b vmath.Vec3) bool { return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z) }

// turn cycles the axes, x to y, y to z and z to x: a rotation, so handedness holds
func turn(v vmath.Vec3) vmath.Vec3 { return vmath.Vec3{X: v.Z, Y: v.X, Z: v.Y} }

func turnPoint(p vmath.Point3) vmath.Point3 { return vmath.Point3{X: p.Z, Y: p.X, Z: p.Y} }

func turnKinetic(k Kinetic3D) Kinetic3D {
	return Kinetic3D{Pos: turn(k.Pos), Vel: turn(k.Vel), Accel: turn(k.Accel)}
}

func nearKinetic(a, b Kinetic3D) bool {
	return nearV3(a.Pos, b.Pos) && nearV3(a.Vel, b.Vel) && nearV3(a.Accel, b.Accel)
}

// deep lifts a flat body into the plane z = 0 of a volume
func deep(k Kinetic) Kinetic3D {
	return Kinetic3D{Pos: v3(k.PreciseX, k.PreciseY, 0), Vel: v3(k.VelX, k.VelY, 0), Accel: v3(k.AccelX, k.AccelY, 0)}
}

// TestABodyInOnePlaneOfAVolumeIsTheFlatBody: every 3D form given no depth steers,
// drags, reflects and moves a body as its flat form does, and leaves the plane
// to none of them.
func TestABodyInOnePlaneOfAVolumeIsTheFlatBody(t *testing.T) {
	rng := vmath.NewFastRand(0x3d)
	rnd := func(span float64) float64 { return (rng.Float64()*2 - 1) * span }
	bounds := vmath.Box{Width: 40, Height: 30, Depth: 1}
	strike := CollisionProfile{MassRatio: 0.7, ImpulseMin: 15, ImpulseMax: 40, OffsetInfluence: 0.6}
	for round := range 3000 {
		flat := Kinetic{PreciseX: 20 + rnd(25), PreciseY: 15 + rnd(20), VelX: rnd(30), VelY: rnd(30)}
		if round%3 == 0 {
			flat.AccelX, flat.AccelY = rnd(10), rnd(10)
		}
		vol := deep(flat)
		tx, ty := 20+rnd(20), 15+rnd(15)
		target := v3(tx, ty, 0)
		profile := &profCruise
		if round%2 == 0 {
			profile = &profBraked
		}
		for step := range 30 {
			const dt = 0.016
			if a, b := ApplyHomingScaled(&flat, tx, ty, profile, 1.3, dt, step%4 != 0),
				ApplyHomingScaled3D(&vol, target, profile, 1.3, dt, step%4 != 0); a != b {
				t.Fatalf("round %d step %d: settled %t flat, %t in the volume", round, step, a, b)
			}
			if a, b := TurnSeverity(&flat, tx, ty, 3, 0.5), TurnSeverity3D(&vol, target, 3, 0.5); !near(a, b) {
				t.Fatalf("round %d step %d: turn severity %v flat, %v in the volume", round, step, a, b)
			}
			switch step % 5 {
			case 0:
				ApplyLinearDrag(&flat, 0.8, dt)
				ApplyLinearDrag3D(&vol, 0.8, dt)
			case 1:
				ApplyQuadraticDrag(&flat, 0.02, dt)
				ApplyQuadraticDrag3D(&vol, 0.02, dt)
			case 2:
				SpringToRest(&flat, tx, ty, 40, 0.98, 30, dt)
				SpringToRest3D(&vol, target, 40, 0.98, 30, dt)
			case 3:
				x, y := IntegratePosition(&flat, dt)
				if p := IntegratePosition3D(&vol, dt); p != (vmath.Point3{X: x, Y: y}) {
					t.Fatalf("round %d step %d: cell (%d,%d) flat, %v in the volume", round, step, x, y, p)
				}
			case 4:
				// a spread turns out of the plane by design, so these strike with none
				dx, dy, ox, oy := rnd(5), rnd(5), rng.Intn(5)-2, rng.Intn(5)-2
				a, b := vmath.NewFastRand(uint64(round)), vmath.NewFastRand(uint64(round))
				ApplyCollision(&flat, dx, dy, &strike, a)
				ApplyCollision3D(&vol, v3(dx, dy, 0), &strike, b)
				ApplyOffsetCollision(&flat, dx, dy, ox, oy, &strike, a)
				ApplyOffsetCollision3D(&vol, v3(dx, dy, 0), vmath.Point3{X: ox, Y: oy}, &strike, b)
			}
			x, y := Integrate(&flat, dt)
			if p := Integrate3D(&vol, dt); p != (vmath.Point3{X: x, Y: y}) {
				t.Fatalf("round %d step %d: cell (%d,%d) flat, %v in the volume", round, step, x, y, p)
			}
			rx, ry := ReflectBoundsDampedX(&flat, 0, 39, 0.7), ReflectBoundsDampedY(&flat, 0, 29, 0.7)
			if got := ReflectBounds3D(&vol, bounds, 0.7); got != (rx || ry) {
				t.Fatalf("round %d step %d: reflected %t flat, %t in the volume", round, step, rx || ry, got)
			}
			if !nearKinetic(vol, deep(flat)) || vol.Pos.Z != 0 || vol.Vel.Z != 0 {
				t.Fatalf("round %d step %d: the volume's body %+v parted from the flat %+v", round, step, vol, flat)
			}
		}
	}
}

// TestAVolumesBodyTreatsItsAxesAlike: a 3D form answers a state with its axes
// cycled as it answers the state, cycled, so depth is steered, dragged, reflected
// and struck as a row or a column is.
func TestAVolumesBodyTreatsItsAxesAlike(t *testing.T) {
	rng := vmath.NewFastRand(0x7c)
	vec := func(span float64) vmath.Vec3 {
		return v3((rng.Float64()*2-1)*span, (rng.Float64()*2-1)*span, (rng.Float64()*2-1)*span)
	}
	bounds := vmath.Box{X: -12, Y: -9, Z: -6, Width: 24, Height: 18, Depth: 12}
	turned := vmath.Box{X: bounds.Z, Y: bounds.X, Z: bounds.Y, Width: bounds.Depth, Height: bounds.Width, Depth: bounds.Height}
	profile := CollisionProfile{MassRatio: 0.7, ImpulseMin: 15, ImpulseMax: 40, OffsetInfluence: 0.6}
	for round := range 3000 {
		k := Kinetic3D{Pos: vec(15), Vel: vec(30), Accel: vec(8)}
		target, d, axis := vec(15), vec(12), vmath.V3Normalize(vec(1))
		offset := vmath.Point3{X: rng.Intn(7) - 3, Y: rng.Intn(7) - 3, Z: rng.Intn(7) - 3}
		const dt = 0.016
		// each runs on the state and on the state turned, with one seed for both
		for name, op := range map[string]func(k *Kinetic3D, face func(vmath.Vec3) vmath.Vec3, rng *vmath.FastRand) vmath.Vec3{
			"homing": func(k *Kinetic3D, face func(vmath.Vec3) vmath.Vec3, _ *vmath.FastRand) vmath.Vec3 {
				ApplyHomingScaled3D(k, face(target), &profCruise, 1.2, dt, true)
				return v3(TurnSeverity3D(k, face(target), 3, 0.5), 0, 0)
			},
			"drag": func(k *Kinetic3D, _ func(vmath.Vec3) vmath.Vec3, _ *vmath.FastRand) vmath.Vec3 {
				ApplyLinearDrag3D(k, 0.8, dt)
				ApplyQuadraticDrag3D(k, 0.02, dt)
				return vmath.Vec3{}
			},
			"spring": func(k *Kinetic3D, face func(vmath.Vec3) vmath.Vec3, _ *vmath.FastRand) vmath.Vec3 {
				SpringToRest3D(k, face(target), 40, 0.98, 30, dt)
				return vmath.Vec3{}
			},
			"collision": func(k *Kinetic3D, face func(vmath.Vec3) vmath.Vec3, rng *vmath.FastRand) vmath.Vec3 {
				ApplyCollision3D(k, face(d), &profile, rng)
				at := offset
				if face(v3(1, 0, 0)).X == 0 {
					at = turnPoint(at)
				}
				ApplyOffsetCollision3D(k, face(d), at, &profile, rng)
				return vmath.Vec3{}
			},
			"orbit": func(k *Kinetic3D, face func(vmath.Vec3) vmath.Vec3, _ *vmath.FastRand) vmath.Vec3 {
				k.Vel = OrbitalDamp3D(vmath.V3Add(k.Vel, OrbitalInsert3D(face(d), face(axis), 12)), face(d), 0.5, dt)
				k.Accel = vmath.V3Add(OrbitalAttraction3D(face(d), 12), OrbitalEquilibrium3D(face(d), 7, 3))
				return vmath.Vec3{}
			},
		} {
			plain, cycled := k, turnKinetic(k)
			a := op(&plain, func(v vmath.Vec3) vmath.Vec3 { return v }, vmath.NewFastRand(uint64(round)))
			b := op(&cycled, turn, vmath.NewFastRand(uint64(round)))
			if !nearKinetic(cycled, turnKinetic(plain)) || !nearV3(a, b) {
				t.Fatalf("round %d %s: the turned state gave %+v (%v), the state turned %+v (%v)", round, name, cycled, b, turnKinetic(plain), a)
			}
		}
		plain, cycled := k, turnKinetic(k)
		p, q := Integrate3D(&plain, dt), Integrate3D(&cycled, dt)
		r, s := ReflectBounds3D(&plain, bounds, 0.7), ReflectBounds3D(&cycled, turned, 0.7)
		if q != turnPoint(p) || r != s || !nearKinetic(cycled, turnKinetic(plain)) {
			t.Fatalf("round %d: a step and its bounds gave %+v on %v turned, %+v on %v", round, cycled, q, plain, p)
		}
		inv := vmath.EllipsoidInvRadiiSq(6, 4, 3)
		radial, hit := CheckSoftCollision3D(offset, vmath.Point3{}, inv)
		if rt, ht := CheckSoftCollision3D(turnPoint(offset), vmath.Point3{}, turn(inv)); ht != hit || rt != turn(radial) && offset != (vmath.Point3{}) {
			t.Fatalf("round %d: a contact at %v gave %v %t, turned %v %t", round, offset, radial, hit, rt, ht)
		}
	}
}

// TestIntegrateWithBounce3DSweepsEachAxis: in one plane the volume's integrator
// is the flat one among walls, from inside the bounds or out, each returning its
// body's cell; along depth a body stops short of a wall under its header,
// reflects off the box's last layer and keeps inside it.
func TestIntegrateWithBounce3DSweepsEachAxis(t *testing.T) {
	rng := vmath.NewFastRand(0xb0)
	wall := func(x, y int) bool { return x&7 == 0 && y%5 == 0 }
	for round := range 20000 {
		hx, hy := rng.Intn(3), rng.Intn(2)
		flat := Kinetic{
			PreciseX: rng.Float64()*210 - 5, PreciseY: rng.Float64()*70 - 5,
			VelX: rng.Float64()*400 - 200, VelY: rng.Float64()*400 - 200,
			AccelX: rng.Float64()*100 - 50, AccelY: rng.Float64()*100 - 50,
		}
		x, y := GridPos(&flat)
		if wall(x-hx, y-hy) {
			continue
		}
		inside := x >= 0 && x < 200 && y >= 0 && y < 60
		vol := deep(flat)
		restitution, dt := rng.Float64(), 0.001+rng.Float64()*0.05
		for step := range 5 {
			x, y, stats := IntegrateWithBounceStats(&flat, dt, hx, hy, 0, 199, 0, 59, restitution, wall)
			p, deepStats := IntegrateWithBounce3D(&vol, dt, vmath.Point3{X: hx, Y: hy}, vmath.Box{Width: 200, Height: 60, Depth: 1},
				restitution, func(c vmath.Point3) bool { return wall(c.X, c.Y) })
			// a bound can set a body from outside it down in a wall, which the
			// third axis then tests a third time a step
			if p != (vmath.Point3{X: x, Y: y}) || deepStats != stats && inside || !nearKinetic(vol, deep(flat)) {
				t.Fatalf("round %d step %d: flat %+v at (%d,%d) %+v, in the volume %+v at %v %+v", round, step, flat, x, y, stats, vol, p, deepStats)
			}
			if gx, gy := GridPos(&flat); gx != x || gy != y || p != vmath.PointAt3(vol.Pos) {
				t.Fatalf("round %d step %d: returned (%d,%d) and %v for bodies on (%d,%d) and %v", round, step, x, y, p, gx, gy, vmath.PointAt3(vol.Pos))
			}
		}
	}

	box := vmath.Box{Width: 10, Height: 10, Depth: 10}
	slab := func(c vmath.Point3) bool { return c.Z == 7 }
	k := Kinetic3D{Pos: v3(5.5, 5.5, 2.5), Vel: v3(0, 0, 50)}
	p, stats := IntegrateWithBounce3D(&k, 0.08, vmath.Point3{Z: -1}, box, 1, slab)
	if stats.WallCollisions != 1 || stats.BoundaryReflections != 0 || p.Z > 5 || p.Z < 4 || k.Vel.Z != -50 || p.X != 5 || p.Y != 5 {
		t.Fatalf("a body one layer under its corner ended on %v moving %v after %+v, want off the slab at layer 5 and falling", p, k.Vel, stats)
	}
	k = Kinetic3D{Pos: v3(5.5, 5.5, 8.5), Vel: v3(0, 0, 40)}
	p, stats = IntegrateWithBounce3D(&k, 0.05, vmath.Point3{}, box, 0.5, func(vmath.Point3) bool { return false })
	if p.Z != 9 || stats.BoundaryReflections != 1 || !near(k.Vel.Z, -20) || k.Pos.Z > 9.5 {
		t.Fatalf("a body at the last layer ended on %v at %v moving %v after %+v, want layer 9 at half speed back", p, k.Pos, k.Vel, stats)
	}
	k = Kinetic3D{Pos: v3(5.5, 5.5, 5.5), Vel: v3(4000, -3000, 5000), Accel: v3(0, 0, -900)}
	if p, _ = IntegrateWithBounce3D(&k, 0.05, vmath.Point3{}, box, 0.5, slab); !box.ContainsPoint(p) {
		t.Fatalf("an extreme velocity left the box for %v", p)
	}
}

// TestACollisionSpreadsItsImpulseWithinACone: a spread turns the impulse off the
// impactor's direction by no more than its half angle, to every side of it, at a
// size inside its bounds; one seed draws one impulse, with an offset or none, no
// source draws no spread, and no direction strikes along x.
func TestACollisionSpreadsItsImpulseWithinACone(t *testing.T) {
	const half, lo, hi, ratio = 0.35, 15.0, 40.0, 0.5
	rng := vmath.NewFastRand(0xc0)
	dir := vmath.V3Normalize(v3(2, -1, 0.5))
	for _, dir := range []vmath.Vec3{dir, v3(0, 0, -1)} {
		across := vmath.V3Normalize(vmath.V3Cross(dir, v3(0, 1, 0)))
		var above, below, wide int
		for range 5000 {
			impulse := ApplyCollisionImpulse3D(vmath.V3Scale(dir, 9), ratio, half, lo, hi, rng)
			size := vmath.V3Mag(impulse)
			angle := math.Acos(min(vmath.V3Dot(impulse, dir)/size, 1))
			// each of the two turns reads its angle from a table a 1024th of a turn apart
			if size < lo*ratio-1e-9 || size > hi*ratio+1e-9 || angle > half+2*vmath.TwoPi/vmath.LUTSize {
				t.Fatalf("impulse %v is %v long and %v off %v", impulse, size, angle, dir)
			}
			switch side := vmath.V3Dot(impulse, across); {
			case side > 0:
				above++
			case side < 0:
				below++
			}
			if angle > half/2 {
				wide++
			}
		}
		if above < 1500 || below < 1500 || wide < 1500 {
			t.Fatalf("the spread about %v fell %d to one side, %d to the other and %d past half its angle of 5000", dir, above, below, wide)
		}
	}
	one := ApplyCollisionImpulse3D(dir, ratio, half, lo, hi, vmath.NewFastRand(7))
	if again := ApplyCollisionImpulse3D(dir, ratio, half, lo, hi, vmath.NewFastRand(7)); again != one {
		t.Fatalf("one seed drew %v and then %v", one, again)
	}
	if got := ApplyOffsetCollisionImpulse3D(dir, vmath.Point3{X: 2, Z: -1}, 0, ratio, half, lo, hi, vmath.NewFastRand(7)); got != one {
		t.Fatalf("an offset of no influence drew %v, the plain impulse %v", got, one)
	}
	if got := ApplyCollisionImpulse3D(vmath.V3Scale(dir, 3), ratio, half, lo, hi, nil); !nearV3(got, vmath.V3Scale(dir, lo*ratio)) {
		t.Fatalf("no source gave %v, want the direction at the lower bound", got)
	}
	k := Kinetic3D{Vel: v3(1, 2, 3)}
	ApplyCollision3D(&k, vmath.Vec3{}, &CollisionProfile{MassRatio: 1, ImpulseMin: 10, ImpulseMax: 10, Mode: ImpulseOverride}, nil)
	if k.Vel != v3(10, 0, 0) {
		t.Fatalf("an override with no direction left velocity %v, want 10 along x", k.Vel)
	}
	inv := vmath.EllipsoidInvRadiiSq(3, 3, 3)
	if radial, hit := CheckSoftCollision3D(vmath.Point3{X: 4, Y: 4, Z: 4}, vmath.Point3{X: 4, Y: 4, Z: 4}, inv); !hit || radial != v3(1, 0, 0) {
		t.Fatalf("a contact on one cell pushes %v (%t), want along x", radial, hit)
	}
}

// TestOrbits3DTurnAboutTheirAxis: about z an insertion is the flat one in either
// sense, and about any axis it is square to the axis and the radius at the speed
// a circle needs, which a step under the attraction then keeps.
func TestOrbits3DTurnAboutTheirAxis(t *testing.T) {
	rng := vmath.NewFastRand(0x0b)
	for range 2000 {
		dx, dy := rng.Float64()*40-20, rng.Float64()*40-20
		for _, clockwise := range []bool{false, true} {
			axis := v3(0, 0, 1)
			if clockwise {
				axis.Z = -1
			}
			vx, vy := OrbitalInsert(dx, dy, 12, clockwise)
			if got := OrbitalInsert3D(v3(dx, dy, 0), axis, 12); !nearV3(got, v3(vx, vy, 0)) {
				t.Fatalf("insertion at (%v,%v) about %v is %v, the flat (%v,%v)", dx, dy, axis, got, vx, vy)
			}
		}
		ax, ay := OrbitalAttraction(dx, dy, 12)
		ex, ey := OrbitalEquilibrium(dx, dy, 7, 3)
		px, py := OrbitalDamp(3, -2, dx, dy, 0.5, 0.016)
		d := v3(dx, dy, 0)
		if !nearV3(OrbitalAttraction3D(d, 12), v3(ax, ay, 0)) || !nearV3(OrbitalEquilibrium3D(d, 7, 3), v3(ex, ey, 0)) ||
			!nearV3(OrbitalDamp3D(v3(3, -2, 0), d, 0.5, 0.016), v3(px, py, 0)) {
			t.Fatalf("at (%v,%v) the volume's attraction, equilibrium or damping parts from the flat", dx, dy)
		}
	}
	axis := vmath.V3Normalize(v3(1, 2, -2))
	d := vmath.V3Scale(vmath.V3Normalize(vmath.V3Cross(axis, v3(0, 0, 1))), 9)
	k := Kinetic3D{Pos: d, Vel: OrbitalInsert3D(d, axis, 16)}
	if math.Abs(vmath.V3Dot(k.Vel, axis)) > 1e-9 || math.Abs(vmath.V3Dot(k.Vel, d)) > 1e-9 || !near(vmath.V3Mag(k.Vel), 12) {
		t.Fatalf("insertion %v at %v about %v is not square to both at speed 12", k.Vel, d, axis)
	}
	for range 4000 {
		k.Accel = OrbitalAttraction3D(k.Pos, 16)
		Integrate3D(&k, 0.001)
	}
	if r := vmath.V3Mag(k.Pos); math.Abs(r-9) > 0.1 || math.Abs(vmath.V3Dot(k.Pos, axis)) > 0.1 {
		t.Fatalf("after four seconds the orbit stands %v from its center and %v off its plane", r, vmath.V3Dot(k.Pos, axis))
	}
	if OrbitalInsert3D(vmath.V3Scale(axis, 5), axis, 16) != (vmath.Vec3{}) || OrbitalEquilibrium3D(vmath.Vec3{}, 7, 3) != v3(3, 0, 0) {
		t.Fatal("an insertion along the axis is not zero, or the center's push not along x")
	}
}
