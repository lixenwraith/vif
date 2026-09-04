// The supervised run's answer about itself.
//
// A dedicated host has nobody watching its screen, because it has no screen. What
// a person reads from a status bar — is it ticking, can it be joined, how many are
// in it — an orchestrator has to be able to read over a socket, and this is where
// the run decides what those answers are. internal/probe decides only how to say
// them.

package app

import (
	"strconv"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/probe"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// startProbe binds the endpoint, if one was configured. It runs before the lobby:
// a run waiting for its first guest is a run a supervisor is watching start, and
// the stall detector already answers for a clock that has not begun.
func (a *App) startProbe() error {
	if a.cfg.ProbeAddress == "" {
		return nil
	}
	p, err := probe.New(a.cfg.ProbeAddress, a.probeSnapshot, a.world.Resources.Status)
	if err != nil {
		return err
	}
	if err := p.Start(); err != nil {
		return err
	}
	a.probe = p
	vlog.Info("app", "msg", "probe listening", "address", p.Addr())
	return nil
}

// closeProbe stops the endpoint. Safe on a run that never bound one.
func (a *App) closeProbe() {
	if a.probe == nil {
		return
	}
	_ = a.probe.Close()
	a.probe = nil
}

// probeSnapshot answers both probes from one read of the run.
//
// Liveness is the tick counter moving, which is the one thing that distinguishes a
// host still simulating its session from a process that is merely still resident.
// It is sampled across reads rather than measured inside one: a probe cannot wait
// for a tick, so it compares this read against the last and calls the run stalled
// only when the clock is running, is not paused, and has not moved in
// ProbeStallInterval.
//
// Readiness is whether a dial would be admitted, which is deliberately not the
// same question. A lobby waiting for its first guest is ready — being dialled is
// what it is waiting for — and a session at capacity is not, because a Service
// that kept routing to it would be sending participants to a roster with no room.
func (a *App) probeSnapshot() probe.Snapshot {
	tick := a.Position().Tick
	now := time.Now() // [wall] a stall is a wall-clock condition, not a game one
	paused := a.ctx.TimeCtl.IsPaused()
	running := a.scheduler != nil && a.scheduler.Running()

	live, reason := a.observeTick(tick, now, running, paused)

	a.sessionMu.Lock()
	guests := len(a.sessionRoster)
	if guests > 0 {
		guests-- // the coordinator holds a roster entry and no slot
	}
	a.sessionMu.Unlock()

	capacity := a.sessionCapacity()
	closing := a.lobbyClosing.Load()
	ready := !closing && guests < capacity
	switch {
	case !ready && closing:
		reason = "lobby closing"
	case !ready:
		reason = "session at capacity"
	}

	return probe.Snapshot{
		Live:   live,
		Ready:  ready,
		Reason: reason,
		Detail: map[string]string{
			"tick":     strconv.FormatUint(tick, 10),
			"guests":   strconv.Itoa(guests),
			"capacity": strconv.Itoa(capacity),
			"address":  a.cfg.HostAddress,
		},
	}
}

// observeTick folds one probe read into the stall detector and reports whether the
// run is live. Caller supplies the sample so both probes share one.
func (a *App) observeTick(tick uint64, now time.Time, running, paused bool) (bool, string) {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()

	moved := tick != a.probeTick
	if moved || a.probeAt.IsZero() {
		a.probeTick, a.probeAt = tick, now
	}
	switch {
	case !running:
		// Not started, or stopped on the way out. Neither is a fault: the lobby
		// has not released tick zero yet, and a run that is shutting down is not
		// one to restart.
		return true, "clock not running"
	case paused:
		a.probeAt = now // a pause is not a stall; do not accumulate one under it
		return true, "paused"
	case now.Sub(a.probeAt) > parameter.ProbeStallInterval:
		return false, "tick stalled"
	}
	return true, ""
}
