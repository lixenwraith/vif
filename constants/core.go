package constants

import "time"

// Game Loop & Engine Timing
const (
	// FrameUpdateInterval is the rendering frame rate interval (~60 FPS)
	FrameUpdateInterval = 16 * time.Millisecond

	// GameUpdateInterval is the game logic update interval (clock tick)
	GameUpdateInterval = 50 * time.Millisecond
)

// ECS & Resource Limits
const (
	// MaxEntities is the hard limit for the Entity Component System
	MaxEntities = 20000

	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 256

	// EventBufferMask is the bitmask for fast modulo operations (256 - 1)
	EventBufferMask = 255
)

// MaxEntitiesPerCell set to 31 to ensure the Cell struct fits exactly into 256 bytes
// (4 cache lines) when Entity is uint64 (8 bytes)
// 31 * 8 (Entities) + 1 (Count) + 7 (Padding) = 256 bytes
const MaxEntitiesPerCell = 31

// System Execution Priorities (lower runs first)
const (
	PriorityBoost   = 5
	PriorityShield  = 6
	PriorityHeat    = 8
	PriorityEnergy  = 10
	PrioritySpawn   = 15
	PriorityNugget  = 18
	PriorityGold    = 20
	PriorityCleaner = 22
	PriorityDrain   = 25
	PriorityDecay   = 30
	PriorityFlash   = 35
	PriorityUI      = 50
	PrioritySplash  = 800 // After game logic, before rendering
	PriorityCleanup = 900 // Last: Removes marked entities
)

// Spatial Grid Defaults
const (
	// DefaultGridWidth is the default width for the spatial grid
	DefaultGridWidth = 200

	// DefaultGridHeight is the default height for the spatial grid
	DefaultGridHeight = 60
)