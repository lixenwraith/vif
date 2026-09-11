package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
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

// Serve holds the session open until a signal stops it. It is App.Loop with the
// presentation removed and the frame handshake kept: the scheduler applies render
// backpressure at real time and slower, so a run that never released it would tick
// at the timeout rather than at the interval.
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
			// The first guest connected and left before confirming it installed the
			// world. Here that is a session with nobody in it and nobody watching, so
			// it ends the way an unclaimed one does — cleanly, with a reason, so the
			// Job completes and the allocator can place the next request. Until the
			// lobby can restart in place this is a window; see doc/kubernetes-fleet.md.
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
	// than a lobby member, so a dropped guest comes back into the slot its departure
	// released. The gate reads a capture a playout lead ahead, so arming it over a
	// stopped clock would time every such dial out.
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
			st := a.life.Observe(a.guestCount(), now)
			if st.Expired {
				a.logSessionEnd(st)
				return nil
			}
			a.holdVacant(st)

		case <-report.C:
			vlog.Info("app", "msg", "server", "summary", a.SessionSummary())
		}
	}
}

// interrupt folds the roster in at the instant of the signal and only then asks the
// policy what a termination request means. A drain waits for the guests the session
// holds, and the loop's last observation can be a whole lifecycleInterval old, so a
// stale empty roster would end a session somebody had only just joined.
func (a *App) interrupt(now time.Time, reason string) lifecycle.State {
	a.life.Observe(a.guestCount(), now)
	return a.life.Interrupt(now, reason)
}

// holdVacant parks a session nobody is in. An unbounded host resets an abandoned
// world once; a host with -empty preserves it until that grace ends the process, so
// a reconnect inside the grace returns to the same match.
func (a *App) holdVacant(st lifecycle.State) {
	if st.Phase != lifecycle.PhaseVacant {
		// The vacancy is over rather than merely interrupted, so the restart it
		// already spent is spent. Cleared here rather than on the resume, because a
		// dial that never becomes a participant leaves the phase vacant and its
		// clock running — and a restart re-armed by that dial would fire again on
		// the very next reading.
		a.vacantReset.Store(false)
		a.resumeVacant()
		return
	}
	if a.ctx.TimeCtl.SetPaused(true) {
		a.parked.Store(true)
		if a.life.Policy().Empty > 0 {
			vlog.Info("app", "msg", "session parked", "tick", a.Position().Tick,
				"expires_in", st.Remaining.Round(time.Second).String())
		} else {
			vlog.Info("app", "msg", "session parked", "tick", a.Position().Tick,
				"restart_in", parameter.SessionVacantReset.String())
		}
	}
	a.dropOwnerlessCursors()
	if a.life.Policy().Empty > 0 {
		return
	}
	if st.Vacant < parameter.SessionVacantReset || !a.vacantReset.CompareAndSwap(false, true) {
		return
	}
	vlog.Info("app", "msg", "parked session restarted",
		"vacant", st.Vacant.Round(time.Second).String(), "tick", a.Position().Tick)
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventGameResetRequest, &event.GameResetPayload{},
			event.OriginSession, core.DomainShared)
	})
}

// dropOwnerlessCursors is the roster half of an empty session: a dedicated host
// drives no cursor, so with no guest every cursor on the map belongs to nobody. It
// exists for the restart above, whose boot spawns the cursor a solo run starts with
// — which a mid-run join cannot take, because an arrival creates a cursor in a free
// slot and would find this one occupied.
func (a *App) dropOwnerlessCursors() {
	var held int
	a.world.RunSafe(func() { held = a.world.Resources.Player.Count() })
	if held == 0 {
		return
	}
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventCursorDespawnRequest,
			&event.CursorDespawnRequestPayload{All: true}, event.OriginSession, core.DomainShared)
	})
	vlog.Info("app", "msg", "parked session dropped ownerless cursors", "cursors", held)
}

// resumeVacant releases a parked session. It is called from the accept goroutine
// before the mid-run gate reads a capture a playout lead ahead of the current tick:
// that gate waits on ticks, so a dial served by a stopped clock would time out
// instead of being admitted. A run that never parked is untouched.
func (a *App) resumeVacant() {
	if !a.parked.CompareAndSwap(true, false) {
		return
	}
	a.ctx.TimeCtl.SetPaused(false)
	vlog.Info("app", "msg", "parked session resumed", "tick", a.Position().Tick)
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
