package app

import (
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/render"
)

// defaultMouseMode is the reporting mode used outside free-look
const defaultMouseMode = terminal.MouseModeClick | terminal.MouseModeDrag

// Run wires, runs, and tears down the game
func Run(cfg Config) error {
	a, err := New(cfg)
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
	if err := a.hub.StartAll(); err != nil {
		return err
	}

	// Prime the first tick, then start the game clock
	a.frameReady <- struct{}{}
	a.scheduler.Start()

	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()

	eventChan := a.termSvc.Events()
	lastMouseMode := defaultMouseMode

	for {
		select {
		case ev := <-eventChan:
			// Dumb pipe: key event → machine → intent → router
			if intent := a.inputMachine.Process(ev); intent != nil {
				if !a.handleIntent(intent) {
					return nil // player quit
				}
			}

			// Input events bypass the game tick wait, acquires lock
			a.scheduler.DispatchEventsImmediately()

			if want := a.wantMouseMode(); want != lastMouseMode {
				a.term.SetMouseMode(want)
				lastMouseMode = want
			}

			if ev.Type == terminal.EventResize {
				a.ctx.Width = ev.Width
				a.ctx.Height = ev.Height
				a.ctx.HandleResize()
				a.orchestrator.Resize(a.ctx.Width, a.ctx.Height)
			}

		case <-frameTicker.C:
			if !a.frame() {
				return nil
			}
		}
	}
}

// handleIntent runs one intent under the world lock.
// The entire router path (motions, operators, mouse cursor writes, undo
// capture, mode transitions) is serialized against tick/event/render by
// construction — mode/ must never acquire the world lock itself.
func (a *App) handleIntent(intent *input.Intent) bool {
	cont := true
	a.world.RunSafe(func() {
		cont = a.router.Handle(intent)
	})
	return cont
}

// wantMouseMode derives terminal mouse reporting from context flags
func (a *App) wantMouseMode() terminal.MouseMode {
	if a.ctx.MouseDisabled.Load() {
		return 0
	}
	want := defaultMouseMode
	if a.ctx.MouseFreeMode.Load() {
		want |= terminal.MouseModeMotion
	}
	return want
}

// frame advances one render frame; false means the player quit
func (a *App) frame() bool {
	a.ctx.IncrementFrameNumber()

	a.router.ProcessMouseTick()

	macroIntents := a.router.ProcessMacroTick()
	for _, intent := range macroIntents {
		if !a.handleIntent(intent) {
			return false
		}
	}
	if len(macroIntents) > 0 {
		a.scheduler.DispatchEventsImmediately()
	}

	// Snapshot shared state under the world lock: minimal hold time, and
	// RenderContext is built from a consistent view
	var (
		snapTime         engine.TimeResource
		cursorX, cursorY int
		renderCtx        render.RenderContext
	)

	a.world.RunSafe(func() {
		if a.ctx.IsPaused.Load() {
			// Keep wall-clock advancing so paused frames still animate
			a.world.Resources.Time.RealTime = a.ctx.PausableClock.RealTime()
		}
		// Plain struct copy; TimeResource holds no locks
		snapTime = *a.world.Resources.Time
		if pos, ok := a.world.Positions.GetPosition(a.world.Resources.Player.Entity); ok {
			cursorX, cursorY = pos.X, pos.Y
		}
		// Config (Map/Viewport/Camera/crop) is mutated under updateMutex by
		// LevelSetup/reset handlers on the event-loop and tick goroutines;
		// RenderContext must be built inside the same critical section
		renderCtx = render.NewRenderContextFromGame(a.ctx, snapTime, cursorX, cursorY)
	})

	if a.ctx.IsPaused.Load() {
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

	if !updatePending && !a.ctx.IsPaused.Load() {
		select {
		case a.frameReady <- struct{}{}:
		default: // channel full, skip signal
		}
	}
	return true
}
