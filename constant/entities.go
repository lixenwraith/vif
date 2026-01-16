package constant

import (
	"time"
)

// --- Cleaner Entity ---
const (
	// CleanerChar is the character used to render the cleaner block
	CleanerChar = '█'

	// CleanerAnimationDuration is the total time for a cleaner to sweep across the screen
	CleanerAnimationDuration = 1000 * time.Millisecond

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10
)

// --- Drain Entity ---
const (
	// DrainChar is the character used to render the drain entity (╬ - Unicode U+256C)
	DrainChar = '╬'

	// DrainEnergyDrainInterval is the duration between energy drain ticks
	DrainEnergyDrainInterval = 1000 * time.Millisecond

	// DrainBaseSpeed is the normal homing velocity in cells/sec (Q32.32 via vmath.FromFloat)
	// Equivalent to previous 1 cell per DrainMoveInterval
	DrainBaseSpeedFloat = 1.0

	// DrainHomingAccel is acceleration toward cursor in cells/sec² (Q32.32)
	// Higher values = snappier homing, lower = more floaty
	DrainHomingAccelFloat = 3.0

	// DrainDrag is deceleration rate when speed exceeds DrainBaseSpeed (1/sec)
	// Applied proportionally to excess speed for smooth convergence
	DrainDragFloat = 2.0

	// DrainDeflectImpulse is velocity magnitude added on cleaner collision (cells/sec)
	// Determines how far drain travels before homing pulls it back
	DrainDeflectImpulseFloat = 8.0

	// DrainDeflectAngleVar is half-angle of random deflection cone (radians)
	// ±0.35 rad ≈ ±20° spread for visual variety
	DrainDeflectAngleVarFloat = 0.35

	// DrainDeflectImmunity is the duration of immunity from homing/drag after cleaner collision
	DrainDeflectImmunity = 350 * time.Millisecond

	// DrainDeflectImpulseMinFloat is minimum deflection velocity (cells/sec)
	DrainDeflectImpulseMinFloat = 15.0

	// DrainDeflectImpulseMaxFloat is maximum deflection velocity (cells/sec)
	DrainDeflectImpulseMaxFloat = 40.0
)

// --- Quasar Entity ---
const (
	// QuasarWidth is the horizontal cell count
	QuasarWidth = 5
	// QuasarHeight is the vertical cell count
	QuasarHeight = 3

	// QuasarShieldDrain is energy drained per tick when any part overlaps shield
	QuasarShieldDrain = 1000
	// QuasarAnchorOffsetX is phantom head X offset from top-left (center column)
	QuasarAnchorOffsetX = 2
	// QuasarAnchorOffsetY is phantom head Y offset from top-left (center row)
	QuasarAnchorOffsetY = 1

	// QuasarSpeedIncreaseTicks
	QuasarSpeedIncreaseTicks = 20

	// QuasarSpeedIncreasePercent is the speed multiplier increase per move (10% = 0.10)
	// Applied as: newSpeed = oldSpeed * (1.0 + QuasarSpeedIncreasePercent)
	QuasarSpeedIncreasePercent = 0.10

	// QuasarZapDuration is the visual duration for zap lightning effect
	// SetPosition long since it's continuously refreshed while zapping
	QuasarZapDuration = 500 * time.Millisecond

	// QuasarDeflectImpulseMinFloat is minimum deflection velocity (cells/sec)
	// Lower than drain due to mass ratio reduction
	QuasarDeflectImpulseMinFloat = 15.0

	// QuasarDeflectImpulseMaxFloat is maximum deflection velocity (cells/sec)
	QuasarDeflectImpulseMaxFloat = 40.0

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

	// QuasarInitialHP is starting hit points
	QuasarInitialHP = 100

	// QuasarHitFlashDuration is yellow flash duration and immunity window on cleaner hit
	QuasarHitFlashDuration = 150 * time.Millisecond
)

// --- Quasar Visual ---
const (
	// QuasarZapBorderWidthCells defines target visual width of zap adaptive range border
	QuasarZapBorderWidthCells = 1.5
	// QuasarBorderPaddingCells is the padding to ensure continuous visual border in small window sizes
	QuasarBorderPaddingCells = 2
	// QuasarShieldPad256X is horizontal cell padding for 256-color solid rim
	QuasarShieldPad256X = 2
	// QuasarShieldPad256Y is vertical cell padding for 256-color solid rim
	QuasarShieldPad256Y = 1
	// QuasarShieldPadTCX is horizontal cell padding for TrueColor gradient
	QuasarShieldPadTCX = 4
	// QuasarShieldPadTCY is vertical cell padding for TrueColor gradient
	QuasarShieldPadTCY = 2
	// QuasarShieldMaxOpacity is peak alpha at ellipse edge (TrueColor)
	QuasarShieldMaxOpacity = 0.3
	// QuasarShield256Palette is xterm-256 index for solid rim (light gray)
	QuasarShield256Palette uint8 = 250
)

