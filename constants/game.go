package constants

import "time"

// Game Entity
const (
	MaxEntities = 200
)

// System Priorities (lower runs first)
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

// Spawn Exclusion Zone
const (
	// CursorExclusionX is horizontal distance from cursor that blocks spawn
	CursorExclusionX = 5
	// CursorExclusionY is vertical distance from cursor that blocks spawn
	CursorExclusionY = 3
)

// Spawn System
const (
	SpawnIntervalMs         = 2000
	MinBlockLines           = 3
	MaxBlockLines           = 15
	MaxPlacementTries       = 3
	MinIndentChange         = 2
	ContentRefreshThreshold = 0.8
)

// Nugget System
const (
	NuggetSpawnIntervalSeconds = 5
	NuggetMaxAttempts          = 100
)

// Game Loop Timing
const (
	// FrameUpdateInterval is the rendering frame rate interval (~60 FPS)
	FrameUpdateInterval = 16 * time.Millisecond

	// GameUpdateInterval is the game logic update interval (clock tick)
	GameUpdateInterval = 50 * time.Millisecond

	// InitialSpawnDelay is the delay before first character spawn
	InitialSpawnDelay = 2 * time.Second
)

// Event Queue
const (
	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 256

	// EventBufferMask is the bitmask for fast modulo operations (256 - 1)
	EventBufferMask = 255
)

// Game Mechanics
const (
	// MaxHeat is the maximum value for the heat meter (100%)
	MaxHeat = 100

	// NuggetHeatIncrease is the amount of heat increased by consuming a nugget
	NuggetHeatIncrease = 10
)

// Cleaner System
const (
	// CleanerDeduplicationWindow is the number of frames to keep in the spawned map
	// for preventing duplicate cleaner spawns from the same event
	CleanerDeduplicationWindow = 30
)

// Shield Constants
const (
	ShieldRadiusX    = 10.0
	ShieldRadiusY    = 5.0
	ShieldMaxOpacity = 0.6
)