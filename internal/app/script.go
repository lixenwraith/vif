package app

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/vlog"
)

// ScriptPaceMax is the -speed token that removes wall pacing from a script run.
const ScriptPaceMax = "max"

// scriptPacing resolves whether a run's wall pace applies; a paced tick occupies the
// game interval at the rate the run's clock holds, which -speed sets alone and the
// host's in a session. An empty spec takes the default from the run rather than the
// flags — a script that starts in a session is paced from its first tick, a solo one
// runs flat out until it opens a session with :host. Bots seat in a session.
func scriptPacing(cfg Config) (paced bool, err error) {
	switch {
	case cfg.TimeScaleSpec == "":
		return cfg.Membership.HostAddress != "" || cfg.Membership.JoinAddress != "" || len(cfg.Membership.Bots) > 0, nil
	case cfg.TimeScaleSpec == ScriptPaceMax:
		return false, nil
	}
	if _, ok := engine.ParseScale(cfg.TimeScaleSpec); !ok {
		return false, fmt.Errorf(
			"-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8) or %q", cfg.TimeScaleSpec, ScriptPaceMax)
	}
	return true, nil
}

// RunScript loads an authored deterministic script and runs it. A host/join
// configuration uses the ordinary session handshake; ModeScript presents the run
// on the platform's Host, as Run does, and every other mode runs it headlessly.
func RunScript(cfg Config, host Host, path string) (journal.ScriptStats, error) {
	script, err := journal.LoadScript(path)
	if err != nil {
		return journal.ScriptStats{}, err
	}
	if script.Width != 0 {
		cfg.Width, cfg.Height = script.Width, script.Height
	}
	var driver *journal.ScriptDriver
	finished, err := runDriven(runConfig{Config: cfg, host: host}, "script", path, func(a *App) (pacedSource, error) {
		d, err := journal.NewScriptDriver(scriptTarget{a: a}, script)
		if err != nil {
			return nil, err
		}
		if cfg.Membership.HostAddress != "" || cfg.Membership.JoinAddress != "" {
			if err := d.Live(); err != nil {
				return nil, err
			}
		}
		driver = d
		return scriptSource{d}, nil
	})
	if driver == nil {
		return journal.ScriptStats{}, err
	}
	stats := driver.Stats()
	if finished {
		vlog.Info("app", "msg", "script completed",
			"path", path, "actions", stats.Executed, "ticks", stats.Ticks,
			"run", stats.End.Run, "tick", stats.End.Tick)
	}
	return stats, err
}

// RunBot plays this instance's own seat with a bot graph: a name or a path, which
// cfg.Resources resolves. It is otherwise a script run, and presented, it keeps real
// time unless -speed says otherwise. It reports its counters however it stops;
// quit says the graph ended the run itself.
func RunBot(cfg Config, host Host, spec string) (bot.Stats, error) {
	graph, err := membership.LoadBotGraph(cfg.Resources, spec)
	if err != nil {
		return bot.Stats{}, err
	}
	name := graph.Name
	if cfg.Mode == ModeScript && cfg.TimeScaleSpec == "" {
		cfg.TimeScaleSpec = "1"
	}
	run := runConfig{Config: cfg, host: host}
	if run.Width == 0 && run.Height == 0 {
		run.wiring.terminalGeometry = run.Mode == ModeScript
		run.Width, run.Height = BotWidth, BotHeight
	}
	var (
		driver *bot.Driver
		played *App
	)
	quit, err := runDriven(run, "bot", name, func(a *App) (pacedSource, error) {
		d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.membership.LocalParticipant())
		driver, played = d, a
		return botSource{d}, err
	})
	if driver == nil {
		return bot.Stats{}, err
	}
	stats := driver.Stats()
	reg := played.world.Resources.Status
	played.log.Info("app", "msg", "bot stopped", "name", name, "quit", quit, "ticks", stats.Ticks,
		"injected", stats.Injected, "dropped", stats.Dropped, "fitness", stats.Fitness, "state", stats.State,
		"typed", reg.Ints.Get("typing.correct").Load(), "errors", reg.Ints.Get("typing.errors").Load())
	return stats, err
}

