// Package app: caller-driven harness.
//
// A driven App runs on a ManualClock advanced only by Tick, with no scheduler or
// event goroutine, so a run is a pure function of its seed, its config, and the
// injected event sequence. Headless adds no I/O; replay adds a terminal and renderer
// but takes its geometry from the journal. Close is the caller's responsibility.
//
// Concurrent Apps in one process still share four process-wide values, none of
// which reaches a simulation snapshot: the status recorder trigger hook, the
// navigation debug pointers in internal/system, help's key table, and vlog's
// correlation stamp. Run harness Apps sequentially until those are scoped.
package app

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

// NewHeadless builds and starts a headless runtime: no I/O at all, and no scheduler
// or event goroutine. Tick drives the simulation instead.
func NewHeadless(cfg Config) (*App, error) {
	cfg.Mode = ModeHeadless
	return newDriven(cfg)
}

// NewReplay builds and starts a presenting runtime for a recorded run: terminal and
// renderer, but a manual clock the caller ticks and geometry taken from the journal.
func NewReplay(cfg Config) (*App, error) {
	cfg.Mode = ModeReplay
	return newDriven(cfg)
}

// newDriven wires a caller-driven runtime, sealing the system set and freezing the
// metric set before anything dispatches so a Settle before the first Tick registers nothing
func newDriven(cfg Config) (*App, error) {
	a, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if err := a.hub.StartAll(); err != nil {
		a.Close()
		return nil, err
	}
	a.scheduler.Prepare()
	return a, nil
}

// Tick advances the simulation by n ticks, servicing a pending FSM reset before
// each one. Pause does not gate a stepped tick, so n ticks always execute.
func (a *App) Tick(n int) {
	if n < 0 {
		return
	}
	a.scheduler.RunTicks(n)
}

// Settle dispatches queued events without advancing time
func (a *App) Settle() {
	a.scheduler.Settle()
}

// Inject applies intents at a tick boundary, settling after each so the next one
// observes the world it produced. Returns false once an intent quits the game.
// Intents are the sole injection path: hand-built ones must carry Count >= 1
// where a motion count applies, since no input.Machine normalizes them.
func (a *App) Inject(intents ...*input.Intent) bool {
	for _, intent := range intents {
		before := a.pushed()
		if !a.handleIntent(intent) {
			return false
		}
		// Elide the settle when nothing was emitted: an unrecorded group cannot replay
		if a.pushed() != before {
			a.scheduler.Settle()
		}
	}
	return true
}

// InputTick advances input-driven work: auto-fire, held-button repeat and macro
// playback. Their intervals are game time, so a caller that never Ticks emits at
// most one round. Returns false when a macro intent quits the game.
func (a *App) InputTick() bool {
	before := a.pushed()
	if a.processInputTick() || a.pushed() != before {
		a.scheduler.Settle()
	}

	// Per intent: a macro motion is applied by CursorSystem, so the next one must see it
	for _, intent := range a.router.ProcessMacroTick() {
		before = a.pushed()
		if !a.handleIntent(intent) {
			return false
		}
		if a.pushed() != before {
			a.scheduler.Settle()
		}
	}
	return true
}

// SetupLevel resizes the map independently of the viewport; cropOnResize=false
// decouples them, so a headless run emulates either mode.
func (a *App) SetupLevel(width, height int, clearEntities, cropOnResize bool) {
	a.ctx.PushEventOrigin(event.EventLevelSetup, &event.LevelSetupPayload{
		Width:         width,
		Height:        height,
		ClearEntities: clearEntities,
		CropOnResize:  cropOnResize,
	}, event.OriginDebug)
	a.scheduler.Settle()
}

// Resize records a terminal dimension change and settles the reflow. Headless has no
// terminal, so this is its only resize path; a live run records the same event from
// App.Loop, which is what makes a resize replayable.
func (a *App) Resize(width, height int) {
	a.ctx.PushEventOrigin(event.EventScreenResize,
		&event.ScreenResizePayload{Width: width, Height: height}, event.OriginDebug)
	a.scheduler.Settle()
}

// Reset requests a new game; purge additionally clears operator session state.
// MetaSystem's synchronous cleanup lands here, the FSM reset at the next Tick,
// matching the interactive ordering.
func (a *App) Reset(purge bool) {
	a.ctx.PushEventOrigin(event.EventGameResetRequest, &event.GameResetPayload{Purge: purge}, event.OriginDebug)
	a.scheduler.Settle()
}

// Context returns the game context, for assertions the snapshot does not carry
func (a *App) Context() *engine.GameContext { return a.ctx }

// World returns the ECS world
func (a *App) World() *engine.World { return a.world }

// Seed returns the root seed the run resolved, including a drawn one
func (a *App) Seed() uint64 { return a.cfg.Seed }

// Position returns the journal stamp the run is at: reset generation, ticks within
// it, and settle groups within the tick
func (a *App) Position() event.Stamp { return a.world.Resources.Event.Queue.Stamp() }

// pushed returns the queue's total push count, for settle elision
func (a *App) pushed() uint64 { return a.world.Resources.Event.Queue.Pushed() }

// JournalStats returns the emitted record count and encode failure count;
// zero when journaling is off
func (a *App) JournalStats() (emitted, encodeFailed uint64) {
	return a.world.Resources.Event.Queue.Journal().Stats()
}

// Region applies an FSM region operation and settles what it emits.
// State is required by event.RegionSpawn and ignored otherwise.
func (a *App) Region(op, region, state string) {
	a.ctx.PushEventOrigin(event.EventFSMRegionRequest, &event.FSMRegionPayload{
		Op: op, Region: region, State: state,
	}, event.OriginDebug)
	a.scheduler.Settle()
}

// SetDispatchTap installs an observer for every dispatched event, for assertions the
// journal cannot make: system-origin events are never journaled.
func (a *App) SetDispatchTap(fn func(event.GameEvent)) { a.scheduler.SetDispatchTap(fn) }
