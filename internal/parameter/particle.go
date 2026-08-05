package parameter

import (
	"time"
)

// Decay / Blossom Entities
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

// Dust Entity
const (
	// DustAttractionBase is orbital attraction strength (cells/sec²)
	DustAttractionBase = 60.0

	// DustOrbitRadiusMin/Max for varied orbital radii (cells)
	DustOrbitRadiusMin = 3.0
	DustOrbitRadiusMax = 10.0

	// DustDamping for orbit circularization (1/sec)
	DustDamping = 2.0

	// DustChaseBoost - attraction multiplier on large cursor movement
	DustChaseBoost = 3.0

	// DustChaseThreshold - cursor delta (cells) triggering chase boost and jitter
	DustChaseThreshold = 3

	// DustChaseDecay - boost decay rate (1/sec)
	DustChaseDecay = 4.0

	// DustInitialSpeed - tangential velocity magnitude at spawn (cells/sec)
	DustInitialSpeed = 32.0

	// DustGlobalDrag - quadratic drag coefficient (1/cell), prevents overshoot: drag scales with speed²
	DustGlobalDrag = 0.02

	// DustJitter - constant random velocity added per frame (cells/sec)
	DustJitter = 2.0

	// Timers are the lifetime of each dust type (dark is disabled for now)
	DustTimerDark   = 2 * time.Second
	DustTimerNormal = 4 * time.Second
	DustTimerBright = 8 * time.Second

	// DustWallRestitution is velocity retained on wall/boundary bounce
	DustWallRestitution = 0.5
)
