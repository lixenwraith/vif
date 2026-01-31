package parameter

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

// ECS & Resources Limits
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

// Spatial Grid Defaults
const (
	// DefaultGridWidth is the default width for the spatial grid
	DefaultGridWidth = 500

	// DefaultGridHeight is the default height for the spatial grid
	DefaultGridHeight = 250
)