package parameter

import (
	"time"
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

// Swarm Entity
const (
	// SwarmWidth is horizontal cell count
	SwarmWidth = 4
	// SwarmHeight is vertical cell count
	SwarmHeight = 2

	// SwarmHeaderOffsetX is phantom head X offset from top-left
	SwarmHeaderOffsetX = 1
	// SwarmHeaderOffsetY is phantom head Y offset from top-left
	SwarmHeaderOffsetY = 0

	// SwarmPatternCount is number of visual patterns
	SwarmPatternCount = 2
	// SwarmPatternDuration is time per pattern before cycling
	SwarmPatternDuration = 500 * time.Millisecond

	// SwarmChargeInterval is time between charge sequences
	SwarmChargeInterval = 5 * time.Second
	// SwarmLockDuration is freeze time before charge
	SwarmLockDuration = 2 * time.Second
	// SwarmChargeDuration is charge travel time (fixed, speed calculated from distance)
	SwarmChargeDuration = 800 * time.Millisecond
	// SwarmDecelerationDuration is rapid stop after charge
	SwarmDecelerationDuration = 100 * time.Millisecond

	// Swarm Charge Line (warning pulse before charge)
	// Number of visual pulses during lock phase (3rd = actual charge)
	SwarmChargeLinePulseCount = 2
	// Delay before first pulse = LockDuration - PulseCount * ChargeDuration
	// Negative value means pulses extend before lock (clamp to 0 at runtime)
	SwarmChargeLineShowDelay = SwarmLockDuration - SwarmChargeLinePulseCount*SwarmChargeDuration
	// Trail fade length as fraction of total line distance
	SwarmChargeLineTrailFloat = 0.25
	// Peak bg alpha for first pulse
	SwarmChargeLineAlpha1Float = 0.25
	// Peak bg alpha for second pulse (escalation)
	SwarmChargeLineAlpha2Float = 0.40
	// 256-color visibility threshold
	SwarmChargeLine256Threshold = 0.15

	// SwarmLifetime is maximum swarm lifespan
	SwarmLifetime = 35 * time.Second
	// SwarmMaxCharges is charge count before despawn
	SwarmMaxCharges = 5

	// SwarmChaseSpeedMultiplier relative to drain base speed
	SwarmChaseSpeedMultiplier = 4

	// SwarmFuseAnimationDuration matches spirit convergence timing
	SwarmFuseAnimationDuration = 500 * time.Millisecond

	// SwarmHomingAccelFloat is acceleration toward cursor (cells/sec²)
	SwarmHomingAccelFloat = 6.0
	// SwarmDragFloat is deceleration when overspeed (1/sec)
	SwarmDragFloat = 2.0
	// SwarmDeflectAngleVarFloat is half-angle of random deflection cone (radians)
	SwarmDeflectAngleVarFloat = 0.25
)

// Swarm Teleport
const (
	// SwarmTeleportDuration is visual effect duration before instant move
	SwarmTeleportDuration = 400 * time.Millisecond

	// SwarmTeleportBeamAlphaFloat is peak beam opacity
	SwarmTeleportBeamAlphaFloat = 0.5
	// SwarmTeleportBeamTrailFloat is trail length fraction
	SwarmTeleportBeamTrailFloat = 0.3
	// SwarmTeleport256Threshold for 256-color visibility
	SwarmTeleport256Threshold = 0.15
)