//go:build !vif_headless

package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/vlog"
)

// defaultMouseMode is the reporting mode used outside free-look
const defaultMouseMode = terminal.MouseModeClick | terminal.MouseModeDrag

// Run wires, runs, and tears down the game, once per scenario the player asks for.
// A scenario change ends one run and starts another on the same command line with
// a different -s: the regions a scenario declares are what register the FSM metric
// set, and that set is frozen for the life of a run. In a session every
// participant does this together — the coordinator because the operator asked, the
// rest because the coordinator said so.
func Run(cfg Config) error {
	if cfg.Mode != ModePlay {
		return fmt.Errorf("%s mode is caller-driven; Run owns the frame loop", cfg.Mode)
	}
	// A host with a player plays from its first tick and opens its door once the
	// clock runs, as :host does; only a run with nobody to play waits in a lobby.
	cfg.HostAddress, cfg.resumeHost = "", cfg.HostAddress
	rejoin := false
	var solo *Config // what a :join that fails after its dial plays on as
	for {
		next, err := runScenario(cfg, rejoin)
		if err != nil && solo != nil && !errors.Is(err, errSessionSignalled) {
			vlog.Warn("app", "msg", "join failed; playing solo", "join", cfg.JoinAddress, "error", err.Error())
			cfg, solo = *solo, nil
			cfg.notice = "Join failed: " + err.Error()
			continue
		}
		if err != nil && !errors.Is(err, errSessionCanceled) {
			return err
		}
		if next == nil {
			return nil
		}
		solo, cfg.notice, cfg.dialled = nil, "", nil
		if next.Scenario != "" {
			cfg.Resources.Scenario, cfg.Resources.Embedded = next.Scenario, false
		}
		// Hosting resumes after the clock rather than before it: this run is not
		// waiting for a lobby, it is reopening a door its guests are already at.
		cfg.HostAddress, cfg.resumeHost = "", next.Host
		switch {
		case next.Rejoin:
			if next.Join != "" {
				cfg.JoinAddress = next.Join
			}
		case next.Join != "":
			prev := cfg
			prev.JoinAddress, prev.SessionName = "", ""
			solo = &prev
			cfg = cfg.joining(next.Join)
		default:
			// Not following anyone: a run that led its session, and one that
			// inherited it and has nobody left, both start over on their own.
			cfg.JoinAddress = ""
		}
		rejoin, cfg.dialled = next.Rejoin, next.dialled
		vlog.Info("app", "msg", "run restarting", "scenario", cfg.Resources.Scenario,
			"host", next.Host, "join", cfg.JoinAddress, "rejoin", rejoin)
	}
}

// runScenario owns one App from construction to teardown. What it returns is the
// run to build next; nil means the player quit.
func runScenario(cfg Config, rejoin bool) (next *restartRequest, err error) {
	a, err := openRun(cfg, rejoin)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r) // does not return under unix
		}
		a.Close()
	}()
	return a.Loop()
}

// openRun builds the App, retrying while the coordinator this participant follows
// rebuilds itself. Only a rejoin retries: a first -join reports a host that is not
// there at once, which is what an operator dialling by hand wants, and an identity
// the coordinator refuses will not be a different identity a second later.
func openRun(cfg Config, rejoin bool) (*App, error) {
	if !rejoin {
		return newSessionApp(cfg)
	}
	deadline := time.Now().Add(parameter.SessionRejoinWindow) // [wall] a link bound
	for {
		a, err := newSessionApp(cfg)
		switch {
		case err == nil:
			return a, nil
		case network.IsIdentityRefusal(err) || !time.Now().Before(deadline):
			return nil, err
		}
		time.Sleep(parameter.SessionRejoinInterval)
	}
}

