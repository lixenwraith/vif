// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package constant

import "time"

// --- Cleaner Entity ---
const (
	// CleanerChar is the character used to render the cleaner block
	CleanerChar = '█'

	// CleanerAnimationDuration is the total time for a cleaner to sweep across the screen
	CleanerAnimationDuration = 1.0 * time.Second

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10

	// CleanerDeduplicationWindow is the number of frames to prevent duplicate spawns
	CleanerDeduplicationWindow = 30
)

// --- Drain Entity ---
const (
	// DrainChar is the character used to render the drain entity (╬ - Unicode U+256C)
	DrainChar = '╬'

	// DrainMoveInterval is the duration between drain movement updates
	DrainMoveInterval = 1000 * time.Millisecond

	// DrainEnergyDrainInterval is the duration between energy drain ticks
	DrainEnergyDrainInterval = 1000 * time.Millisecond

	// DrainEnergyDrainAmount is the amount of energy drained per tick
	DrainEnergyDrainAmount = 100
)

// --- Quasar Entity ---
const (
	// QuasarWidth is the horizontal cell count
	QuasarWidth = 5
	// QuasarHeight is the vertical cell count
	QuasarHeight = 3
	// QuasarMoveInterval is duration between movement updates (2× drain speed)
	QuasarMoveInterval = DrainMoveInterval / 2 // 500ms
	// QuasarShieldDrain is energy drained per tick when any part overlaps shield
	QuasarShieldDrain = 1000
	// QuasarAnchorOffsetX is phantom head X offset from top-left (center column)
	QuasarAnchorOffsetX = 2
	// QuasarAnchorOffsetY is phantom head Y offset from top-left (center row)
	QuasarAnchorOffsetY = 1
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

// --- Spirit Entity ---
const (
	// SpiritAnimationDuration is the time for spirits to converge to target
	SpiritAnimationDuration = 500 * time.Millisecond

	// SpiritSafetyBuffer is additional time before safety despawn to allow final frame render
	SpiritSafetyBuffer = 100 * time.Millisecond

	// SpiritBlinkHz is the color oscillation frequency during travel
	SpiritBlinkHz = 12
)

// --- Shield Entity ---
const (
	ShieldRadiusX    = 10.5
	ShieldRadiusY    = 5.5
	ShieldMaxOpacity = 0.3
)

// --- Splash Entity ---
const (
	SplashCharWidth  = 12
	SplashCharHeight = 12
	SplashMaxLength  = 8
	SplashDuration   = 1 * time.Second

	// SplashTimerPadding is the vertical padding between gold timer and sequence
	SplashTimerPadding = 0
)

// --- Global Visual Effects ---
const (
	// DestructionFlashDuration is how long the destruction flash effect lasts in milliseconds
	// Used for drain collision, decay terminal, cleaner sweep
	DestructionFlashDuration = 500 * time.Millisecond
)