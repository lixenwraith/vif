package parameter

import "time"

// Time domain convention: durations in this package are game time and dilate
// with the simulation rate. Wall-clock durations are marked [wall].

// Game Loop & Engine Timing
const (
	// FrameUpdateInterval is the rendering frame rate interval (~60 FPS)
	FrameUpdateInterval = 16 * time.Millisecond

	// GameUpdateInterval is the game logic update interval (clock tick) [game]
	GameUpdateInterval = 50 * time.Millisecond

	// EventLoopInterval is the frequency at which events are attempted to be processed
	EventLoopInterval = 4 * time.Millisecond

	// InputTickInterval drives the router input tick: auto-fire, macro playback, etc.
	InputTickInterval = 16 * time.Millisecond

	// PausedPollInterval is the scheduler's wall-clock poll period while paused
	PausedPollInterval = 50 * time.Millisecond

	// StepBurstMax caps the ticks one :step request may advance
	StepBurstMax = 10000

	// StepRunMaxTicks is the tick budget a run-until request spends before self-disarming
	StepRunMaxTicks = 20000

	// EventLoopBackoffMax is the maximum number of intervals that failure to acquire lock is tolerated (deferred to next event tick)
	EventLoopBackoffMax = 2

	// EventLoopIterations is the cycles event loop attempts to consume events for immediate settling
	EventLoopIterations = 16

	// StatSnapshotTicks is the game-tick period between status snapshots; 0 disables.
	// The flight recorder holds fine-grained history, so the periodic snapshot
	// is a coarse heartbeat: 200 = 0.1 Hz at a 50ms tick
	StatSnapshotTicks = 200

	// RecorderDepthTicks is the flight-recorder ring depth in game ticks; 0 disables
	RecorderDepthTicks = 200

	// DevDrainInterval is the poll period for captured stderr in dev mode
	DevDrainInterval = 500 * time.Millisecond
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
// 31 * 8 (Entities) + 1 (Count) + 1 (SharedCount) + 6 (Padding) = 256 bytes
const MaxEntitiesPerCell = 31

// ReservedPlayerPerCell caps the player half of a cell so a pile of local effects
// can never consume the slots a shared entity needs.
const ReservedPlayerPerCell = 12

// Spatial Grid Defaults
const (
	// DefaultGridWidth is the default width for the spatial grid
	DefaultGridWidth = 500

	// DefaultGridHeight is the default height for the spatial grid
	DefaultGridHeight = 250
)
