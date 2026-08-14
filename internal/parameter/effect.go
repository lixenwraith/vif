package parameter

import (
	"time"
)

// Materialization Effect
const (
	// MaterializeAnimationDuration is the time for spawners to converge
	MaterializeAnimationDuration = 1 * time.Second

	// Materialize phase thresholds (normalized 0.0-1.0)
	MaterializeFillEnd = 0.4 // Fill phase ends, hold begins
	MaterializeHoldEnd = 0.6 // Hold phase ends, recede begins
	MaterializePulseHz = 8   // Sine wave cycles during fill phase

	// Materialize visual parameters
	MaterializeWidthFalloff = 0.5 // Side-line intensity for multi-width beams
)

// Lightning Entity
const (
	LightningAlpha = 0.8

	// LightningZapDuration is the visual duration of short zap effects (vampire, buff)
	// One tick: the shortest interval a game-time duration can span
	LightningZapDuration = GameUpdateInterval
)

// Spirit Entity
const (
	// SpiritAnimationDuration is the time for spirits to converge to target
	SpiritAnimationDuration = 500 * time.Millisecond

	// SpiritSafetyBuffer is additional time before safety despawn to allow final frame render
	SpiritSafetyBuffer = 100 * time.Millisecond
)

// Explosion Field
const (
	// ExplosionFieldRadius is visual radius in cells (aspect-corrected)
	ExplosionFieldRadius = 12.0

	// ExplosionFieldDuration is total fade time
	ExplosionFieldDuration = 300 * time.Millisecond

	// ExplosionCenterCap is maximum concurrent explosion centers
	ExplosionCenterCap = 256

	// ExplosionMergeThreshold is distance for center merging (cells)
	ExplosionMergeThreshold = 4.0

	// ExplosionIntensityBoost is intensity added on merge
	ExplosionIntensityBoost = 0.3

	// ExplosionRadiusBoost is radius expansion on merge (cells)
	ExplosionRadiusBoost = 0.5

	// ExplosionIntensityCap is maximum intensity after merges
	ExplosionIntensityCap = 3.0

	// ExplosionRadiusCapMultiplier caps radius growth (× base)
	ExplosionRadiusCapMultiplier = 1.5

	// Render intensity thresholds (0.0-1.0)
	ExplosionEdgeThreshold = 0.03

	// Explosion Visual Parameters (0.0-1.0)
	ExplosionAlphaMax         = 0.8
	ExplosionAlphaMin         = 0.1
	ExplosionGradientMidpoint = 0.5
)

// Missile Phase
const (
	// MissileMaxSpeed is base homing velocity (cells/sec).
	MissileMaxSpeed = 180.0

	// MissileHomingAccel is steering acceleration (cells/sec²).
	MissileHomingAccel = 400.0

	// MissileDrag is velocity damping for stable turns
	MissileDrag = 4.0

	// MissileSpreadTurns preserves the original full-turn spread value.
	MissileSpreadTurns = 2.1

	// MissileStaggerFactor is velocity reduction per child index (0.05 = 5%)
	MissileStaggerFactor = 0.05

	// MissileArrivalRadius is distance to begin braking (cells)
	MissileArrivalRadius = 2.0

	// MissileMaxLifetime is safety timeout for orphaned missiles
	MissileMaxLifetime = 3 * time.Second
)

// Missile Visuals
const (
	// MissileTrailMaxAge is duration before trail point fades completely
	MissileTrailMaxAge = 300 * time.Millisecond

	// MissileTrailInterval is duration between trail point emissions
	MissileTrailInterval = 50 * time.Millisecond

	// MissileExplosionRadius is visual radius for impact explosion (smaller than main)
	MissileExplosionRadius = 6.0
)

// Destruction
const (
	// DestructionFlashDuration is how long the destruction flash effect lasts in milliseconds
	DestructionFlashDuration = 500 * time.Millisecond

	// FadeoutDuration is how long the fadeout effect lasts
	FadeoutDuration = 400 * time.Millisecond
)
