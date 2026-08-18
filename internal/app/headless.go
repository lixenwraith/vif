// Package app: headless harness.
//
// A headless App has no terminal, audio service or renderer, and runs on a
// ManualClock advanced only by Tick. Nothing runs on another goroutine, so a
// run is a pure function of its seed, its config, and the injected intent
// sequence. Close is the caller's responsibility.
package app

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

// NewHeadless builds and starts a headless runtime without spawning the
// scheduler or event goroutines. Tick drives the simulation instead.
func NewHeadless(cfg Config) (*App, error) {
	cfg.Headless = true

	a, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if err := a.hub.StartAll(); err != nil {
		a.Close()
		return nil, err
	}

	// Seal the system set and freeze the metric set before anything dispatches,
	// so a Settle preceding the first Tick cannot register a late metric.
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

// Inject applies intents at a tick boundary and settles what they emit.
// Returns false once an intent quits the game; remaining intents are dropped.
// Intents are the sole injection path: hand-built ones must carry Count >= 1
// where a motion count applies, since no input.Machine normalizes them.
func (a *App) Inject(intents ...*input.Intent) bool {
	cont := true
	for _, intent := range intents {
		if !a.handleIntent(intent) {
			cont = false
			break
		}
	}
	a.scheduler.Settle()
	return cont
}

// InputTick advances input-driven work: auto-fire, held-button repeat and macro
// playback. Their intervals are game time, so a caller that never Ticks emits at
// most one round. Returns false when a macro intent quits the game.
func (a *App) InputTick() bool {
	emitted := a.router.ProcessInputTick()

	macroIntents := a.router.ProcessMacroTick()
	for _, intent := range macroIntents {
		if !a.handleIntent(intent) {
			return false
		}
	}

	if emitted || len(macroIntents) > 0 {
		a.scheduler.Settle()
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
