package parameter

import (
	"time"
)

// Materialization Effect
const (
	// A spawn's materialize: each beam's head runs from the map edge to the target
	// through the fill, the beam holds, then the tail runs after it through the
	// recede, the slow trail of something being ported in
	MaterializeFill   = 400 * time.Millisecond
	MaterializeHold   = 200 * time.Millisecond
	MaterializeRecede = 400 * time.Millisecond

	// The nugget beacon's materialize: quicker, and at one speed in and out, so
	// each beam reads as one pass rather than a sweep and a faster second one
	MaterializeNuggetFill   = 270 * time.Millisecond
	MaterializeNuggetHold   = 60 * time.Millisecond
	MaterializeNuggetRecede = 270 * time.Millisecond

	// Materialize visual parameters
	MaterializeTailIntensity = 0.35 // The lit segment's dim end; the gradient never falls to black
	MaterializePulseHz       = 4    // Pulse at the fill's head; 8 Hz aliased at the 20 Hz tick into a flicker
	MaterializeWidthFalloff  = 0.5  // Side-line intensity for multi-width beams
)

// Lightning Entity
const (
	LightningAlpha = 0.8

	// A zap settled before a driven tick must survive that tick to reach a frame.
	LightningZapDuration = 2 * GameUpdateInterval

	// LightningTrackedLease retires a tracked bolt its owner stopped renewing; a
	// renewal settles as the tick that raised it closes, after that tick's timer
	// pass, so the lease must outlast one pass
	LightningTrackedLease = 4 * GameUpdateInterval
)

// Spirit Entity
const (
	// SpiritAnimationDuration is the time for spirits to converge to target
	SpiritAnimationDuration = 500 * time.Millisecond
)

// Explosion Field
const (
	// ExplosionFieldRadius is visual radius in cells (aspect-corrected)
	ExplosionFieldRadius = 12.0

	// ExplosionFieldDuration is total fade time
	ExplosionFieldDuration = 300 * time.Millisecond

	// ExplosionFieldDurationNano is ExplosionFieldDuration in nanoseconds for integer age math
	ExplosionFieldDurationNano = int64(ExplosionFieldDuration)

	// ExplosionCenterCap is maximum concurrent explosion centers and maximum centers one batch explosion can request
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

	// Inside the arrival radius the drag is boosted MissileArrivalDragBoost times
	// while MissileArrivalAccelMin of the homing accel stays; MissileDeadZone snaps
	MissileArrivalDragBoost = 2.0
	MissileArrivalAccelMin  = 1.0
	MissileDeadZone         = 0.1

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
