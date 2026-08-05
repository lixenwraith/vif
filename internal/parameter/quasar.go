package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
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
	QuasarSpeedIncreasePercent = 0.10

	// QuasarZapDuration is the visual duration for zap lightning effect
	// Set long since it's continuously refreshed while zapping
	QuasarZapDuration = 500 * time.Millisecond

	// QuasarHomingAccel is acceleration toward cursor (cells/sec²)
	QuasarHomingAccel = 4.0

	// QuasarBaseSpeed is normal homing velocity (cells/sec)
	QuasarBaseSpeed = 2.0

	// QuasarMaxSpeed caps velocity after impulse accumulation (5x base speed)
	QuasarMaxSpeed = QuasarBaseSpeed * 10.0

	// QuasarDrag is deceleration when overspeed (1/sec)
	QuasarDrag = 1.5

	// QuasarSpeedMultiplierMax caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMax = 10

	// QuasarChargeDuration is the delay before zapping starts when cursor exits range
	QuasarChargeDuration = 3 * time.Second

	QuasarRestitution = 0.9

	QuasarDamageHeat = 10
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
	// QuasarSpeedMultiplierMaxFixed caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMaxFixed = vmath.Scale * QuasarSpeedMultiplierMax
)

// Pre-computed inverse squared radii for ellipse overlap checks
var (
	QuasarCollisionInvRxSq, QuasarCollisionInvRySq = vmath.EllipseInvRadiiSqF(QuasarCollisionRadiusX, QuasarCollisionRadiusY)
)
