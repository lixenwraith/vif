package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// Eye Dimensions (shared across all types)
const (
	EyeWidth         = 5
	EyeHeight        = 3
	EyeHeaderOffsetX = 2 // Center column
	EyeHeaderOffsetY = 1 // Center row
)

const (
	EyeTypeCount = 7

	EyeRestitutionFloat = 0.5

	// Shield drain per tick when any eye member overlaps cursor shield
	EyeShieldDrain = 500

	// Heat penalty when eye occupies cursor cell without shield
	EyeDamageHeat = 5

	// TODO: make the radii flexible

	// EyeContactCheckDistSq is squared distance threshold for target contact member iteration
	// Avoids per-member spatial queries when eye is far from target
	EyeContactCheckDistSq = 100

	// EyeSelfDestructRadius is the proximity trigger distance (cells) for entity target contact
	// Sized to cover default tower footprint (radiusX=6) + 1 cell buffer
	// Used for both combat trigger and visual explosion effect
	EyeSelfDestructRadius   = 7
	EyeSelfDestructRadiusSq = EyeSelfDestructRadius * EyeSelfDestructRadius
)

var EyeExplosionRadius = vmath.FromFloat(float64(EyeSelfDestructRadius))

// Navigation — band routing spawn defaults (pre-GA override)
var (
	EyeNavBudgetMultiplierDefault = vmath.FromFloat(1.3)
	EyeNavExplorationBiasDefault  = vmath.FromFloat(0.3)
)

var EyeRestitution = vmath.FromFloat(EyeRestitutionFloat)

// EyeTypeParams holds per-type configuration
type EyeTypeParams struct {
	HP            int
	BaseSpeed     float64
	HomingAccel   float64
	Drag          float64
	FrameCount    int
	FrameDuration time.Duration
}

// EyeTypeTable indexed by EyeType iota values
var EyeTypeTable = [EyeTypeCount]EyeTypeParams{
	// 0: Void — medium, contemplative
	{HP: 15, BaseSpeed: 2.0, HomingAccel: 3.0, Drag: 2.0, FrameCount: 5, FrameDuration: 600 * time.Millisecond},
	// 1: Flame — fast, fragile
	{HP: 8, BaseSpeed: 4.0, HomingAccel: 6.0, Drag: 2.5, FrameCount: 4, FrameDuration: 300 * time.Millisecond},
	// 2: Frost — slow, tanky
	{HP: 25, BaseSpeed: 1.5, HomingAccel: 2.0, Drag: 1.5, FrameCount: 4, FrameDuration: 600 * time.Millisecond},
	// 3: Storm — medium, electric
	{HP: 12, BaseSpeed: 3.0, HomingAccel: 5.0, Drag: 2.0, FrameCount: 3, FrameDuration: 600 * time.Millisecond},
	// 4: Blood — aggressive
	{HP: 18, BaseSpeed: 3.5, HomingAccel: 5.0, Drag: 2.0, FrameCount: 4, FrameDuration: 450 * time.Millisecond},
	// 5: Golden — resilient
	{HP: 30, BaseSpeed: 1.8, HomingAccel: 2.5, Drag: 1.5, FrameCount: 4, FrameDuration: 750 * time.Millisecond},
	// 6: Abyss — shifty
	{HP: 10, BaseSpeed: 2.5, HomingAccel: 4.0, Drag: 2.0, FrameCount: 4, FrameDuration: 600 * time.Millisecond},
}