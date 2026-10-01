package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
)

// ScriptPaceMax is the -speed token that removes wall pacing from a script run.
const ScriptPaceMax = "max"

// scriptPacing resolves a run's wall pace: the interval one simulation tick may
// occupy, and whether pacing applies. An empty spec takes the default from the run
// rather than the flags — a script that starts in a session is paced from its first
// tick, a solo one runs flat out until it opens a session with :host. Bots seat
// in a session, so a run with them starts in one.
func scriptPacing(cfg Config) (interval time.Duration, paced bool, err error) {
	if cfg.TimeScaleSpec == "" {
		inSession := cfg.HostAddress != "" || cfg.JoinAddress != "" || len(cfg.Bots) > 0
		return parameter.GameUpdateInterval, inSession, nil
	}
	if cfg.TimeScaleSpec == ScriptPaceMax {
		return parameter.GameUpdateInterval, false, nil
	}
	scale, ok := engine.ParseScale(cfg.TimeScaleSpec)
	if !ok {
		return 0, false, fmt.Errorf(
			"-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8) or %q", cfg.TimeScaleSpec, ScriptPaceMax)
	}
	// A faster rate spends less wall time per tick.
	return time.Duration(int64(parameter.GameUpdateInterval) * scale.Den / scale.Num), true, nil
}