// runDriven plays one driven participant, a script or a bot, until it finishes or
// the process is signalled.
func runDriven(cfg runConfig, kind, name string, build func(*App) (pacedSource, error)) (finished bool, err error) {
	signals, stopSignals := notifySignals()
	defer stopSignals()
	return drive(cfg, kind, name, signals, nil, build)
}

// drive builds the App in the session cfg names, lets build put a driver on it, and
// steps that driver headless under the run's wall pace, or presented. finished is
// false when stop or a cancelled session ended the run first. While hold reports
// true, or a hold stands, the run stands still and re-anchors its pace after.
func drive(cfg runConfig, kind, name string, signals <-chan os.Signal, hold func() bool,
	build func(*App) (pacedSource, error)) (finished bool, err error) {
	if cfg.Mode != ModeScript {
		cfg.Mode = ModeHeadless
	}
	cfg.wiring.scriptedSession = true
	paced, err := scriptPacing(cfg.Config)
	if err != nil {
		return false, err
	}

	a, err := newScriptApp(cfg, signals)
	if err != nil {
		if errors.Is(err, membership.ErrSessionCanceled) {
			return false, nil
		}
		return false, err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r)
		}
		a.Close()
	}()

	src, err := build(a)
	if err != nil {
		return false, err
	}
	// cfg, not a.cfg: once build hands the App out, its config is read under its lock
	if cfg.Mode == ModeScript {
		return true, runPresented(a, src, kind, name, paced, signals)
	}
	// A solo run that opens a session, with :host or for its bots, gains a peer to
	// keep step with, so pacing engages at that moment. The clock is re-anchored there
	// rather than carried forward, or the ticks it ran flat out would be a debt the
	// pacing immediately spends. An explicit -speed max is honoured either way.
	autoPace := cfg.TimeScaleSpec == ""
	nextTick := time.Now() // [wall] pacing only
	for {
		select {
		case <-signals:
			return false, nil
		default:
		}
		// A headless bot left with no session plays for nobody; a watched one plays
		// on, as a person does, and a seat ends as its holder says (seatEnds).
		if a.membership.Dismissed() || kind == "bot" && a.cfg.wiring.membership.Holder == 0 && membership.ForkCell(a.world).Load() {
			return false, nil
		}
		// Between two steps, as a presenting run's loop serves it between two waits
		a.scribe.ServeJournal()
		// A held run stands still as its holder's pause does, but still drains its
		// link: the host's resume is the next epoch it sends.
		if held := a.ctx.TimeCtl.Held(); held || hold != nil && hold() {
			if held {
				a.ApplyPendingCorrections()
			}
			if !membership.WaitScriptTick(signals, parameter.PausedPollInterval) {
				return false, nil
			}
			nextTick = time.Now() // [wall]
			continue
		}

		more, err := src.Step()
		if err != nil {
			return false, err
		}
		if !more {
			return true, nil
		}
		if !paced && autoPace && a.membership.SessionTransport() != nil {
			paced, nextTick = true, time.Now() // [wall]
			a.log.Info("app", "msg", kind+" pacing engaged",
				"address", a.HostAddr(), "tick", a.Position().Tick)
		}
		if paced {
			trim, step := a.scheduler.TakePace()
			tick := a.ctx.TimeCtl.Scale().Wall(a.ctx.World.TickInterval())
			nextTick = nextTick.Add(engine.PacedInterval(tick, trim, step))
			if !membership.WaitScriptTick(signals, time.Until(nextTick)) {
				return false, nil
			}
		}
	}
}

// pacedSource is the driven stream a run advances. Each driver reports its own
// counters, because "how far through" means a different thing to a record stream,
// an action list and a graph.
type pacedSource interface {
	Step() (bool, error)
	progress() string
}

type scriptSource struct{ d *journal.ScriptDriver }

func (s scriptSource) Step() (bool, error) { return s.d.Step() }

