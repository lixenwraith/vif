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
	DrainEnergyDrainAmount = 10
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
	// MaterializeChar is the character used for spawn animation blocks
	MaterializeChar = '█'

	// MaterializeAnimationDuration is the time for spawners to converge
	MaterializeAnimationDuration = 1 * time.Second

	// MaterializeTrailLength is the number of trail positions for fade effect
	MaterializeTrailLength = 8
)

// --- Shield Entity ---
const (
	ShieldRadiusX    = 10.5
	ShieldRadiusY    = 5.5
	ShieldMaxOpacity = 0.3
)

// --- Splash Entity ---
const (
	SplashCharWidth   = 16
	SplashCharHeight  = 12
	SplashCharSpacing = 1
	SplashMaxLength   = 8
	SplashDuration    = 1 * time.Second

	// SplashTimerPadding is the vertical padding between gold timer and sequence
	SplashTimerPadding = 2
)

// --- Global Visual Effects ---
const (
	// DestructionFlashDuration is how long the destruction flash effect lasts in milliseconds
	// Used for drain collision, decay terminal, cleaner sweep
	DestructionFlashDuration = 500 * time.Millisecond
)