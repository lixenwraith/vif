package constants

import "time"

// Game Loop & Engine Timing
const (
	// FrameUpdateInterval is the rendering frame rate interval (~60 FPS)
	FrameUpdateInterval = 16 * time.Millisecond

	// GameUpdateInterval is the game logic update interval (clock tick)
	GameUpdateInterval = 50 * time.Millisecond

	// InitialSpawnDelay is the delay before first character spawn
	InitialSpawnDelay = 2 * time.Second
)

// ECS & Resource Limits
const (
	// MaxEntities is the hard limit for the Entity Component System
	MaxEntities = 200

	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 256

	// EventBufferMask is the bitmask for fast modulo operations (256 - 1)
	EventBufferMask = 255
)

// System Execution Priorities (lower runs first)
const (
	PriorityBoost   = 5
	PriorityEnergy  = 10
	PrioritySpawn   = 15
	PriorityNugget  = 18
	PriorityGold    = 20
	PriorityCleaner = 22
	PriorityDrain   = 25
	PriorityDecay   = 30
	PriorityFlash   = 35
)
