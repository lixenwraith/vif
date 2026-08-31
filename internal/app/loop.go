package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// defaultMouseMode is the reporting mode used outside free-look
const defaultMouseMode = terminal.MouseModeClick | terminal.MouseModeDrag

// Run wires, runs, and tears down the game
func Run(cfg Config) error {
	if cfg.Mode != ModePlay {
		return fmt.Errorf("%s mode is caller-driven; Run owns the frame loop", cfg.Mode)
	}
	a, err := newInteractiveApp(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r) // does not return under unix
		}
		a.Close()
	}()
	return a.Loop()
}

// Loop starts the services and runs the frame loop until the player quits
func (a *App) Loop() error {
	if a.cfg.Mode != ModePlay {
		return fmt.Errorf("%s mode has no interactive loop", a.cfg.Mode)
	}
	sigChan, stopSignals := notifySignals()
	defer stopSignals()

	if a.pendingJoin != nil {
		if err := a.startJoinSession(); err != nil {
			return err
		}
	}
	if err := a.hub.StartAll(); err != nil {
		return err
	}
	if a.cfg.HostAddress != "" {
		if err := a.startHostSession(sigChan); err != nil {
			if errors.Is(err, errSessionCanceled) {
				return nil
			}
			return err
		}
	}
	if a.cfg.HostAddress != "" || a.cfg.JoinAddress != "" {
		a.activateNetworkSession()
		// Paused directly during construction, without emitting an operator event:
		// the start gate is the authority that releases tick-zero game time.
		a.ctx.TimeCtl.SetPaused(false)
	}

	// Prime the first tick, then start the game clock
	a.frameReady <- struct{}{}
	a.scheduler.Start()

	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()

	inputTicker := time.NewTicker(parameter.InputTickInterval)
	defer inputTicker.Stop()

	eventChan := a.termSvc.Events()

	for {
		select {
		case sig := <-sigChan:
			vlog.Info("app", "msg", "signal received", "signal", sig.String())
			return nil

		case ev := <-eventChan:
			// Dumb pipe: key event → machine → intent → router
			if intent := a.inputMachine.Process(ev); intent != nil {
				before := a.pushed()
				if !a.handleIntent(intent) {
					return nil // player quit
				}
				// Input events bypass the game tick wait; an intent that emitted
				// nothing has nothing of its own to settle
				if a.pushed() != before {
					a.scheduler.DispatchEventsImmediately()
				}
			}

			if ev.Type == terminal.EventResize {
				a.handleResize(ev.Width, ev.Height)
			}

		case <-inputTicker.C:
			if !a.inputTick() {
				return nil
			}

		case <-frameTicker.C:
			if !a.frame() {
				return nil
			}
		}
	}
}

// handleIntent runs one intent under the world lock, tagged with its producer.
// The entire router path (motions, operators, mouse cursor writes, undo
// capture, mode transitions) is serialized against tick/event/render by
// construction — mode/ must never acquire the world lock itself.
func (a *App) handleIntent(intent *input.Intent) bool {
	origin := event.OriginInput
	if intent.MacroPlayback {
		origin = event.OriginMacro
	}
	cont := true
	a.world.RunSafe(func() {
		a.world.WithOrigin(origin, func() {
			cont = a.router.Handle(intent)
		})
	})
	return cont
}

// handleResize records the terminal change and lets the handler apply it. The
// dispatch is synchronous so the orchestrator resizes against dimensions the
// handler has already written; the render pipeline is main-loop state, so it stays
// here rather than in the handler.
func (a *App) handleResize(width, height int) {
	a.ctx.PushEventOrigin(event.EventScreenResize,
		&event.ScreenResizePayload{Width: width, Height: height}, event.OriginInput)
	a.scheduler.DispatchEventsImmediately()
	a.orchestrator.Resize(a.ctx.Width, a.ctx.Height)
}

// frame advances one render frame; false means the player quit
func (a *App) frame() bool {
	a.ctx.IncrementFrameNumber()

	// Snapshot shared state under the world lock: minimal hold time, and
	// RenderContext is built from a consistent view
	var (
		snapTime         engine.TimeResource
		cursorX, cursorY int
		cursorValid      bool
		renderCtx        render.RenderContext
	)

	a.world.RunSafe(func() {
		// Render on the continuous clock: the tick-written stamps are quantized to
		// the tick, which shows as stepped animation once the rate is slowed.
		// Local copy only: the render loop never writes tick-owned resources.
		snapTime.GameTime = a.ctx.TimeCtl.Now()
		snapTime.RealTime = a.ctx.TimeCtl.RealTime()
		if pos, ok := a.world.LocalCursor(); ok {
			cursorX, cursorY, cursorValid = pos.X, pos.Y, true
		}
		// Config (Map/Viewport/Camera/crop) is mutated under updateMutex by
		// LevelSetup/reset handlers on the event-loop and tick goroutines;
		// RenderContext must be built inside the same critical section
		renderCtx = render.NewRenderContextFromGame(a.ctx, snapTime, cursorX, cursorY, cursorValid)
	})

	paused := a.ctx.TimeCtl.IsPaused()
	if paused {
		// Pause overlay still renders
		a.orchestrator.RenderFrame(renderCtx, a.world)
		return true
	}

	updatePending := true
	select {
	case <-a.gameUpdateDone:
		updatePending = false
	default:
	}

	// All updates complete; RenderFrame locks internally for component access
	a.orchestrator.RenderFrame(renderCtx, a.world)

	if !updatePending && !paused {
		select {
		case a.frameReady <- struct{}{}:
		default: // channel full, skip signal
		}
	}
	return true
}

// applyMouseMode is the Router's terminal sink for mouse reporting state
func (a *App) applyMouseMode(enabled, motion bool) {
	if !enabled {
		a.term.SetMouseMode(0)
		return
	}
	mode := defaultMouseMode
	if motion {
		mode |= terminal.MouseModeMotion
	}
	a.term.SetMouseMode(mode)
}

// inputTick advances input-driven work: mouse reporting reconciliation,
// auto-fire and button repeat, macro playback. Each macro intent settles
// before the next, so a chained motion observes the applied position.
// Returns false when the player quit.
func (a *App) inputTick() bool {
	before := a.pushed()
	if a.processInputTick() || a.pushed() != before {
		a.scheduler.DispatchEventsImmediately()
	}

	for _, intent := range a.router.ProcessMacroTick() {
		before = a.pushed()
		if !a.handleIntent(intent) {
			return false
		}
		if a.pushed() != before {
			a.scheduler.DispatchEventsImmediately()
		}
	}
	return true
}

// processInputTick serializes timer-driven router reads with cursor lifecycle.
// Unlike Handle intents, ProcessInputTick enters through no input event, so the
// App boundary must acquire the world lock explicitly before it snapshots the
// local cursor for auto-fire and held-button repeat.
func (a *App) processInputTick() bool {
	emitted := false
	a.world.RunSafe(func() {
		emitted = a.router.ProcessInputTick()
	})
	return emitted
}
