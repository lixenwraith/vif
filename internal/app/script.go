// Package app: authored headless script runtime.
package app

import (
	"errors"
	"os"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// RunScript loads an authored deterministic script and runs it headlessly. A
// host/join configuration uses the ordinary session handshake and real-time tick
// pacing; solo scripts run as fast as the caller can drive them.
func RunScript(cfg Config, path string) (journal.ScriptStats, error) {
	script, err := journal.LoadScript(path)
	if err != nil {
		return journal.ScriptStats{}, err
	}
	cfg.Mode = ModeHeadless
	cfg.scriptedSession = true
	if script.Width != 0 {
		cfg.Width, cfg.Height = script.Width, script.Height
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
	pace := cfg.HostAddress != "" || cfg.JoinAddress != ""
	nextTick := time.Now()
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
		if pace {
			nextTick = nextTick.Add(parameter.GameUpdateInterval)
			if !waitScriptTick(signals, time.Until(nextTick)) {
				return driver.Stats(), nil
			}
		}
	}
	stats := driver.Stats()
	vlog.Info("app", "msg", "script complete",
		"path", path, "actions", stats.Executed, "ticks", stats.Ticks,
		"run", stats.End.Run, "tick", stats.End.Tick)
	return stats, nil
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
		if err := a.startJoinSession(); err != nil {
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
