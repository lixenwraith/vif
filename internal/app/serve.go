// The dedicated host: the interactive runtime with its two ends removed. No
// terminal, no renderer, no audio, and no cursor of its own — what is left is the
// part a session cannot do without, running on the real clock and the scheduler
// goroutine, because a session's simulation has to advance whether or not anybody is
// watching it here.
//
// Holding no cursor is a roster property rather than an absence. The coordinator
// keeps its participant identity, its authority term and its vote; its slot is
// parameter.NoPlayerSlot, so every "is this my cursor" test answers no without a
// special case. The FSM's boot cursor is not suppressed: it is created as always and
// the roster hands it to the first guest, which keeps shared creation order identical
// to an ordinary host's.

package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/lifecycle"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// serveReportInterval is how often a server logs what it is holding. It is a log
// line rather than a status bar because nothing here draws one.
const serveReportInterval = 30 * time.Second

// lifecycleInterval is how often the run folds its roster into the lifetime policy.
// The bounds it enforces are counted in tens of seconds, so a second's resolution
// is far finer than any of them and costs one mutex and one comparison; a tick is
// not the place to do this, because the roster changes on a connection rather than
// on a simulation step.
const lifecycleInterval = time.Second

// RunServer wires, runs and tears down a dedicated host.
func RunServer(cfg Config) error {
	cfg.Mode = ModeServer
	a, err := newSessionApp(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r) // does not return under unix
		}
		a.Close()
	}()
	return a.Serve()
}

// Serve holds the session open until a signal stops it.
//
// The loop is App.Loop with the presentation removed and one thing kept: the frame
// handshake. The scheduler applies render backpressure at real time and slower, so
// a run that never released the handshake would tick at the timeout rather than at
// the interval. A server has no renderer, so it releases the same handshake on the
// same interval and draws nothing.
func (a *App) Serve() error {
	if a.cfg.Mode != ModeServer {
		return fmt.Errorf("%s mode is not a dedicated host", a.cfg.Mode)
	}
	sigChan, stopSignals := notifySignals()
	defer stopSignals()

	if err := a.hub.StartAll(); err != nil {
		return err
	}
	// Before the lobby, not after it. The lobby is a wait, and a wait is exactly
	// when a supervisor most needs an answer: a probe that only appears once the
	// session is running would report nothing for the whole window in which the
	// pod is starting, which reads as a pod that failed to start.
	if err := a.startProbe(); err != nil {
		// Refused rather than degraded. A probe that did not bind is a pod whose
		// orchestrator cannot tell a healthy host from a wedged one, which is a
		// worse condition than a host that did not start.
		return err
	}
	// After the probe, so a supervisor can already see the run, and before the
	// lobby, because the window an allocated session gives its first guest is the
	// lobby. An unbounded policy starts too and simply never reaches a deadline.
	a.life.Start(time.Now())

	if err := a.startHostSession(sigChan); err != nil {
		switch {
		case errors.Is(err, errSessionCanceled):
			return nil
		case errors.Is(err, errSessionExpired):
			a.logSessionEnd(a.life.State(time.Now()))
			return nil
		case errors.Is(err, errLobbyAbandoned):
			// The first guest connected and then left before confirming it had
			// installed the world. On a host somebody started by hand that is a
			// failure worth reporting; here it is a session with nobody in it and
			// nobody watching, so it ends the way an unclaimed one does — cleanly,
			// with a reason, so the Job completes rather than failing and the
			// allocator can place the next request.
			//
			// It is also, until the lobby can be restarted in place, a window in
			// which any peer that reaches the port first can end a session somebody
			// else was allocated. See the hardening notes in doc/kubernetes-fleet.md.
			a.logSessionEnd(a.life.Expire(time.Now(), "lobby abandoned before the session started"))
			return nil
		}
		return err
	}
	a.activateNetworkSession()
	// Paused during construction so the lobby wait does not age a game-time
	// deadline; the start gate is what releases tick zero.
	a.ctx.TimeCtl.SetPaused(false)

	a.frameReady <- struct{}{}
	a.scheduler.Start()
	// After the scheduler, not before it. From here a dial is a mid-run join rather
	// than a lobby member — which is what lets a guest that dropped come back into
	// the slot its departure released, and what lets the rest of the roster arrive
	// in its own time rather than being waited for. The gate reads a capture a
	// playout lead ahead of the current tick, so arming it over a clock that has
	// not started would time every such dial out instead of admitting it.
	a.openMidRunJoins()
	vlog.Info("app", "msg", "server running",
		"address", a.cfg.HostAddress, "capacity", a.sessionCapacity())

	frameTicker := time.NewTicker(parameter.FrameUpdateInterval)
	defer frameTicker.Stop()
	report := time.NewTicker(serveReportInterval)
	defer report.Stop()
	life := time.NewTicker(lifecycleInterval)
	defer life.Stop()

	for {
		select {
		case sig := <-sigChan:
			// A signal drains rather than exits: the roster is what the session is
			// for, and a rollout that ended a match in progress would be a rollout
			// nobody could schedule. A second signal ends it, and so does a drain
			// that finds an empty roster or runs out its deadline.
			st := a.interrupt(time.Now(), "signal "+sig.String())
			vlog.Info("app", "msg", "signal received",
				"signal", sig.String(), "phase", st.Phase.String(),
				"guests", st.Guests, "reason", st.Reason)
			if st.Expired {
				a.logSessionEnd(st)
				return nil
			}

		case <-frameTicker.C:
			a.releaseFrame()

		case now := <-life.C:
			if st := a.life.Observe(a.guestCount(), now); st.Expired {
				a.logSessionEnd(st)
				return nil
			}

		case <-report.C:
			vlog.Info("app", "msg", "server", "summary", a.SessionSummary())
		}
	}
}

// interrupt folds the roster in at the instant of the signal and only then asks the
// policy what a termination request means.
//
// The order is the point. A drain waits for the guests the session holds, and the
// loop's last observation can be a whole lifecycleInterval old — so a signal that
// arrived just after a guest connected would otherwise read a stale empty roster
// and end a session somebody had only just joined.
func (a *App) interrupt(now time.Time, reason string) lifecycle.State {
	a.life.Observe(a.guestCount(), now)
	return a.life.Interrupt(now, reason)
}

// logSessionEnd records why an allocated session stopped. It is the one line an
// operator reading a pod's last output needs: a container that exits cleanly says
// nothing about whether it was never claimed, emptied, or asked to go.
func (a *App) logSessionEnd(st lifecycle.State) {
	vlog.Info("app", "msg", "session ended",
		"phase", st.Phase.String(), "reason", st.Reason, "guests", st.Guests,
		"address", a.cfg.HostAddress, "tick", a.Position().Tick)
}

// releaseFrame is App.frame's handshake without the frame: it takes the completed
// update and lets the next tick start.
func (a *App) releaseFrame() {
	select {
	case <-a.gameUpdateDone:
	default:
		return // an update is still running; the tick that finishes it releases itself
	}
	select {
	case a.frameReady <- struct{}{}:
	default: // channel full, skip signal
	}
}

// localPlayers is how many cursors this instance drives. A dedicated host drives
// none, which is the whole of what "zero players" means.
func (a *App) localPlayers() int {
	if a.localSlot() == parameter.NoPlayerSlot {
		return 0
	}
	return 1
}