// RunScript loads an authored deterministic script and runs it. A host/join
// configuration uses the ordinary session handshake; ModeScript presents the run
// on this terminal and every other mode runs it headlessly.
func RunScript(cfg Config, path string) (journal.ScriptStats, error) {
	script, err := journal.LoadScript(path)
	if err != nil {
		return journal.ScriptStats{}, err
	}
	if script.Width != 0 {
		cfg.Width, cfg.Height = script.Width, script.Height
	}
	var driver *journal.ScriptDriver
	finished, err := runDriven(cfg, "script", path, func(a *App) (pacedSource, error) {
		d, err := journal.NewScriptDriver(scriptTarget{a: a}, script)
		if err != nil {
			return nil, err
		}
		if cfg.HostAddress != "" || cfg.JoinAddress != "" {
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
		vlog.Info("app", "msg", "script complete",
			"path", path, "actions", stats.Executed, "ticks", stats.Ticks,
			"run", stats.End.Run, "tick", stats.End.Tick)
	}
	return stats, err
}

// RunBot plays this instance's own seat with a bot graph: a name or a path, which
// cfg.Resources resolves. It is otherwise a script run, and presented, it keeps real
// time unless -speed says otherwise. It reports its counters however it stops;
// quit says the graph ended the run itself.
func RunBot(cfg Config, spec string) (bot.Stats, error) {
	graph, err := loadBotGraph(cfg.Resources, spec)
	if err != nil {
		return bot.Stats{}, err
	}
	name := graph.Name
	if cfg.Mode == ModeScript && cfg.TimeScaleSpec == "" {
		cfg.TimeScaleSpec = "1"
	}
	if cfg.Width == 0 && cfg.Height == 0 {
		cfg.terminalGeometry = cfg.Mode == ModeScript
		cfg.Width, cfg.Height = BotWidth, BotHeight
	}
	var (
		driver *bot.Driver
		played *App
	)
	quit, err := runDriven(cfg, "bot", name, func(a *App) (pacedSource, error) {
		d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.localParticipant())
		driver, played = d, a
		return botSource{d}, err
	})
	if driver == nil {
		return bot.Stats{}, err
	}
	stats := driver.Stats()
	reg := played.world.Resources.Status
	vlog.Info("app", "msg", "bot stopped", "name", name, "quit", quit, "ticks", stats.Ticks,
		"injected", stats.Injected, "dropped", stats.Dropped, "state", stats.State,
		"typed", reg.Ints.Get("typing.correct").Load(), "errors", reg.Ints.Get("typing.errors").Load())
	return stats, err
}

// loadBotGraph resolves and parses one graph spec, a name or a path.
func loadBotGraph(o resource.Options, spec string) (*bot.Graph, error) {
	data, name, err := resource.BotGraph(o, spec)
	if err != nil {
		return nil, err
	}
	return bot.ParseGraph(name, data)
}

// runDriven plays one driven participant, a script or a bot, until it finishes or
// the process is signalled.
func runDriven(cfg Config, kind, name string, build func(*App) (pacedSource, error)) (finished bool, err error) {
	signals, stopSignals := notifySignals()
	defer stopSignals()
	return drive(cfg, kind, name, signals, nil, build)
}

// drive builds the App in the session cfg names, lets build put a driver on it, and
// steps that driver headless under the run's wall pace, or presented. finished is
// false when stop or a cancelled session ended the run first. While hold reports
// true the run stands still, as a paused clock does, and re-anchors its pace after.
func drive(cfg Config, kind, name string, signals <-chan os.Signal, hold func() bool,
	build func(*App) (pacedSource, error)) (finished bool, err error) {
	if cfg.Mode != ModeScript {
		cfg.Mode = ModeHeadless
	}
	cfg.scriptedSession = true
	interval, paced, err := scriptPacing(cfg)
	if err != nil {
		return false, err
	}

	a, err := newScriptApp(cfg, signals)
	if err != nil {
		if errors.Is(err, errSessionCanceled) {
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
	if a.cfg.Mode == ModeScript {
		return true, runPresented(a, src, kind, name, interval, paced, signals)
	}
	// A solo run that opens a session with :host gains a peer to keep step with, so
	// pacing engages at that moment. The clock is re-anchored there rather than
	// carried forward, or the ticks it ran flat out would be a debt the pacing
	// immediately spends. An explicit -speed max is honoured either way.
	autoPace := cfg.TimeScaleSpec == ""
	nextTick := time.Now() // [wall] pacing only
	for {
		select {
		case <-signals:
			return false, nil
		default:
		}
		if a.dismissed.Load() {
			return false, nil
		}
		if hold != nil && hold() {
			if !waitScriptTick(signals, parameter.PausedPollInterval) {
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
		if !paced && autoPace && a.HostAddr() != "" {
			paced, nextTick = true, time.Now() // [wall]
			vlog.Info("app", "msg", kind+" pacing engaged",
				"address", a.HostAddr(), "tick", a.Position().Tick)
		}
		if paced {
			trim, step := a.scheduler.TakePace()
			nextTick = nextTick.Add(engine.PacedInterval(interval, trim, step))
			if !waitScriptTick(signals, time.Until(nextTick)) {
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

// newScriptApp starts the same tick-zero gate as interactive play, but leaves the
// scheduler caller-driven after the roster is closed.
func newScriptApp(cfg Config, signals <-chan os.Signal) (*App, error) {
	a, err := newSessionApp(cfg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*App, error) {
		a.Close()
		return nil, err
	}
	if a.pendingJoin != nil {
		if err := a.pollTerminalEarly(); err != nil {
			return fail(err)
		}
		if err := a.startJoinSession(signals); err != nil {
			return fail(err)
		}
	}
	if err := a.hub.StartAll(); err != nil {
		return fail(err)
	}
	if a.cfg.HostAddress != "" {
		// Before the lobby, which the host's own bots may be all of.
		if err := a.seatBots(); err != nil {
			return fail(err)
		}
		if err := a.startHostSession(signals); err != nil {
			return fail(err)
		}
	}
	if a.cfg.HostAddress != "" || a.cfg.JoinAddress != "" {
		a.activateNetworkSession()
		if err := a.resumeJoinedSession(); err != nil {
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
	a.openMidRunJoins()
	if a.cfg.HostAddress == "" {
		if err := a.seatBots(); err != nil {
			return fail(err)
		}
	}
	return a, nil
}

func waitScriptTick(signals <-chan os.Signal, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-signals:
		return false
	case <-timer.C:
		return true
	}
}

type scriptTarget struct{ a *App }

func (t scriptTarget) Position() event.Stamp { return t.a.Position() }
func (t scriptTarget) Tick(n int)            { t.a.Tick(n) }
func (t scriptTarget) Inject(intents ...*input.Intent) bool {
	return t.a.Inject(intents...)
}
func (t scriptTarget) Emit(et event.EventType, payload any, domain core.Domain) {
	t.a.ctx.PushEventFull(et, payload, event.OriginDebug, domain)
	t.a.Settle()
}
