package profile

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// profileTolerance covers FromFloat round-trip error (~2.3e-10) with margin
const profileTolerance = 1e-6

func eqF(t *testing.T, name, field string, fixed int64, flt float64) {
	t.Helper()
	if e := math.Abs(vmath.ToFloat(fixed) - flt); e > profileTolerance {
		t.Errorf("%s.%s: fixed %v, float %v (delta %g)", name, field, vmath.ToFloat(fixed), flt, e)
	}
}

func assertHoming(t *testing.T, name string, a *physics.HomingProfile, b *physics.HomingProfileF) {
	t.Helper()
	eqF(t, name, "BaseSpeed", a.BaseSpeed, b.BaseSpeed)
	eqF(t, name, "HomingAccel", a.HomingAccel, b.HomingAccel)
	eqF(t, name, "Drag", a.Drag, b.Drag)
	eqF(t, name, "ArrivalRadius", a.ArrivalRadius, b.ArrivalRadius)
	eqF(t, name, "ArrivalDragBoost", a.ArrivalDragBoost, b.ArrivalDragBoost)
	eqF(t, name, "ArrivalAccelMin", a.ArrivalAccelMin, b.ArrivalAccelMin)
	eqF(t, name, "DeadZone", a.DeadZone, b.DeadZone)
}

func assertCollision(t *testing.T, name string, a *physics.CollisionProfile, b *physics.CollisionProfileF) {
	t.Helper()
	eqF(t, name, "MassRatio", a.MassRatio, b.MassRatio)
	eqF(t, name, "ImpulseMin", a.ImpulseMin, b.ImpulseMin)
	eqF(t, name, "ImpulseMax", a.ImpulseMax, b.ImpulseMax)
	eqF(t, name, "AngleVariance", a.AngleVariance, b.AngleVariance)
	eqF(t, name, "OffsetInfluence", a.OffsetInfluence, b.OffsetInfluence)
	if a.Mode != b.Mode {
		t.Errorf("%s.Mode: fixed %v, float %v", name, a.Mode, b.Mode)
	}
}

func TestHomingProfileParity(t *testing.T) {
	pairs := []struct {
		name string
		a    *physics.HomingProfile
		b    *physics.HomingProfileF
	}{
		{"DrainHoming", &DrainHoming, &DrainHomingF},
		{"SwarmHoming", &SwarmHoming, &SwarmHomingF},
		{"QuasarHoming", &QuasarHoming, &QuasarHomingF},
		{"SnakeHoming", &SnakeHoming, &SnakeHomingF},
		{"LootHoming", &LootHoming, &LootHomingF},
		{"MissileHoming", &MissileHoming, &MissileHomingF},
	}

	for _, p := range pairs {
		assertHoming(t, p.name, p.a, p.b)
	}

	if len(EyeHomingProfiles) != len(EyeHomingProfilesF) {
		t.Fatalf("eye table length %d vs %d", len(EyeHomingProfiles), len(EyeHomingProfilesF))
	}
	for i := range EyeHomingProfiles {
		assertHoming(t, "EyeHomingProfiles["+string(rune('0'+i))+"]",
			&EyeHomingProfiles[i], &EyeHomingProfilesF[i])
	}
}

