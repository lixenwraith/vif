package constant

import "time"

// Game Loop & Engine Timing
const (
	// FrameUpdateInterval is the rendering frame rate interval (~60 FPS)
	FrameUpdateInterval = 16 * time.Millisecond

	// GameUpdateInterval is the game logic update interval (clock tick)
	GameUpdateInterval = 50 * time.Millisecond

	// EventLoopInterval is the frequency at which events are attempted to be processed
	EventLoopInterval = 4 * time.Millisecond

	// EventLoopBackoffMax is the maximum number of intervals that failure to acquire lock is tolerated (deferred to next event tick)
	EventLoopBackoffMax = 2

	// EventLoopIterations is the cycles event loop attempts to consume events for immediate settling
	EventLoopIterations = 16
)

// ECS & Resource Limits
const (
	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 2048

	// EventBufferMask is the bitmask for fast modulo operations (2048 - 1)
	EventBufferMask = 2047
)

// MaxEntitiesPerCell set to 31 to ensure the Cell struct fits exactly into 256 bytes
// (4 cache lines) when Entity is uint64 (8 bytes)
// 31 * 8 (Entities) + 1 (Count) + 7 (Padding) = 256 bytes
const MaxEntitiesPerCell = 31

// System Execution Priorities (lower runs first)
const (
	PriorityShield      = 10
	PriorityHeat        = 20
	PriorityEnergy      = 30
	PriorityBoost       = 40
	PriorityTyping      = 50 // After boost, before composite
	PriorityComposite   = 60 // After boost, before spawn (position sync)
	PrioritySpawn       = 70
	PriorityNugget      = 80
	PriorityGold        = 90
	PriorityCleaner     = 100
	PriorityFuse        = 110 // After Cleaner, before Drain
	PrioritySpirit      = 120 // After Fuse, before Drain // TODO: really? or after drain?
	PriorityDrain       = 130
	PriorityMaterialize = 140 // PriorityDrain + 1
	PriorityQuasar      = 150 // After Drain
	PriorityExplosion   = 155 // After Quasar, before Dust
	PriorityDust        = 160 // After Quasar, before Decay
	PriorityDecay       = 170
	PriorityBlossom     = 180
	PriorityLightning   = 190 // After Quasar
	PriorityFlash       = 200
	PriorityUI          = 210
	PriorityEffect      = 300
	PrioritySplash      = 800 // After game logic, before rendering
	PriorityDeath       = 850 // After game logic, before TimeKeeper
	PriorityTimekeeper  = 900 // After game logic, final
)

// Spatial Grid Defaults
const (
	// DefaultGridWidth is the default width for the spatial grid
	DefaultGridWidth = 500

	// DefaultGridHeight is the default height for the spatial grid
	DefaultGridHeight = 250
)