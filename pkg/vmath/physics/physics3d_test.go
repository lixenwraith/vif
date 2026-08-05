package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func v3(x, y, z float64) vmath.Vec3F {
	return vmath.Vec3F{X: x, Y: y, Z: z}
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

	pBefore := vmath.V3FAdd(vmath.V3FScale(velA, massA), vmath.V3FScale(velB, massB))
	if !ElasticCollision3D(&posA, &posB, &velA, &velB, massA, massB, 1) {
		t.Skip("configuration is separating")
	}
	pAfter := vmath.V3FAdd(vmath.V3FScale(velA, massA), vmath.V3FScale(velB, massB))

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
	d := vmath.V3FMag(vmath.V3FSub(posB, posA))
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

	if z := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(0, 0, 0), 1, 1, repulsionR, 5); z != (vmath.Vec3F{}) {
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
	n := vmath.V3FNormalize(v3(3, 4, 12))
	if m := vmath.V3FMag(n); math.Abs(m-1) > 1e-9 {
		t.Fatalf("normalized magnitude = %v", m)
	}
	if vmath.V3FNormalize(vmath.Vec3F{}) != (vmath.Vec3F{}) {
		t.Fatal("zero vector must normalize to zero")
	}
}

func TestV3ClampMagnitude(t *testing.T) {
	v := v3(30, 40, 0)
	c := vmath.V3FClampMagnitude(v, 5)
	if m := vmath.V3FMag(c); math.Abs(m-5) > 1e-9 {
		t.Fatalf("clamped magnitude = %v, want 5", m)
	}
	small := v3(1, 1, 1)
	if vmath.V3FClampMagnitude(small, 100) != small {
		t.Fatal("under-limit vector was modified")
	}
}

func TestV3DampDtBounds(t *testing.T) {
	v := v3(10, 0, 0)
	// dt large enough to over-decay must clamp at zero, not invert.
	d := vmath.V3FDampDt(v, 0, 10)
	if d.X != 0 {
		t.Fatalf("over-decay produced %v, want 0", d.X)
	}
	// No decay.
	if n := vmath.V3FDampDt(v, 1, 1); n != v {
		t.Fatal("factor=1 must not change the vector")
	}
}