// Loop starts the services and runs the frame loop until the player quits or this
// run is replaced, which it describes on the way out.
func (a *App) Loop() (*restartRequest, error) {
	if a.cfg.Mode != ModePlay {
		return nil, fmt.Errorf("%s mode has no interactive loop", a.cfg.Mode)
	}
	sigChan, stopSignals := notifySignals()
	defer stopSignals()

	if a.pendingJoin != nil {
		// The terminal is polled before the rest of the hub, and only it: the gate
		// owns the dialled stream until it hands it over, so the network service
		// must not start yet — and without an event source the gate is a wait with
		// no key and no signal to leave on.
		if err := a.pollTerminalEarly(); err != nil {
			return nil, err
		}
		if err := a.startJoinSession(sigChan); err != nil {
			return nil, err
		}
	}
	if err := a.hub.StartAll(); err != nil {
		return nil, err
	}
	a.reportAudioSpec()
	a.recordMusic()
	if a.cfg.notice != "" {
		a.ctx.SetStatusMessage(a.cfg.notice, parameter.StatusMessageMaxDuration, true)
	}
	if a.cfg.JoinAddress != "" {
		a.activateNetworkSession()
		if err := a.resumeJoinedSession(); err != nil {
			return nil, err
		}
		// A guest applies corrections between two ticks, and nothing on this side
		// calls Tick: the scheduler owns the tick loop, so the apply loop needs a
		// goroutine of its own. A tick runs inside one acquisition of the update
		// mutex, which is what makes World.RunSafe "between two ticks".
		a.corrections.StartCorrector()
		// Paused directly during construction, without emitting an operator event:
		// the start gate is the authority that releases tick-zero game time.
		a.ctx.TimeCtl.SetPaused(false)
	}

	// Prime the first tick, then start the game clock
	a.frameReady <- struct{}{}
	a.scheduler.Start()
	// After the clock, not before it: from here a dial is a mid-run join rather than
	// a lobby member, and the gate that serves one reads a capture a playout lead
	// ahead of the current tick. It also ends the lobby's closing window, which is
	// what a run that opens a session later with :host needs even when it never had
	// a lobby of its own.
	a.openMidRunJoins()
	// After the clock: the mid-run gate reads a capture a playout lead ahead of a
	// tick that has to be running. A door that cannot open costs the session, not
	// the game.
	if a.cfg.resumeHost != "" {
		if err := a.BeginHosting(a.cfg.resumeHost); err != nil {
			vlog.Warn("app", "msg", "hosting not opened; playing solo", "address", a.cfg.resumeHost, "error", err.Error())
			a.ctx.SetStatusMessage("Host: "+err.Error()+"; playing solo", parameter.StatusMessageMaxDuration, true)
		}
	}

	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()

	inputTicker := time.NewTicker(parameter.InputTickInterval)
	defer inputTicker.Stop()

	eventChan := a.termSvc.Events()

	for {
		// Before the wait, not inside it: a latched restart is serviced on the
		// iteration after the intent that asked for one, and every other path
		// through the loop arrives back here too.
		if req := a.restart.Load(); req != nil {
			return req, nil
		}
		select {
		case sig := <-sigChan:
			vlog.Info("app", "msg", "signal received", "signal", sig.String())
			return nil, nil

		case ev := <-eventChan:
			// Dumb pipe: key event → machine → intent → router
			if intent := a.inputMachine.Process(ev); intent != nil {
				before := a.pushed()
				if !a.handleIntent(intent) {
					return nil, nil // player quit
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
				return nil, nil
			}

		case <-frameTicker.C:
			if !a.frame() {
				return nil, nil
			}
		}
	}
}

// handleResize records the terminal change and lets the handler apply it. The
// dispatch is synchronous so the orchestrator resizes against dimensions the
// handler has already written; the render pipeline is main-loop state, so it stays
// here rather than in the handler.
func (a *App) handleResize(width, height int) {
	a.ctx.PushLocalOrigin(event.EventScreenResize,
		&event.ScreenResizePayload{Width: width, Height: height}, event.OriginInput)
	a.scheduler.DispatchEventsImmediately()
	a.orchestrator.Resize(a.ctx.Width, a.ctx.Height)
}

// renderContext reads everything a frame draws from, in one critical section: Config
// is mutated under updateMutex by LevelSetup and reset handlers, so it has to be read
// with the cursor and the clock. The clock read is the continuous one — tick-written
// stamps are quantised and show as stepped animation at a slowed rate. Everything
// taken here is a local copy; a render never writes a tick-owned resource.
func (a *App) renderContext() render.RenderContext {
	var (
		snapTime         engine.TimeResource
		cursorX, cursorY int
		cursorValid      bool
		out              render.RenderContext
	)
	a.world.RunSafe(func() {
		snapTime.GameTime = a.ctx.TimeCtl.Now()
		snapTime.RealTime = a.ctx.TimeCtl.RealTime()
		if pos, ok := a.world.LocalCursor(); ok {
			cursorX, cursorY, cursorValid = pos.X, pos.Y, true
		}
		out = render.NewRenderContextFromGame(a.ctx, snapTime, cursorX, cursorY, cursorValid)
	})
	return out
}

// frame advances one render frame; false means the player quit
func (a *App) frame() bool {
	a.ctx.IncrementFrameNumber()
	renderCtx := a.renderContext()

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
