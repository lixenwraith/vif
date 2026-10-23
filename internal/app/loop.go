//go:build !vif_headless

package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/vlog"
)

// A scenario change rebuilds the run because its FSM metrics are frozen at startup.
// In a session the coordinator requests the change for every participant.
func Run(cfg Config) error {
	if cfg.Mode != ModePlay {
		return fmt.Errorf("%s mode is caller-driven; Run owns the frame loop", cfg.Mode)
	}
	// One keeper for every run of the process, so a run that replaces another comes
	// back to the place that one left.
	cfg.claims = newClaimKeeper(cfg.ClaimStore)
	// A host with a player plays from its first tick and opens its door once the
	// clock runs, as :host does; only a run with nobody to play waits in a lobby.
	cfg.HostAddress, cfg.resumeHost = "", cfg.HostAddress
	// A port the player asked for that this run cannot have ends it before the
	// terminal starts, as a -join that cannot dial does.
	if cfg.resumeHost != "" && buildHasSocketNetwork {
		ln, err := network.Listen(cfg.resumeHost, network.DebugConfig(network.RoleHost, cfg.resumeHost))
		if err != nil {
			return fmt.Errorf("host %s: %w", cfg.resumeHost, err)
		}
		defer ln.Close() // the run's port closes it first; one that never served it must not keep it
		cfg.hostListener = ln
	}
	// One terminal for every run: a replacement redraws on it instead of leaving
	// and re-entering the screen. The first run's hub opens it; Run releases it.
	if cfg.Host.Presentation == nil {
		term := newTerminal(cfg)
		term.Retain()
		defer term.Release()
		cfg.Host.Presentation = term
	}
	// A join the player started at launch is seated at their press, unless the host
	// they launched from already took it.
	cfg.joinPress = cfg.JoinAddress != "" && !cfg.Host.Pressed
	rejoin := false
	var solo *Config // what a :join that fails after its dial plays on as
	for {
		next, err := runScenario(cfg, rejoin)
		cfg.hostListener, cfg.Host.Pressed = nil, false // the press the host took was the first game's
		if err != nil && solo != nil && !errors.Is(err, errSessionSignalled) {
			vlog.Warn("session", "msg", "join failed; playing solo", "join", cfg.JoinAddress, "error", err.Error())
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
		cfg, solo = cfg.restarted(next)
		rejoin = next.Rejoin
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
	next, err = a.Loop()
	if next != nil {
		next.Play = a.livePlay()
		next.Journal = a.journaling()
		// A seat's journal closes with this run, which is the last its holder hears of it
		for _, line := range a.seatJournalNotices() {
			next.Notice = joinNotice(next.Notice, line)
		}
	}
	return next, err
}

// openRun builds the App, retrying while the coordinator this participant follows
// rebuilds itself. A first -join retries only a refusal that passes in moments: it
// reports a host that is not there at once, which is what an operator dialling by
// hand wants, and a refusal that no retry helps meets the same answer a second later.
func openRun(cfg Config, rejoin bool) (*App, error) {
	deadline := time.Now().Add(parameter.SessionRejoinWindow) // [wall] a link bound
	for {
		a, err := newSessionApp(cfg)
		retry := network.RetryOf(err)
		switch {
		case err == nil:
			return a, nil
		case retry == network.RetryNever || !rejoin && retry != network.RetrySoon || !time.Now().Before(deadline):
			return nil, err
		}
		// A dial the run before handed over ends with the attempt that failed on it,
		// as a seat refused once the world was built does: the next one dials afresh.
		cfg.dialled = nil
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
		if !a.seated() {
			if next, err := a.awaitJoinPress(sigChan); next != nil || err != nil {
				return next, err
			}
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
		a.ctx.SetStatusMessage(a.cfg.notice, parameter.StatusAlert)
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

	var awayChan <-chan bool
	if p, ok := a.pres.(awayPresentation); ok {
		awayChan = p.Away()
	}
	a.holdFirstGame(awayChan)

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
	// tick that has to be running. A door a rebuilt run cannot reopen costs the
	// session, not the game.
	if a.cfg.resumeHost != "" {
		if err := a.BeginHosting(a.cfg.resumeHost); err != nil {
			a.log.Warn("session", "msg", "hosting not opened; playing solo", "address", a.cfg.resumeHost, "error", err.Error())
			a.ctx.SetStatusMessage(err.Error()+"; playing solo", parameter.StatusAlert)
		}
	}
	// After the door opens, for the same reason: every bot arrives through it. One
	// that cannot be seated costs the bot, not the game.
	if err := a.seatBots(); err != nil {
		a.log.Warn("app", "msg", "bots not seated", "error", err.Error())
		a.ctx.SetStatusMessage("Bots: "+err.Error(), parameter.StatusAlert)
	}

	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()

	inputTicker := time.NewTicker(parameter.InputTickInterval)
	defer inputTicker.Stop()

	eventChan := a.pres.Events()

	for {
		// Before the wait, not inside it: a latched restart is serviced on the
		// iteration after the intent that asked for one, and every other path
		// through the loop arrives back here too.
		if a.dismissed.Load() {
			return &restartRequest{Notice: "Removed by the host; started a local game"}, nil
		}
		if req := a.restart.Load(); req != nil {
			return req, nil
		}
		if a.leaving.Load() {
			a.leaveSession()
			continue // a replacement latched while it ran is taken first
		}
		a.serveJournal()
		select {
		case sig := <-sigChan:
			a.log.Info("app", "msg", "signal received", "signal", sig.String())
			return nil, nil

		case ev := <-eventChan:
			if !a.takeEvent(ev) {
				return nil, nil // player quit
			}

		case away := <-awayChan:
			a.setAway(away)

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

// takeEvent passes a presentation's event down the dumb pipe: key event → machine →
// intent → router. The key cell shows what the machine holds, pending or completed,
// whatever the router does. False means the player quit.
func (a *App) takeEvent(ev terminal.Event) bool {
	intent := a.inputMachine.Process(ev)
	if ev.Type == terminal.EventMouse && ev.MouseAction == terminal.MouseActionPress {
		a.touched = ev.Touch
	}
	if a.takeStart(ev, intent) {
		intent = nil // the press that starts a waiting game is nothing else
	}
	if ev.Type == terminal.EventKey {
		a.ctx.SetLastCommand(a.inputMachine.Feedback())
	}
	if intent != nil {
		before := a.pushed()
		if !a.handleIntent(intent) {
			return false
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
	if ev.Type == terminal.EventKeyReport {
		a.keyReported(ev.Report == terminal.KeyReportReleases)
	}
	return true
}

// handleResize records the terminal change and dispatches it synchronously, so the
// orchestrator (main-loop state) resizes against dimensions the handler wrote. The
// push takes the lock like any input: a journaled record landing mid-tick would
// settle inside that tick, where a reproduction cannot put it. A run whose terminal
// is its geometry publishes it for its seats to follow. A solo game waiting for its
// start begins over at the new size, as one opened at it, rather than cropped as a
// game in play is; its hold outlasts the reset, which ends a pause.
func (a *App) handleResize(width, height int) {
	a.world.RunSafe(func() {
		a.ctx.PushLocalOrigin(event.EventScreenResize,
			&event.ScreenResizePayload{Width: width, Height: height}, event.OriginInput)
		if a.ctx.TimeCtl.Holds(engine.HoldStart) && a.world.MapSizeLocal() {
			a.ctx.PushEventOrigin(event.EventGameResetRequest, &event.GameResetPayload{}, event.OriginInput)
		}
	})
	a.scheduler.DispatchEventsImmediately()
	a.orchestrator.Resize(a.ctx.Width, a.ctx.Height)
	if a.cfg.Mode.OwnsGeometry() {
		a.seatScreen.Store(&screen{a.ctx.Width, a.ctx.Height})
	}
}

// setAway pauses a solo run while its player is away and resumes it on return; a
// pause the player made before leaving stays theirs to end. The first return to an
// unseen game, Android's start screen's tap, is the press that starts it.
func (a *App) setAway(away bool) {
	a.world.RunSafe(func() {
		switch {
		case !away && a.unseen && a.ctx.TimeCtl.Holds(engine.HoldStart):
			a.ctx.PushLocalOrigin(event.EventGameHoldRequest, &event.GamePausePayload{Paused: false}, event.OriginInput)
		case away && !a.ctx.TimeCtl.IsPaused() && a.world.Session().Pausable():
			a.ctx.PauseForInput()
			a.awayPaused = true
		case !away && a.awayPaused:
			a.ctx.SetPaused(false)
			a.awayPaused = false
		}
	})
	if !away {
		a.unseen = false
	}
	a.scheduler.DispatchEventsImmediately()
}

// holdFirstGame holds the run's first game at its first tick until its player's
// press. One away before the clock starts, behind Android's start screen, is unseen
// until they come, and their return is that press. Otherwise the setting decides,
// unless the run joined a host, whose game it lands in, or its host took the press.
func (a *App) holdFirstGame(reports <-chan bool) {
	for drained := false; !drained; {
		select {
		case a.unseen = <-reports:
		default:
			drained = true
		}
	}
	if !a.unseen && (!a.ctx.WaitStart.Load() || a.cfg.JoinAddress != "" || a.cfg.Host.Pressed) {
		return
	}
	a.scheduler.Prepare() // the hold settles before the clock starts
	a.world.RunSafe(func() { a.ctx.HoldAs(engine.HoldStart) })
	a.scheduler.DispatchEventsImmediately()
}

// takeStart starts a waiting game at its player's press: a key going down that Ctrl
// or Alt does not chord, or a pointer button's, as the router admits it
func (a *App) takeStart(ev terminal.Event, intent *input.Intent) bool {
	press := ev.Type == terminal.EventKey && ev.Action == terminal.KeyPress &&
		ev.Modifiers&(terminal.ModCtrl|terminal.ModAlt) == 0 && (ev.Key < terminal.KeyCtrlA || ev.Key > terminal.KeyCtrlUnderscore) ||
		ev.Type == terminal.EventMouse && ev.MouseAction == terminal.MouseActionPress &&
			(ev.MouseBtn == terminal.MouseBtnLeft || ev.MouseBtn == terminal.MouseBtnRight)
	if !press || !a.ctx.TimeCtl.Holds(engine.HoldStart) {
		return false
	}
	started := false
	a.world.RunSafe(func() {
		a.world.WithOrigin(event.OriginInput, func() { started = a.router.TakeStart(intent) })
	})
	if started {
		a.scheduler.DispatchEventsImmediately()
	}
	return started
}

// awaitJoinPress seats this joining run at its player's press, its world built and
// checked and its session told it is ready. A press after the host's ready period
// dials again (takeSeat); a session that started another game meanwhile is joined
// anew by the next run, which the press already seats.
func (a *App) awaitJoinPress(signals <-chan os.Signal) (*restartRequest, error) {
	a.showJoinStatus(a.joinPrompt())
	for {
		select {
		case <-signals:
			return nil, errSessionSignalled
		case ev := <-a.lobbyEvents():
			if a.lobbyEventCancels(ev) {
				return nil, errSessionCanceled
			}
			if ev.Type == terminal.EventMouse && ev.MouseAction == terminal.MouseActionPress {
				a.touched = ev.Touch
			}
			if !joinPress(ev, a.ctx.MouseDisabled.Load()) {
				a.showJoinStatus(a.joinPrompt()) // redrawn after a resize, and asked as a touch was
				continue
			}
			a.showJoinStatus("Joining " + network.Shown(a.cfg.JoinAddress) + "...")
			err := a.takeSeat(true)
			if errors.Is(err, errSessionMoved) {
				// Pressed already, and holding the bots it was to seat once it was in.
				a.log.Info("session", "msg", "joining the session's new game", "join", network.Shown(a.cfg.JoinAddress))
				return &restartRequest{Rejoin: true, provided: a.cfg.Resources.Provided, Bots: a.cfg.Bots}, nil
			}
			return nil, err
		}
	}
}

// joinPress reports whether ev is the press that seats a waiting join: a key going
// down, unchorded by Ctrl or Alt, that types or moves, or a pointer button's press
// while the mouse is on. Escape, Tab and the function keys seat nobody, so a key
// reached for out of habit is not a join.
func joinPress(ev terminal.Event, mouseOff bool) bool {
	switch ev.Type {
	case terminal.EventKey:
		return ev.Action == terminal.KeyPress && ev.Modifiers&(terminal.ModCtrl|terminal.ModAlt) == 0 &&
			(ev.Key == terminal.KeyRune || ev.Key == terminal.KeyEnter ||
				ev.Key >= terminal.KeyBackspace && ev.Key <= terminal.KeyInsert)
	case terminal.EventMouse:
		return !mouseOff && ev.MouseAction == terminal.MouseActionPress &&
			(ev.MouseBtn == terminal.MouseBtnLeft || ev.MouseBtn == terminal.MouseBtnRight)
	}
	return false
}

// joinPrompt is the status line a join waiting for its press stands with, asking for
// the press the player's last suggests, as startPrompt does.
func (a *App) joinPrompt() string {
	host := network.Shown(a.cfg.JoinAddress)
	switch {
	case a.touched:
		return "Tap to join " + host
	case a.ctx.MouseDisabled.Load():
		return "Press a key to join " + host
	}
	return "Click or press a key to join " + host
}

// startPrompt is the status line a waiting game stands with: a guest waits for its
// host, and a player is asked for the press their last one suggests. A replay's
// hold is the recording's, which no press ends.
func (a *App) startPrompt() string {
	switch {
	case !a.cfg.Mode.OwnsInput() || !a.ctx.TimeCtl.Holds(engine.HoldStart):
		return ""
	case a.world.LiveSession() && !a.world.IsSessionCoordinator():
		return "Waiting for the host to start"
	case a.touched:
		return "Tap to start"
	case a.ctx.MouseDisabled.Load():
		return "Press a key to start"
	}
	return "Click or press a key to start"
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
		a.router.RefreshConfigMenu()
		snapTime.GameTime = a.ctx.TimeCtl.Now()
		snapTime.RealTime = a.ctx.TimeCtl.RealTime()
		if pos, ok := a.world.LocalCursor(); ok {
			cursorX, cursorY, cursorValid = pos.X, pos.Y, true
		}
		out = render.NewRenderContextFromGame(a.ctx, snapTime, cursorX, cursorY, cursorValid)
		out.Prompt = a.startPrompt()
	})
	if p, ok := a.pres.(coveredPresentation); ok {
		out.StatusCovered = p.Covered()
	}
	// A pause made for an away player is not theirs, and a game waiting for its start
	// has not begun: either is drawn as it is, the picture Android's start screen
	// lifts off at its tap
	out.IsPaused = out.IsPaused && !a.awayPaused && !a.ctx.TimeCtl.Holds(engine.HoldStart)
	return out
}

// frame advances one render frame; false means the player quit. A hidden page draws
// nothing, so no frames queue behind it, and is handed the whole screen on its
// return. Neither does a terminal that stopped taking output. The tick
// handshake below runs either way: the frame loop never waits on the terminal, so a
// terminal nobody reads cannot slow the world.
func (a *App) frame() bool {
	a.ctx.IncrementFrameNumber()
	draw := !pageHidden() && a.terminalTaking()
	var renderCtx render.RenderContext
	if draw {
		renderCtx = a.renderContext()
	}

	paused := a.ctx.TimeCtl.IsPaused()
	if paused {
		// Pause overlay still renders
		if draw {
			a.orchestrator.RenderFrame(renderCtx, a.world)
		}
		return true
	}

	updatePending := true
	select {
	case <-a.gameUpdateDone:
		updatePending = false
	default:
	}

	// All updates complete; RenderFrame locks internally for component access
	if draw {
		a.orchestrator.RenderFrame(renderCtx, a.world)
	}

	if !updatePending && !paused {
		select {
		case a.frameReady <- struct{}{}:
		default: // channel full, skip signal
		}
	}
	return true
}

// terminalTaking reports whether the terminal is taking output. One that has held a
// write for TerminalBlockedAfter (a hidden tab, a multiplexer client that went away)
// is not drawn for until it returns, and the hold is logged at both ends, since
// nothing else on screen or in the frame counter shows it.
func (a *App) terminalTaking() bool {
	held := a.pres.Blocked()
	switch blocked := held >= parameter.TerminalBlockedAfter; {
	case blocked && a.termBlocked.IsZero():
		a.termBlocked = time.Now().Add(-held) // [wall]
		a.log.Warn("app", "msg", "terminal stopped taking output; drawing paused",
			"held_ms", held.Milliseconds())
	case !blocked && held == 0 && !a.termBlocked.IsZero():
		a.log.Info("app", "msg", "terminal taking output again; drawing resumed",
			"held_ms", time.Since(a.termBlocked).Milliseconds()) // [wall]
		a.termBlocked = time.Time{}
	}
	return a.termBlocked.IsZero()
}

// applyMouseMode is the Router's terminal sink for mouse reporting state
func (a *App) applyMouseMode(enabled, motion bool) { a.pres.SetPointerMode(enabled, motion) }

// persistCell writes back the zoom the player left the presentation at, as
// persistPlay writes [play]
func (a *App) persistCell() {
	z, ok := a.pres.(zoomPresentation)
	if !ok || a.cfg.Settings == nil {
		return
	}
	if cell := z.Cell(); cell != a.cell {
		a.cell = cell
		if err := paths.SaveCell(a.cfg.Settings, cell); err != nil {
			a.settingsUnsaved(err)
		}
	}
}

// keyReportApplier is the Router's sink for asking for key releases: from a terminal's
// protocol or a host's own key events, and a console keyboard's when the router says.
// Nil for a presentation that reports presses alone.
func (a *App) keyReportApplier() mode.KeyReportApplier {
	p, ok := a.pres.(keyReportPresentation)
	if !ok {
		return nil
	}
	return func(console bool) {
		allow := terminal.KeySourceProtocol | terminal.KeySourceHost
		if console {
			allow |= terminal.KeySourceConsole
		}
		p.SetKeyReport(terminal.KeyReportReleases, allow)
	}
}

// keyReported hands the presentation's answer to the router, which crosses it while
// keys are captured
func (a *App) keyReported(releases bool) {
	before := a.pushed()
	a.world.RunSafe(func() { a.router.KeyReported(releases) })
	if a.pushed() != before {
		a.scheduler.DispatchEventsImmediately()
	}
}

// inputTick advances input-driven work: mouse reporting reconciliation,
// auto-fire and button repeat, macro playback. Each macro intent settles
// before the next, so a chained motion observes the applied position.
// Returns false when the player quit.
func (a *App) inputTick() bool {
	a.persistPlay()
	a.persistCell()
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
