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
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// serveReportInterval is how often a server logs what it is holding. It is a log
// line rather than a status bar because nothing here draws one.
const serveReportInterval = 30 * time.Second

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
	if err := a.startHostSession(sigChan); err != nil {
		if errors.Is(err, errSessionCanceled) {
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

	for {
		select {
		case sig := <-sigChan:
			vlog.Info("app", "msg", "signal received", "signal", sig.String())
			return nil

		case <-frameTicker.C:
			a.releaseFrame()

		case <-report.C:
			vlog.Info("app", "msg", "server", "summary", a.SessionSummary())
		}
	}
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

// openMidRunJoins ends the lobby's closing window and arms the mid-run gate, in
// that order: a dial refused a moment ago retries into a gate that now exists.
func (a *App) openMidRunJoins() {
	a.lateJoins.Store(true)
	a.lobbyClosing.Store(false)
}

// localPlayers is how many cursors this instance drives. A dedicated host drives
// none, which is the whole of what "zero players" means.
func (a *App) localPlayers() int {
	if a.localSlot() == parameter.NoPlayerSlot {
		return 0
	}
	return 1
}