func TestCollisionProfileParity(t *testing.T) {
	pairs := []struct {
		name string
		a    *physics.CollisionProfile
		b    *physics.CollisionProfileF
	}{
		{"CleanerToDrain", &CleanerToDrain, &CleanerToDrainF},
		{"CleanerToSwarm", &CleanerToSwarm, &CleanerToSwarmF},
		{"CleanerToQuasar", &CleanerToQuasar, &CleanerToQuasarF},
		{"CleanerToStorm", &CleanerToStorm, &CleanerToStormF},
		{"CleanerToSnakeHead", &CleanerToSnakeHead, &CleanerToSnakeHeadF},
		{"CleanerToSnakeBody", &CleanerToSnakeBody, &CleanerToSnakeBodyF},
		{"CleanerToEye", &CleanerToEye, &CleanerToEyeF},
		{"ShieldToDrain", &ShieldToDrain, &ShieldToDrainF},
		{"ShieldToSwarm", &ShieldToSwarm, &ShieldToSwarmF},
		{"ShieldToQuasar", &ShieldToQuasar, &ShieldToQuasarF},
		{"ShieldToStorm", &ShieldToStorm, &ShieldToStormF},
		{"ShieldToSnakeHead", &ShieldToSnakeHead, &ShieldToSnakeHeadF},
		{"ShieldToSnakeBody", &ShieldToSnakeBody, &ShieldToSnakeBodyF},
		{"ShieldToEye", &ShieldToEye, &ShieldToEyeF},
		{"ExplosionToDrain", &ExplosionToDrain, &ExplosionToDrainF},
		{"ExplosionToSwarm", &ExplosionToSwarm, &ExplosionToSwarmF},
		{"ExplosionToQuasar", &ExplosionToQuasar, &ExplosionToQuasarF},
		{"ExplosionToStorm", &ExplosionToStorm, &ExplosionToStormF},
		{"ExplosionToSnakeHead", &ExplosionToSnakeHead, &ExplosionToSnakeHeadF},
		{"ExplosionToSnakeBody", &ExplosionToSnakeBody, &ExplosionToSnakeBodyF},
		{"ExplosionToEye", &ExplosionToEye, &ExplosionToEyeF},
		{"DustToDrain", &DustToDrain, &DustToDrainF},
		{"DustToComposite", &DustToComposite, &DustToCompositeF},
		{"SoftDrainToQuasar", &SoftDrainToQuasar, &SoftDrainToQuasarF},
		{"SoftSwarmToSwarm", &SoftSwarmToSwarm, &SoftSwarmToSwarmF},
		{"SoftSwarmToQuasar", &SoftSwarmToQuasar, &SoftSwarmToQuasarF},
		{"SoftQuasarToSwarm", &SoftQuasarToSwarm, &SoftQuasarToSwarmF},
		{"SoftQuasarToDrain", &SoftQuasarToDrain, &SoftQuasarToDrainF},
		{"SoftQuasarToQuasar", &SoftQuasarToQuasar, &SoftQuasarToQuasarF},
		{"SoftStormToSwarm", &SoftStormToSwarm, &SoftStormToSwarmF},
		{"SoftStormToQuasar", &SoftStormToQuasar, &SoftStormToQuasarF},
		{"SoftPylonToDrain", &SoftPylonToDrain, &SoftPylonToDrainF},
		{"SoftPylonToSwarm", &SoftPylonToSwarm, &SoftPylonToSwarmF},
		{"SoftPylonToQuasar", &SoftPylonToQuasar, &SoftPylonToQuasarF},
	}

	for _, p := range pairs {
		assertCollision(t, p.name, p.a, p.b)
	}
}

func TestMassParity(t *testing.T) {
	pairs := []struct {
		name string
		a    Mass
		b    MassF
	}{
		{"MassDust", MassDust, MassDustF}, {"MassCursor", MassCursor, MassCursorF},
		{"MassCleaner", MassCleaner, MassCleanerF}, {"MassDrain", MassDrain, MassDrainF},
		{"MassSwarm", MassSwarm, MassSwarmF}, {"MassEye", MassEye, MassEyeF},
		{"MassSnakeBody", MassSnakeBody, MassSnakeBodyF}, {"MassSnakeHead", MassSnakeHead, MassSnakeHeadF},
		{"MassQuasar", MassQuasar, MassQuasarF}, {"MassExplosion", MassExplosion, MassExplosionF},
		{"MassStorm", MassStorm, MassStormF}, {"MassPylon", MassPylon, MassPylonF},
		{"MassRatioMin", MassRatioMin, MassRatioMinF}, {"MassRatioMax", MassRatioMax, MassRatioMaxF},
		{"SoftRatioMax", SoftRatioMax, SoftRatioMaxF},
		{"OffsetBody", OffsetBody, OffsetBodyF},
	}
	for _, p := range pairs {
		eqF(t, p.name, "", p.a, p.b)
	}
}