func (s scriptSource) progress() string {
	st := s.d.Stats()
	return fmt.Sprintf("run %d tick %d | %d/%d act", st.End.Run, st.End.Tick, st.Executed, st.Actions)
}

type botSource struct{ d *bot.Driver }

func (s botSource) Step() (bool, error) { return s.d.Step() }

func (s botSource) progress() string {
	st := s.d.Stats()
	return fmt.Sprintf("tick %d | %s | %d intents, %d dropped", st.Ticks, st.State, st.Injected, st.Dropped)
}

// screen is a terminal's size in cells.
type screen struct{ w, h int }

// seatSource ends a seat once its session is gone for it (seatEnds): a holder left
// with no session takes its bots along. A seat follows its holder's screen as it is
// published, between two of its steps.
type seatSource struct {
	botSource
	ends   []*atomic.Bool
	screen *atomic.Pointer[screen]
	run    *App
	shown  screen
}

func (s *seatSource) Step() (bool, error) {
	for _, gone := range s.ends {
		if gone.Load() {
			return false, nil
		}
	}
	if sc := s.screen.Load(); sc != nil && *sc != s.shown {
		s.shown = *sc
		s.run.Resize(sc.w, sc.h)
	}
	return s.botSource.Step()
}

// newScriptApp starts the same tick-zero gate as interactive play, but leaves the
// scheduler caller-driven after the roster is closed.
func newScriptApp(cfg runConfig, signals <-chan os.Signal) (*App, error) {
	a, err := newSessionApp(cfg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*App, error) {
		a.Close()
		return nil, err
	}
	if a.membership.HasPendingJoin() {
		if err := a.pollTerminalEarly(); err != nil {
			return fail(err)
		}
		if err := a.membership.StartJoinSession(signals); err != nil {
			return fail(err)
		}
	}
	if err := a.hub.StartAll(); err != nil {
		return fail(err)
	}
	if a.cfg.Membership.HostAddress != "" {
		// Before the lobby, which the host's own bots may be all of.
		if err := a.membership.SeatBots(); err != nil {
			return fail(err)
		}
		if err := a.membership.StartHostSession(signals); err != nil {
			return fail(err)
		}
	}
	if a.cfg.Membership.HostAddress != "" || a.cfg.Membership.JoinAddress != "" {
		a.membership.ActivateNetworkSession()
		if err := a.membership.ResumeJoinedSession(); err != nil {
			return fail(err)
		}
		a.ctx.TimeCtl.SetPaused(false)
	}
	a.scheduler.Prepare()
	// A scripted host is a host: its lobby has closed, so a dial from here is a
	// mid-run join and a guest that dropped comes back through the same gate. The
	// clock its capture waits on is the script's own, advanced by the caller that is
	// about to start stepping. On a run that hosts nothing this is inert until a
	// later :host opens a session.
	a.membership.OpenMidRunJoins()
	if a.cfg.Membership.HostAddress == "" {
		if err := a.membership.SeatBots(); err != nil {
			return fail(err)
		}
	}
	return a, nil
}

type scriptTarget struct{ a *App }

func (t scriptTarget) Position() event.Stamp        { return t.a.Position() }
func (t scriptTarget) Declared() component.Declared { return t.a.behaviors.Declared() }
func (t scriptTarget) Tick(n int)                   { t.a.Tick(n) }
func (t scriptTarget) Inject(intents ...*input.Intent) bool {
	return t.a.Inject(intents...)
}
func (t scriptTarget) Emit(et event.EventType, payload any, domain core.Domain) {
	// A script's own hold would stand for good: its driven loop takes no step while
	// the run is held, so no action of its timeline could end it.
	if p, ok := payload.(*event.GamePausePayload); ok && et == event.EventGameHoldRequest && p.Paused {
		t.a.log.Warn("script", "msg", "a script cannot hold its own run; the hold request was refused")
		return
	}
	t.a.ctx.PushEventFull(et, payload, event.OriginDebug, domain)
	t.a.Settle()
}
