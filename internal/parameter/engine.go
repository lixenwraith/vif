package parameter

import "time"

// Time domain convention: durations in this package are game time and dilate
// with the simulation rate. Wall-clock durations are marked [wall].

// Game Loop & Engine Timing
const (
	// FrameUpdateInterval is the rendering frame interval at the default tick, 60 FPS:
	// a third of a tick, so every tick is drawn for exactly three frames. A shorter
	// tick draws no faster than FrameMinInterval [wall]
	FrameUpdateInterval = GameUpdateInterval / 3
	FrameMinInterval    = time.Second / 120

	// GameUpdateInterval is the default tick, the game time one tick advances; a run's
	// host may choose another between TickIntervalMin and TickIntervalMax [game]
	GameUpdateInterval = 50 * time.Millisecond
	TickIntervalMin    = 10 * time.Millisecond
	TickIntervalMax    = 100 * time.Millisecond

	// MaxSimulationDeltaSeconds caps continuous integration after a delayed live
	// tick or a configured tick-interval change.
	MaxSimulationDeltaSeconds = 0.1

	// EventLoopInterval is the frequency at which events are attempted to be processed
	EventLoopInterval = 4 * time.Millisecond

	// InputTickInterval drives the router input tick: auto-fire, macro playback, etc.
	InputTickInterval = 16 * time.Millisecond

	// TerminalBlockedAfter is how long the terminal may hold one write before the
	// frame loop stops drawing for it: past any write a reading terminal takes, so
	// only one that stopped reading (a hidden tab, a gone client) crosses it [wall]
	TerminalBlockedAfter = 100 * time.Millisecond

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

	// ReplayRing is how many of a replay's last steps keep a whole-state checkpoint,
	// so a step back restores one instead of replaying to it
	ReplayRing = 8

	// ReplayCheckpointSteps is the steps between the checkpoints a replay keeps past
	// its ring, so a move further back replays from the nearest instead of the
	// stream's start. ReplayCheckpoints bounds how many are kept: past it every other
	// is dropped and the spacing doubles. One holds about 6.5 MB at the map limit.
	ReplayCheckpointSteps = 100
	ReplayCheckpoints     = 32

	// LockHoldWarn is the world-lock hold above which a hold is reported [wall];
	// a race build instruments every access, so it reads LockHoldWarnRace
	LockHoldWarn     = 20 * time.Millisecond
	LockHoldWarnRace = 200 * time.Millisecond

	// AnchorIntervalTicks is the tick period between journal anchor records, so a
	// rotated file carries one within this many ticks of its first record
	AnchorIntervalTicks = 600

	// DigestIntervalTicks is the tick period between journal world digests: a
	// replay names the tick it left its run within one period, at about 5% of a
	// bot journal
	DigestIntervalTicks = 20

	// JournalLineBytes is the longest journal line a reader takes; an opening one
	// line could not hold is refused when the journal starts, its payloads held to
	// JournalLineHeadroom under it for the record's other fields
	JournalLineBytes    = 8 << 20
	JournalLineHeadroom = 64 << 10
)

// ECS & Resources Limits
const (
	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 2048

	// EventBufferMask is the bitmask for fast modulo operations (2048 - 1)
	EventBufferMask = 2047

	// FreeAreaSearchRadius is the spiral radius FindFreeAreaSpiral walks when a
	// caller names none
	FreeAreaSearchRadius = 20
)

// Store index: an entity's ID selects a slot in a page of 1<<StoreIndexPageBits.
// Pages and page table stay within StoreIndexFloorBytes plus StoreIndexEntityBytes
// per entity indexed; an entity past that bound is kept in a map, so IDs no counter
// issues (a peer's capture) cost what a map costs instead of a page each.
const (
	StoreIndexPageBits    = 10
	StoreIndexFloorBytes  = 64 << 10
	StoreIndexEntityBytes = 256
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
