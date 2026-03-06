package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// Quasar Entity
const (
	// QuasarWidth is the horizontal cell count
	QuasarWidth = 5
	// QuasarHeight is the vertical cell count
	QuasarHeight = 3

	// QuasarShieldDrain is energy drained per tick when any part overlaps shield
	QuasarShieldDrain = 1000
	// QuasarHeaderOffsetX is phantom head X offset from top-left (center column)
	QuasarHeaderOffsetX = 2
	// QuasarHeaderOffsetY is phantom head Y offset from top-left (center row)
	QuasarHeaderOffsetY = 1

	// QuasarSpeedIncreaseTicks
	QuasarSpeedIncreaseTicks = 20

	// QuasarSpeedIncreasePercent is the speed multiplier increase per move (10% = 0.10)
	// Applied as: newSpeed = oldSpeed * (1.0 + QuasarSpeedIncreasePercent)
	QuasarSpeedIncreasePercentFloat = 0.10

	// QuasarZapDuration is the visual duration for zap lightning effect
	// Set long since it's continuously refreshed while zapping
	QuasarZapDuration = 500 * time.Millisecond

	// QuasarHomingAccelFloat is acceleration toward cursor (cells/sec²)
	QuasarHomingAccelFloat = 4.0

	// QuasarBaseSpeedFloat is normal homing velocity (cells/sec)
	QuasarBaseSpeedFloat = 2.0

	// QuasarMaxSpeedFloat caps velocity after impulse accumulation (5x base speed)
	QuasarMaxSpeedFloat = QuasarBaseSpeedFloat * 10.0

	// QuasarDragFloat is deceleration when overspeed (1/sec)
	QuasarDragFloat = 1.5

	// QuasarSpeedMultiplierMax caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMax = 10

	// QuasarChargeDuration is the delay before zapping starts when cursor exits range
	QuasarChargeDuration = 3 * time.Second

	QuasarRestitutionFloat = 0.9

	QuasarDamageHeat = 10
)

var (
	QuasarSpeedIncreasePercent = vmath.FromFloat(1.0 + QuasarSpeedIncreasePercentFloat)

	QuasarRestitution = vmath.FromFloat(QuasarRestitutionFloat)
)

// Quasar Visual
const (
	// QuasarZapBorderWidthCells defines target visual width of zap adaptive range border
	QuasarZapBorderWidthCells = 2
	// QuasarBorderPaddingCells is the padding to ensure continuous visual border in small window sizes
	QuasarBorderPaddingCells = 2
	// QuasarShieldPadX is horizontal cell padding
	QuasarShieldPadX = 4
	// QuasarShieldPadY is vertical cell padding
	QuasarShieldPadY = 2
	// QuasarShieldMaxOpacity is peak alpha at ellipse edge (TrueColor)
	QuasarShieldMaxOpacity = 0.3
	// QuasarShield256Palette is xterm-256 index for solid rim (light gray)
	QuasarShield256Palette uint8 = 250
)

// Quasar physics
var (
	// Quasar physics
	QuasarHomingAccel = vmath.FromFloat(QuasarHomingAccelFloat)
	QuasarBaseSpeed   = vmath.FromFloat(QuasarBaseSpeedFloat)
	QuasarMaxSpeed    = vmath.FromFloat(QuasarMaxSpeedFloat)
	QuasarDrag        = vmath.FromFloat(QuasarDragFloat)
	// QuasarSpeedMultiplierMaxFixed caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMaxFixed = vmath.Scale * QuasarSpeedMultiplierMax
)

// Entity collision radii (ellipse semi-axes for overlap detection)
var (
	QuasarCollisionRadiusX = vmath.FromFloat(QuasarCollisionRadiusXFloat)
	QuasarCollisionRadiusY = vmath.FromFloat(QuasarCollisionRadiusYFloat)
)

// Pre-computed inverse squared radii for ellipse overlap checks
var (
	QuasarCollisionInvRxSq, QuasarCollisionInvRySq = vmath.EllipseInvRadiiSq(QuasarCollisionRadiusX, QuasarCollisionRadiusY)
)
