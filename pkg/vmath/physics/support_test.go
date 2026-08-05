package physics

import "math"

type kin = Kinetic

func newKin(px, py, vx, vy float64) kin {
	return kin{
		PreciseX: px,
		PreciseY: py,
		VelX:     vx,
		VelY:     vy,
	}
}

func posOf(k *kin) (float64, float64) {
	return k.PreciseX, k.PreciseY
}

func velOf(k *kin) (float64, float64) {
	return k.VelX, k.VelY
}

func distTo(k *kin, tx, ty float64) float64 {
	x, y := posOf(k)
	return math.Hypot(x-tx, y-ty)
}

func speedOf(k *kin) float64 {
	vx, vy := velOf(k)
	return math.Hypot(vx, vy)
}

// Test-local profiles are deliberately independent of internal/parameter.
var (
	// braked: BaseSpeed 0 makes drag apply at every speed
	profBraked = HomingProfile{
		HomingAccel:      20.0,
		Drag:             4.0,
		ArrivalRadius:    3.0,
		ArrivalDragBoost: 5.0,
		DeadZone:         0.5,
	}

	// cruise: mirrors the shipped species pattern, drag only above BaseSpeed
	profCruise = HomingProfile{
		BaseSpeed:        5.0,
		HomingAccel:      20.0,
		Drag:             4.0,
		ArrivalRadius:    3.0,
		ArrivalDragBoost: 5.0,
		DeadZone:         0.5,
	}

	// deterministic impulse: equal bounds and zero variance consume no RNG
	profImpulse = CollisionProfile{
		MassRatio:     1.0,
		ImpulseMin:    10.0,
		ImpulseMax:    10.0,
		AngleVariance: 0,
		Mode:          ImpulseAdditive,
	}
)

func noWall(int, int) bool { return false }
