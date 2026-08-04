package profile

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/physics"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func TestMassRatioClamped(t *testing.T) {
	if got := massRatio(MassPylon, MassDrain); got != MassRatioMax {
		t.Errorf("pylon/drain = %d, want clamp %d", got, MassRatioMax)
	}
	if got := massRatio(MassDust, MassStorm); got != MassRatioMin {
		t.Errorf("dust/storm = %d, want clamp %d", got, MassRatioMin)
	}
	if got := massRatio(MassDrain, MassDrain); got != vmath.Scale {
		t.Errorf("equal masses = %d, want Scale", got)
	}
}

func TestMassRatioMonotonic(t *testing.T) {
	// heavier target absorbs more: ratio must not increase
	targets := []Mass{MassDrain, MassSwarm, MassEye, MassQuasar, MassStorm}
	prev := int64(1) << 62
	for _, tgt := range targets {
		r := massRatio(MassCleaner, tgt)
		if r > prev {
			t.Fatalf("ratio rose with target mass: %d > %d", r, prev)
		}
		prev = r
	}
}

// allProfiles enumerates every shipped collision profile for invariant checks
func allProfiles() map[string]*physics.CollisionProfile {
	return map[string]*physics.CollisionProfile{
		"CleanerToDrain": &CleanerToDrain, "CleanerToSwarm": &CleanerToSwarm,
		"CleanerToQuasar": &CleanerToQuasar, "CleanerToStorm": &CleanerToStorm,
		"CleanerToSnakeHead": &CleanerToSnakeHead, "CleanerToSnakeBody": &CleanerToSnakeBody,
		"CleanerToEye":  &CleanerToEye,
		"ShieldToDrain": &ShieldToDrain, "ShieldToSwarm": &ShieldToSwarm,
		"ShieldToQuasar": &ShieldToQuasar, "ShieldToStorm": &ShieldToStorm,
		"ShieldToSnakeHead": &ShieldToSnakeHead, "ShieldToSnakeBody": &ShieldToSnakeBody,
		"ShieldToEye":      &ShieldToEye,
		"ExplosionToDrain": &ExplosionToDrain, "ExplosionToSwarm": &ExplosionToSwarm,
		"ExplosionToQuasar": &ExplosionToQuasar, "ExplosionToStorm": &ExplosionToStorm,
		"ExplosionToSnakeHead": &ExplosionToSnakeHead, "ExplosionToSnakeBody": &ExplosionToSnakeBody,
		"ExplosionToEye": &ExplosionToEye,
		"DustToDrain":    &DustToDrain, "DustToComposite": &DustToComposite,
		"SoftDrainToQuasar": &SoftDrainToQuasar, "SoftSwarmToSwarm": &SoftSwarmToSwarm,
		"SoftSwarmToQuasar": &SoftSwarmToQuasar, "SoftQuasarToSwarm": &SoftQuasarToSwarm,
		"SoftQuasarToDrain": &SoftQuasarToDrain, "SoftQuasarToQuasar": &SoftQuasarToQuasar,
		"SoftStormToSwarm": &SoftStormToSwarm, "SoftStormToQuasar": &SoftStormToQuasar,
		"SoftPylonToDrain": &SoftPylonToDrain, "SoftPylonToSwarm": &SoftPylonToSwarm,
		"SoftPylonToQuasar": &SoftPylonToQuasar,
	}
}

func TestProfileInvariants(t *testing.T) {
	for name, p := range allProfiles() {
		if p.MassRatio < MassRatioMin || p.MassRatio > MassRatioMax {
			t.Errorf("%s: MassRatio %d outside clamp band", name, p.MassRatio)
		}
		if p.ImpulseMin <= 0 || p.ImpulseMax < p.ImpulseMin {
			t.Errorf("%s: impulse bounds [%d, %d]", name, p.ImpulseMin, p.ImpulseMax)
		}
		if p.AngleVariance < 0 {
			t.Errorf("%s: negative angle variance", name)
		}
		if p.OffsetInfluence < 0 || p.OffsetInfluence > vmath.Scale {
			t.Errorf("%s: OffsetInfluence %d outside [0, Scale]", name, p.OffsetInfluence)
		}
	}
}

func TestProfilesProduceUsableImpulse(t *testing.T) {
	rng := vmath.NewFastRand(0xB0A7)
	for name, p := range allProfiles() {
		ix, iy := physics.ImpulseFromProfile(vmath.FromFloat(1), vmath.FromFloat(1), p, rng)
		if ix == 0 && iy == 0 {
			t.Errorf("%s: produced zero impulse", name)
		}
		if m := vmath.Magnitude(ix, iy); m > vmath.FromFloat(1000) {
			t.Errorf("%s: impulse magnitude %v cells/sec is implausible", name, vmath.ToFloat(m))
		}
	}
}

func TestHomingProfilesSane(t *testing.T) {
	profiles := map[string]*physics.HomingProfile{
		"Drain": &DrainHoming, "Swarm": &SwarmHoming, "Quasar": &QuasarHoming,
		"Snake": &SnakeHoming, "Loot": &LootHoming, "Missile": &MissileHoming,
	}
	for name, p := range profiles {
		if p.HomingAccel <= 0 {
			t.Errorf("%s: non-positive HomingAccel", name)
		}
		if p.Drag < 0 || p.BaseSpeed < 0 {
			t.Errorf("%s: negative Drag or BaseSpeed", name)
		}
		if p.ArrivalDragBoost > 0 && p.ArrivalRadius <= 0 {
			t.Errorf("%s: ArrivalDragBoost set without ArrivalRadius", name)
		}
	}
	for i := range EyeHomingProfiles {
		if EyeHomingProfiles[i].HomingAccel <= 0 {
			t.Errorf("EyeHomingProfiles[%d]: uninitialized", i)
		}
	}
}
