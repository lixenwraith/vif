package physics

import (
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

const benchBodies = 1024

var benchSink float64

// benchKinetics are bodies spread over a 200×60 map at speeds from a crawl to
// several sub-steps a tick
func benchKinetics() []Kinetic {
	rng := vmath.NewFastRand(0x51)
	ks := make([]Kinetic, benchBodies)
	for i := range ks {
		ks[i] = Kinetic{
			PreciseX: 1 + rng.Float64()*198, PreciseY: 1 + rng.Float64()*58,
			VelX: rng.Float64()*80 - 40, VelY: rng.Float64()*40 - 20,
		}
	}
	return ks
}

// benchWall is a wall every eighth column of every fifth row
func benchWall(x, y int) bool { return x&7 == 0 && y%5 == 0 }

// benchKinetics3D are the same bodies through ten layers
func benchKinetics3D() []Kinetic3D {
	rng := vmath.NewFastRand(0x53)
	ks := make([]Kinetic3D, benchBodies)
	for i, k := range benchKinetics() {
		ks[i] = Kinetic3D{
			Pos: vmath.Vec3{X: k.PreciseX, Y: k.PreciseY, Z: 1 + rng.Float64()*8},
			Vel: vmath.Vec3{X: k.VelX, Y: k.VelY, Z: rng.Float64()*20 - 10},
		}
	}
	return ks
}

// BenchmarkIntegrateWithBounce steps a body a tick between the map's edges and
// through a field of walls, as a quasar or a swarm moves.
func BenchmarkIntegrateWithBounce(b *testing.B) {
	ks := benchKinetics()
	i := 0
	for b.Loop() {
		k := &ks[i&(benchBodies-1)]
		IntegrateWithBounce(k, 0.016, 0, 0, 0, 199, 0, 59, 1, benchWall)
		i++
	}
	benchSink = ks[0].PreciseX
}

// BenchmarkIntegrateWithBounce3D steps the bodies through the same walls on
// every layer of a volume
func BenchmarkIntegrateWithBounce3D(b *testing.B) {
	ks := benchKinetics3D()
	box := vmath.Box{Width: 200, Height: 60, Depth: 10}
	wall := func(c vmath.Point3) bool { return benchWall(c.X, c.Y) }
	i := 0
	for b.Loop() {
		IntegrateWithBounce3D(&ks[i&(benchBodies-1)], 0.016, vmath.Point3{}, box, 1, wall)
		i++
	}
	benchSink = ks[0].Pos.X
}

// BenchmarkApplyHoming steers a body toward a target a few cells to tens away,
// inside and outside its arrival radius, and brakes its turn.
func BenchmarkApplyHoming(b *testing.B) {
	ks := benchKinetics()
	i := 0
	for b.Loop() {
		k := &ks[i&(benchBodies-1)]
		tx, ty := ks[(i+7)&(benchBodies-1)].PreciseX, ks[(i+7)&(benchBodies-1)].PreciseY
		ApplyHomingScaled(k, tx, ty, &profCruise, 1.2, 0.016, true)
		benchSink += TurnSeverity(k, tx, ty, 3, 0.5)
		i++
	}
}

// BenchmarkApplyHoming3D is BenchmarkApplyHoming through a volume
func BenchmarkApplyHoming3D(b *testing.B) {
	ks := benchKinetics3D()
	i := 0
	for b.Loop() {
		k := &ks[i&(benchBodies-1)]
		target := ks[(i+7)&(benchBodies-1)].Pos
		ApplyHomingScaled3D(k, target, &profCruise, 1.2, 0.016, true)
		benchSink += TurnSeverity3D(k, target, 3, 0.5)
		i++
	}
}

// BenchmarkApplyCollision draws and applies an impulse with an angle spread
func BenchmarkApplyCollision(b *testing.B) {
	ks := benchKinetics()
	rng := vmath.NewFastRand(0x52)
	profile := CollisionProfile{MassRatio: 0.7, ImpulseMin: 15, ImpulseMax: 40, AngleVariance: 0.35}
	i := 0
	for b.Loop() {
		k := &ks[i&(benchBodies-1)]
		ApplyCollision(k, k.PreciseX-100, k.PreciseY-30, &profile, rng)
		ApplyLinearDrag(k, 30, 0.016)
		i++
	}
	benchSink = ks[0].VelX
}

// BenchmarkApplyCollision3D is BenchmarkApplyCollision through a volume, its
// spread a cone
func BenchmarkApplyCollision3D(b *testing.B) {
	ks := benchKinetics3D()
	rng := vmath.NewFastRand(0x52)
	profile := CollisionProfile{MassRatio: 0.7, ImpulseMin: 15, ImpulseMax: 40, AngleVariance: 0.35}
	i := 0
	for b.Loop() {
		k := &ks[i&(benchBodies-1)]
		ApplyCollision3D(k, vmath.V3Sub(k.Pos, vmath.Vec3{X: 100, Y: 30, Z: 5}), &profile, rng)
		ApplyLinearDrag3D(k, 30, 0.016)
		i++
	}
	benchSink = ks[0].Vel.X
}
