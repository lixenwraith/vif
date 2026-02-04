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

	// LightningZapDuration is visual duration for short zap effects (vampire, buff)
	// 2 frames at 60fps for perceptible but brief flash
	LightningZapDuration = 2 * FrameUpdateInterval
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
	// ExplosionFieldRadiusFloat is visual radius in cells (aspect-corrected)
	ExplosionFieldRadiusFloat = 12.0

	// ExplosionFieldDuration is total fade time
	ExplosionFieldDuration = 300 * time.Millisecond

	// ExplosionCenterCap is maximum concurrent explosion centers
	ExplosionCenterCap = 256

	// ExplosionMergeThresholdFloat is distance for center merging (cells)
	ExplosionMergeThresholdFloat = 4.0

	// ExplosionIntensityBoostFloat is intensity added on merge
	ExplosionIntensityBoostFloat = 0.3

	// ExplosionRadiusBoostFloat is radius expansion on merge (cells)
	ExplosionRadiusBoostFloat = 0.5

	// ExplosionIntensityCapFloat is maximum intensity after merges
	ExplosionIntensityCapFloat = 3.0

	// ExplosionRadiusCapMultiplier caps radius growth (× base)
	ExplosionRadiusCapMultiplier = 1.5

	// Render intensity thresholds (0.0-1.0, mapped to Scale)
	ExplosionCoreThresholdFloat = 0.4
	ExplosionBodyThresholdFloat = 0.15
	ExplosionEdgeThresholdFloat = 0.03

	// Explosion Visual Parameters (0.0-1.0)
	ExplosionAlphaMaxFloat         = 0.8
	ExplosionAlphaMinFloat         = 0.1
	ExplosionGradientMidpointFloat = 0.5
)

// Cluster Missile - Parent Phase
const (
	// MissileClusterLaunchSpeedFloat is parent travel speed (cells/sec)
	MissileClusterLaunchSpeedFloat = 90.0

	// TODO: use
	// MissileClusterSplitRatio is fraction of distance to centroid before split (0.5 = halfway)
	MissileClusterSplitRatio = 0.5

	// MissileClusterMinDistanceFloat is minimum travel before split allowed (cells)
	MissileClusterMinDistanceFloat = 8.0

	// MissileParentMaxLifetime is safety timeout for forced split
	MissileParentMaxLifetime = 1 * time.Second
)

// Cluster Missile - Child/Seeker Phase
const (
	// MissileSeekerMaxSpeedFloat is base homing velocity (cells/sec)
	MissileSeekerMaxSpeedFloat = 180.0

	// MissileSeekerHomingAccelFloat is steering acceleration (cells/sec²)
	MissileSeekerHomingAccelFloat = 400.0

	// MissileSeekerDragFloat is velocity damping for stable turns
	MissileSeekerDragFloat = 4.0

	// MissileSeekerSpreadAngleFloat is arc spread for children spawn (radians, ~120°)
	MissileSeekerSpreadAngleFloat = 2.1

	// MissileSeekerStaggerFactor is velocity reduction per child index (0.05 = 5%)
	MissileSeekerStaggerFactor = 0.05

	// MissileSeekerArrivalRadius is distance to begin braking (cells)
	MissileSeekerArrivalRadiusFloat = 2.0

	// MissileSeekerMaxLifetime is safety timeout for orphaned seekers
	MissileSeekerMaxLifetime = 3 * time.Second
)

// Missile Visuals - Parent and Child
const (
	// MissileTrailMaxAge is duration before trail point fades completely
	MissileTrailMaxAge = 300 * time.Millisecond

	// MissileTrailInterval is duration between trail point emissions
	MissileTrailInterval = 50 * time.Millisecond

	// MissileExplosionRadiusFloat is visual radius for impact explosion (smaller than main)
	MissileExplosionRadiusFloat = 6.0
)

// Destruction
const (
	// DestructionFlashDuration is how long the destruction flash effect lasts in milliseconds
	DestructionFlashDuration = 500 * time.Millisecond

	// FadeoutDuration is how long the fadeout effect lasts
	FadeoutDuration = 400 * time.Millisecond
)