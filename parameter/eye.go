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

	// TODO: integrate this
	// EyeContactCheckDistSq is squared distance threshold for target contact member iteration
	// Avoids per-member spatial queries when eye is far from target
	EyeContactCheckDistSq = 100

	// TODO: make the radii flexible

	// EyeSelfDestructRadius is the proximity trigger distance (cells) for entity target contact
	// Sized to cover default tower footprint (radiusX=6) + 1 cell buffer
	// Used for both combat trigger and visual explosion effect
	EyeSelfDestructRadius   = 7
	EyeSelfDestructRadiusSq = EyeSelfDestructRadius * EyeSelfDestructRadius
)

var EyeExplosionRadius = vmath.FromFloat(float64(EyeSelfDestructRadius))

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
// Balance: HP×BaseSpeed ≈ 42, HomingAccel ≈ 1.9×BaseSpeed, Drag = Accel/Speed
var EyeTypeTable = [EyeTypeCount]EyeTypeParams{
	// 0: Void — baseline
	{HP: 16, BaseSpeed: 2.6, HomingAccel: 5.0, Drag: 1.9, FrameCount: 5, FrameDuration: 600 * time.Millisecond},
	// 1: Flame — glass cannon
	{HP: 10, BaseSpeed: 4.2, HomingAccel: 8.0, Drag: 1.9, FrameCount: 4, FrameDuration: 300 * time.Millisecond},
	// 2: Frost — tank
	{HP: 26, BaseSpeed: 1.6, HomingAccel: 3.0, Drag: 1.9, FrameCount: 4, FrameDuration: 600 * time.Millisecond},
	// 3: Storm — skirmisher
	{HP: 14, BaseSpeed: 3.0, HomingAccel: 5.5, Drag: 1.8, FrameCount: 3, FrameDuration: 600 * time.Millisecond},
	// 4: Blood — aggressive
	{HP: 12, BaseSpeed: 3.5, HomingAccel: 6.5, Drag: 1.85, FrameCount: 4, FrameDuration: 450 * time.Millisecond},
	// 5: Golden — resilient
	{HP: 22, BaseSpeed: 1.9, HomingAccel: 3.6, Drag: 1.9, FrameCount: 4, FrameDuration: 750 * time.Millisecond},
	// 6: Abyss — shifty (over-curve accel, matched drag: snappier turns, same terminal)
	{HP: 17, BaseSpeed: 2.4, HomingAccel: 5.5, Drag: 2.3, FrameCount: 4, FrameDuration: 600 * time.Millisecond},
}