// --- Swarm Entity ---
const (
	// SwarmEnergyDrainInterval is the duration between energy drain ticks
	SwarmEnergyDrainInterval = 1000 * time.Millisecond

	// SwarmBaseSpeed is the normal homing velocity in cells/sec (Q32.32 via vmath.FromFloat)
	// Equivalent to previous 1 cell per SwarmMoveInterval
	SwarmBaseSpeedFloat = 2.0

	// SwarmHomingAccel is acceleration toward cursor in cells/sec² (Q32.32)
	// Higher values = snappier homing, lower = more floaty
	SwarmHomingAccelFloat = 3.0

	// SwarmDrag is deceleration rate when speed exceeds SwarmBaseSpeed (1/sec)
	// Applied proportionally to excess speed for smooth convergence
	SwarmDragFloat = 2.0

	// SwarmDeflectImpulse is velocity magnitude added on cleaner collision (cells/sec)
	// Determines how far drain travels before homing pulls it back
	SwarmDeflectImpulseFloat = 2.0

	// SwarmDeflectAngleVar is half-angle of random deflection cone (radians)
	// ±0.35 rad ≈ ±20° spread for visual variety
	SwarmDeflectAngleVarFloat = 0.45

	// SwarmDeflectImmunity is the duration of immunity from homing/drag after cleaner collision
	SwarmDeflectImmunity = 2000 * time.Millisecond

	// SwarmDeflectImpulseMinFloat is minimum deflection velocity (cells/sec)
	SwarmDeflectImpulseMinFloat = 15.0

	// SwarmDeflectImpulseMaxFloat is maximum deflection velocity (cells/sec)
	SwarmDeflectImpulseMaxFloat = 40.0
)

// --- Shield Knockback ---
const (
	// ShieldKnockbackImmunity is duration of immunity after shield knockback
	ShieldKnockbackImmunity = 350 * time.Millisecond

	// ShieldKnockbackImpulseMinFloat is minimum knockback velocity (cells/sec)
	ShieldKnockbackImpulseMinFloat = 15.0

	// ShieldKnockbackImpulseMaxFloat is maximum knockback velocity (cells/sec)
	ShieldKnockbackImpulseMaxFloat = 40.0
)

// --- Decay / Blossom Entities ---
const (
	// ParticleMinSpeed is minimum initial cell per second velocity of decay/blossom components
	ParticleMinSpeed = 8.0
	// ParticleMaxSpeed is maximum initial cell per second velocity of decay/blossom components
	ParticleMaxSpeed = 15.0
	// ParticleAcceleration is acceleration (velocity increase) per second
	ParticleAcceleration = 2.0
	// ParticleChangeChance is the chance of character change of particles when moving from one cell to next (Matrix-style char swap probability)
	ParticleChangeChance = 0.4
)

// --- Materialization Effect ---
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

// --- Lightning Entity ---
const (
	LightningAlpha = 0.8
)

// --- Spirit Entity ---
const (
	// SpiritAnimationDuration is the time for spirits to converge to target
	SpiritAnimationDuration = 500 * time.Millisecond

	// SpiritSafetyBuffer is additional time before safety despawn to allow final frame render
	SpiritSafetyBuffer = 100 * time.Millisecond

	// SpiritBlinkHz is the color oscillation frequency during travel
	SpiritBlinkHz = 12
)

// --- Dust Entity ---
const (
	// DustAttractionBaseFloat is orbital attraction strength (cells/sec²)
	DustAttractionBaseFloat = 60.0

	// DustOrbitRadiusMinFloat/MaxFloat for varied orbital radii (cells)
	DustOrbitRadiusMinFloat = 3.0
	DustOrbitRadiusMaxFloat = 10.0

	// DustDampingFloat for orbit circularization (1/sec)
	DustDampingFloat = 2.0

	// DustChaseBoostFloat - attraction multiplier on large cursor movement
	DustChaseBoostFloat = 3.0

	// DustChaseThreshold - cursor delta (cells) triggering chase boost and jitter
	DustChaseThreshold = 3

	// DustChaseDecayFloat - boost decay rate (1/sec)
	DustChaseDecayFloat = 4.0

	// DustInitialSpeedFloat - tangential velocity magnitude at spawn (cells/sec)
	DustInitialSpeedFloat = 32.0

	// DustGlobalDragFloat - quadratic drag coefficient (1/cell)
	// Prevents overshoot: drag scales with speed²
	DustGlobalDragFloat = 0.02

	// DustJitterFloat - constant random velocity added per frame (cells/sec)
	DustJitterFloat = 2.0
)

// --- Explosion Field VFX ---
const (
	// ExplosionFieldRadiusFloat is visual radius in cells (aspect-corrected)
	ExplosionFieldRadiusFloat = 12.0

	// ExplosionFieldDuration is total fade time
	ExplosionFieldDuration = 300 * time.Millisecond

	// ExplosionCenterCap is maximum concurrent explosion centers
	ExplosionCenterCap = 256

	// ExplosionMergeThresholdFloat is distance for center merging (cells)
	ExplosionMergeThresholdFloat = 2.0

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

// --- Shield Entity ---
const (
	ShieldRadiusX    = 10
	ShieldRadiusY    = 5
	ShieldMaxOpacity = 0.3
)

// --- Splash Entity ---
const (
	SplashCharWidth  = 12
	SplashCharHeight = 12
	SplashMaxLength  = 8
	SplashDuration   = 200 * time.Millisecond

	// SplashTimerPadding is the vertical padding between timer and anchor
	SplashTimerPadding = 1

	// SplashTopPadding is adjustment for splash displayed on top of an anchor to account for vertical asymmetry of empty lines above and below splash font (1 top, 2 bottom)
	SplashTopPadding = -1

	// SplashCollisionPadding is the cell padding between different splashes to prevent overcrowding
	SplashCollisionPadding = 2
)

// --- Global Visual Effects ---
const (
	// DestructionFlashDuration is how long the destruction flash effect lasts in milliseconds
	// Used for drain collision, decay terminal, cleaner sweep
	DestructionFlashDuration = 500 * time.Millisecond
)