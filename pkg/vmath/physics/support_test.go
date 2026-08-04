package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// kin localizes the only internal dependency in the physics test suite.
// Phase 3 moves Kinetic into this package; this alias is the single edit.
type kin = Kinetic

func newKin(px, py, vx, vy float64) kin {
	return kin{
		PreciseX: vmath.FromFloat(px),
		PreciseY: vmath.FromFloat(py),
		VelX:     vmath.FromFloat(vx),
		VelY:     vmath.FromFloat(vy),
	}
}

func posOf(k *kin) (float64, float64) {
	return vmath.ToFloat(k.PreciseX), vmath.ToFloat(k.PreciseY)
}

func velOf(k *kin) (float64, float64) {
	return vmath.ToFloat(k.VelX), vmath.ToFloat(k.VelY)
}

func distTo(k *kin, tx, ty float64) float64 {
	x, y := posOf(k)
	return math.Hypot(x-tx, y-ty)
}

func speedOf(k *kin) float64 {
	vx, vy := velOf(k)
	return math.Hypot(vx, vy)
}

// Test-local profiles. Deliberately independent of internal/parameter so the
// suite is unaffected when Phase 2 evicts the shipped profile table.
var (
	// braked: BaseSpeed 0 makes drag apply at every speed
	profBraked = HomingProfile{
		HomingAccel:      vmath.FromFloat(20.0),
		Drag:             vmath.FromFloat(4.0),
		ArrivalRadius:    vmath.FromFloat(3.0),
		ArrivalDragBoost: vmath.FromFloat(5.0),
		DeadZone:         vmath.Scale / 2,
	}

	// cruise: mirrors the shipped species pattern, drag only above BaseSpeed
	profCruise = HomingProfile{
		BaseSpeed:        vmath.FromFloat(5.0),
		HomingAccel:      vmath.FromFloat(20.0),
		Drag:             vmath.FromFloat(4.0),
		ArrivalRadius:    vmath.FromFloat(3.0),
		ArrivalDragBoost: vmath.FromFloat(5.0),
		DeadZone:         vmath.Scale / 2,
	}

	// deterministic impulse: equal bounds and zero variance consume no RNG
	profImpulse = CollisionProfile{
		MassRatio:     vmath.Scale,
		ImpulseMin:    vmath.FromFloat(10.0),
		ImpulseMax:    vmath.FromFloat(10.0),
		AngleVariance: 0,
		Mode:          ImpulseAdditive,
	}
)

func noWall(int, int) bool { return false }

// --- Float profiles ---

// Float twins of the test profiles; values identical, representation differs
var (
	profBrakedF = HomingProfileF{
		HomingAccel:      20.0,
		Drag:             4.0,
		ArrivalRadius:    3.0,
		ArrivalDragBoost: 5.0,
		DeadZone:         0.5,
	}

	profCruiseF = HomingProfileF{
		BaseSpeed:        5.0,
		HomingAccel:      20.0,
		Drag:             4.0,
		ArrivalRadius:    3.0,
		ArrivalDragBoost: 5.0,
		DeadZone:         0.5,
	}

	profImpulseF = CollisionProfileF{
		MassRatio:     1.0,
		ImpulseMin:    10.0,
		ImpulseMax:    10.0,
		AngleVariance: 0,
		Mode:          ImpulseAdditive,
	}
)
