package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// Drain System
const (
	// DrainMaxCount is the maximum number of drain entities (at 100% heat)
	DrainMaxCount = 10

	// DrainShieldEnergyDrainAmount is energy cost per tick per drain inside shield
	DrainShieldEnergyDrainAmount = 100

	// DrainHeatReductionAmount is heat penalty when drain hits cursor without shield
	DrainHeatReductionAmount = 10

	// DrainSpawnOffsetMax is the maximum random offset from cursor position (±N)
	DrainSpawnOffsetMax = 10

	// DrainSpawnStaggerTicks is game ticks between staggered spawns
	// Set to 0 for simultaneous spawning
	DrainSpawnStaggerTicks = 4
)

// Drain Entity
const (
	// DrainEnergyDrainInterval is the duration between energy drain ticks
	DrainEnergyDrainInterval = 1000 * time.Millisecond

	// DrainBaseSpeed is the normal homing velocity in cells/sec (Q32.32 via vmath.FromFloat)
	// Equivalent to previous 1 cell per DrainMoveInterval
	DrainBaseSpeedFloat = 2.0

	// DrainHomingAccel is acceleration toward cursor in cells/sec² (Q32.32)
	// Higher values = snappier homing, lower = more floaty
	DrainHomingAccelFloat = 3.0

	// DrainDrag is deceleration rate when speed exceeds DrainBaseSpeed (1/sec)
	// Applied proportionally to excess speed for smooth convergence
	DrainDragFloat = 2.0

	// DrainDeflectAngleVar is half-angle of random deflection cone (radians)
	// ±0.35 rad ≈ ±20° spread for visual variety
	DrainDeflectAngleVarFloat = 0.35

	// DrainEnrageThreshold is HP below which drain becomes enraged
	DrainEnrageThreshold = 5
)

// Drain physics
var (
	// Drain physics
	DrainBaseSpeed       = vmath.FromFloat(DrainBaseSpeedFloat)
	DrainHomingAccel     = vmath.FromFloat(DrainHomingAccelFloat)
	DrainDrag            = vmath.FromFloat(DrainDragFloat)
	DrainDeflectAngleVar = vmath.FromFloat(DrainDeflectAngleVarFloat)
)

// Entity collision radii (ellipse semi-axes for overlap detection)
var (
	DrainCollisionRadius = vmath.FromFloat(DrainCollisionRadiusFloat)
)
