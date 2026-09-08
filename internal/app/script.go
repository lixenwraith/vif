package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// ScriptPaceMax is the -speed token that removes wall pacing from a script run.
const ScriptPaceMax = "max"

// scriptPacing resolves a run's wall pace: the interval one simulation tick may
// occupy, and whether pacing applies. An empty spec takes the default from the run
// rather than the flags — a script that starts in a session is paced from its first
// tick, a solo one runs flat out until it opens a session with :host.
func scriptPacing(cfg Config) (interval time.Duration, paced bool, err error) {
	if cfg.TimeScaleSpec == "" {
		inSession := cfg.HostAddress != "" || cfg.JoinAddress != ""
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
	if cfg.Mode != ModeScript {
		cfg.Mode = ModeHeadless
	}
	cfg.scriptedSession = true
	if script.Width != 0 {
		cfg.Width, cfg.Height = script.Width, script.Height
	}
	interval, paced, err := scriptPacing(cfg)
	if err != nil {
		return journal.ScriptStats{}, err
	}

	signals, stopSignals := notifySignals()
	defer stopSignals()
	a, err := newScriptApp(cfg, signals)
	if err != nil {
		if errors.Is(err, errSessionCanceled) {
			return journal.ScriptStats{}, nil
		}
		return journal.ScriptStats{}, err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r)
		}
		a.Close()
	}()

	driver, err := journal.NewScriptDriver(scriptTarget{a: a}, script)
	if err != nil {
		return journal.ScriptStats{}, err
	}
	if a.cfg.Mode == ModeScript {
		return runPresentedScript(a, driver, path, interval, paced, signals)
	}
	// A solo script that opens a session with :host gains a peer to keep step with,
	// so pacing engages at that moment. The clock is re-anchored there rather than
	// carried forward, or the ticks it ran flat out would be a debt the pacing
	// immediately spends. An explicit -speed max is honoured either way.
	autoPace := cfg.TimeScaleSpec == ""
	nextTick := time.Now() // [wall] pacing only
	for {
		select {
		case <-signals:
			return driver.Stats(), nil
		default:
		}

		more, err := driver.Step()
		if err != nil {
			return driver.Stats(), err
		}
		if !more {
			break
		}
		if !paced && autoPace && a.HostAddr() != "" {
			paced, nextTick = true, time.Now() // [wall]
			vlog.Info("app", "msg", "script pacing engaged",
				"address", a.HostAddr(), "tick", a.Position().Tick)
		}
		if paced {
			nextTick = nextTick.Add(interval)
			if !waitScriptTick(signals, time.Until(nextTick)) {
				return driver.Stats(), nil
			}
		}
	}
	return reportScript(driver, path), nil
}

// reportScript logs and returns one completed run's counters.
func reportScript(driver *journal.ScriptDriver, path string) journal.ScriptStats {
	stats := driver.Stats()
	vlog.Info("app", "msg", "script complete",
		"path", path, "actions", stats.Executed, "ticks", stats.Ticks,
		"run", stats.End.Run, "tick", stats.End.Tick)
	return stats
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
