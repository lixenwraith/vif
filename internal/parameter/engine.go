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

	// MaxSimulationDeltaSeconds caps continuous integration after a delayed live
	// tick or a configured tick-interval change.
	MaxSimulationDeltaSeconds = 0.1

	// EventLoopInterval is the frequency at which events are attempted to be processed
	EventLoopInterval = 4 * time.Millisecond

	// InputTickInterval drives the router input tick: auto-fire, macro playback, etc.
	InputTickInterval = 16 * time.Millisecond

	// ProbeStallInterval is how long a running scheduler may leave the tick
	// counter unmoved before a liveness probe calls the run stalled: forty ticks,
	// past any ordinary slip, short enough that a wedged loop is restarted.
	ProbeStallInterval = 2 * time.Second

	// PausedPollInterval is the scheduler's wall-clock poll period while paused
	PausedPollInterval = 50 * time.Millisecond

	// StepBurstMax caps the ticks one :step request may advance
	StepBurstMax = 10000

	// StepRunMaxTicks is the tick budget a run-until request spends before self-disarming
	StepRunMaxTicks = 20000

	// EventLoopBackoffMax is how many intervals a failed lock acquisition may defer
	EventLoopBackoffMax = 2

	// EventLoopIterations is the cycles event loop attempts to consume events for immediate settling
	EventLoopIterations = 16

	// StatSnapshotTicks is the game-tick period between status snapshots; 0 disables.
	// A coarse heartbeat beside the flight recorder: 0.1 Hz at a 50ms tick.
	StatSnapshotTicks = 200

	// RecorderDepthTicks is the flight-recorder ring depth in game ticks; 0 disables
	RecorderDepthTicks = 200

	// DevDrainInterval is the poll period for captured stderr in dev mode
	DevDrainInterval = 500 * time.Millisecond

	// ProfWindow is the profiler's averaging window; windows close on ticks [wall]
	ProfWindow = time.Second

	// ProfTopN is how many modules the prof.top card ranks
	ProfTopN = 10

	// ProfCaptureDefault/ProfCaptureMax bound a timed :d cpu or :d trace [wall]
	ProfCaptureDefault = 10 * time.Second
	ProfCaptureMax     = 5 * time.Minute

	// ReplayBackSpares is how many replay copies trail the presented one, a tick
	// apart, so that many step backs present at once; each costs a world and its
	// share of the simulation while playback runs
	ReplayBackSpares = 8

	// ReplayCheckpointSteps is the presented steps between the whole-state
	// checkpoints a replay keeps, so a copy behind its spares replays from the
	// nearest instead of the stream's start. ReplayCheckpoints bounds how many are
	// kept: past it every other is dropped and the spacing doubles. One holds about
	// 6.5 MB at the map limit.
	ReplayCheckpointSteps = 100
	ReplayCheckpoints     = 32
)

// ECS & Resources Limits
const (
	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 2048

	// EventBufferMask is the bitmask for fast modulo operations (2048 - 1)
	EventBufferMask = 2047
)

// MaxEntitiesPerCell keeps a Cell at exactly 256 bytes, four cache lines:
// 31 eight-byte entities, two counts and six bytes of padding.
const MaxEntitiesPerCell = 31

// ReservedPlayerPerCell caps the player half of a cell so a pile of local effects
// can never consume the slots a shared entity needs.
const ReservedPlayerPerCell = 12

// Map bounds, a clamp rather than a rejection: LevelSetup is replicated, so every
// participant must reach the same bounds, and the product reaches make(). A grid
// is sized to its map at 256 bytes a cell, so the ceiling costs 32 MB per world.
const (
	// MaxMapWidth is the widest simulation map, in cells.
	MaxMapWidth = 2000

	// MaxMapHeight is the tallest simulation map, in cells.
	MaxMapHeight = 2000

	// MaxMapCells is the most cells a map may hold, whatever its shape.
	MaxMapCells = 500 * 250
)
