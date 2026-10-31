package parameter

import (
	"time"

	"github.com/lixenwraith/vif/pkg/position"
)

// Time domain convention: durations in this package are game time and dilate
// with the simulation rate. Wall-clock durations are marked [wall].

// Game Loop & Engine Timing
const (
	// FrameUpdateInterval is the rendering frame interval at the default tick, 60 FPS:
	// a third of a tick, so every tick is drawn for exactly three frames. A shorter
	// tick draws no faster than FrameMinInterval [wall]
	FrameUpdateInterval = GameUpdateInterval / 3
	FrameMinInterval    = time.Second / 120

	// MultiplexerFrameInterval is the frame interval inside tmux or screen [wall].
	// tmux drops a client's output once more than eight bytes a cell back up and
	// from then on repaints it every 100 ms, about 10 frames a second, until a quiet
	// 100 ms that a scrolling playfield never gives. Two vanguard panes at 8x get
	// there at 60 frames a second and at times at 30; at 24 tmux kept up
	MultiplexerFrameInterval = time.Second / 24

	// FrameRateMin and FrameRateMax bound [play] max_fps, frames a second [wall]; 0
	// there leaves the rate to the presentation. A rate under one frame a tick draws
	// one a tick, which a tick at real time waits for
	FrameRateMin = 10
	FrameRateMax = int(time.Second / FrameMinInterval)

	// GameUpdateInterval is the default tick, the game time one tick advances; a run's
	// host may choose another between TickIntervalMin and TickIntervalMax [game]
	GameUpdateInterval = 50 * time.Millisecond
	TickIntervalMin    = 10 * time.Millisecond
	TickIntervalMax    = 100 * time.Millisecond

	// MaxSimulationDeltaSeconds caps continuous integration after a delayed live
	// tick or a configured tick-interval change.
	MaxSimulationDeltaSeconds = 0.1

	// EventLoopInterval is how long the event loop defers a settle the world lock refused
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

	// EventLoopBackoffMax is how many misses the event loop defers before it blocks on the lock
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

// FrameRateSteps are the max_fps values the configuration menu steps through, 0
// first for the presentation's own; tmux's 24, a tick's 20, then every third of one
var FrameRateSteps = []int{0, 20, 24, 30, 60, FrameRateMax}

// ECS & Resources Limits
const (
	// EventQueueSize is the fixed capacity of the event ring buffer
	EventQueueSize = 2048

	// EventBufferMask is the bitmask for fast modulo operations (2048 - 1)
	EventBufferMask = 2047
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

// A cell's capacity, the player half's budget and the map ceiling are the spatial
// index's (pkg/position), named here as the game reads them.
const (
	MaxEntitiesPerCell    = position.CellCapacity
	ReservedPlayerPerCell = position.PrivateBudget
	MaxMapWidth           = position.MaxWidth
	MaxMapHeight          = position.MaxHeight
	MaxMapCells           = position.MaxCells
)
