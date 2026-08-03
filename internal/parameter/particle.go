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

	// DustGlobalDragFloat - quadratic drag coefficient (1/cell), prevents overshoot: drag scales with speed²
	DustGlobalDragFloat = 0.02

	// DustJitterFloat - constant random velocity added per frame (cells/sec)
	DustJitterFloat = 2.0

	// Timers are the lifetime of each dust type (dark is disabled for now)
	DustTimerDark   = 2 * time.Second
	DustTimerNormal = 4 * time.Second
	DustTimerBright = 8 * time.Second
)