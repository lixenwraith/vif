package parameter

import (
	"time"
)

// Falling particles (decay and blossom behaviors)
const (
	// ParticleMinSpeed is the minimum initial particle velocity in cells per second
	ParticleMinSpeed = 8.0
	// ParticleMaxSpeed is the maximum initial particle velocity in cells per second
	ParticleMaxSpeed = 15.0
	// ParticleAcceleration is acceleration (velocity increase) per second
	ParticleAcceleration = 2.0
	// ParticleChangeChance is the chance of character change of particles when moving from one cell to next (Matrix-style char swap probability)
	ParticleChangeChance = 0.4
)

// Dust particles orbiting the local cursor
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

	// Lifetime of dust by glyph level. Requested dust skips dark glyphs, so dark dust
	// is special fire's conversion of dark glyphs or the last fade step of brighter dust
	DustTimerDark   = 2 * time.Second
	DustTimerNormal = 4 * time.Second
	DustTimerBright = 8 * time.Second

	// DustWallRestitution is velocity retained on wall/boundary bounce
	DustWallRestitution = 0.5
)

// Decay particles orbiting a spinning or charging Kraken
const (
	// DecayOrbitStiffness pulls toward the orbit radius (1/s²); it keeps pace with a charge
	DecayOrbitStiffness = 30.0
	// DecayOrbitRadiusMin/Max sit inside the leg span, where the legs have cleared walls (cells)
	DecayOrbitRadiusMin = 12.0
	DecayOrbitRadiusMax = 26.0
	// DecayOrbitDamping circularizes the orbit (1/sec)
	DecayOrbitDamping = 2.0
	// DecayOrbitSpeed is the tangential speed a particle takes on entering orbit (cells/sec)
	DecayOrbitSpeed = 40.0
	// DecayOrbitDrag is quadratic drag while orbiting (1/cell); a falling particle has none
	DecayOrbitDrag = 0.005
)
