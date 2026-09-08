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

// probeSnapshot answers the endpoint from one read of the run. Live is the tick
// counter moving, sampled across reads because a probe cannot wait for a tick: the
// run is stalled only when the clock is running, unpaused and has not moved in
// ProbeStallInterval. Ready is whether a dial would be admitted, a different question
// and not the status code. A reason is written only when one of the two is false.
func (a *App) probeSnapshot() probe.Snapshot {
	tick := a.Position().Tick
	now := time.Now() // [wall] a stall is a wall-clock condition, not a game one
	paused := a.ctx.TimeCtl.IsPaused()
	running := a.scheduler != nil && a.scheduler.Running()

	live, clock := a.observeTick(tick, now, running, paused)

	guests := a.guestCount()
	capacity := a.sessionCapacity()
	closing := a.lobbyClosing.Load()
	// The lifetime policy is read rather than folded here: a probe is a read of the
	// run, and a session that ended because an orchestrator happened to scrape it
	// would be a session whose lifetime depended on being watched. The serve loop
	// supplies the observations; this settles nothing the loop has not already
	// reached, and reports what it finds.
	life := a.life.State(now)
	ready := live && !closing && guests < capacity && life.Admit

	// Ordered by which outranks which: a session that is ending is not merely
	// full, and one that is not live is neither.
	var reason string
	switch {
	case !live:
		reason = "clock " + clock
	case !life.Admit:
		reason = life.Phase.String() + ": " + life.Reason
	case closing:
		reason = "lobby closing"
	case !ready:
		reason = "session at capacity"
	}

	detail := map[string]string{
		"tick":     strconv.FormatUint(tick, 10),
		"clock":    clock,
		"guests":   strconv.Itoa(guests),
		"capacity": strconv.Itoa(capacity),
		"address":  a.cfg.HostAddress,
		"phase":    life.Phase.String(),
	}
	if !life.Deadline.IsZero() {
		// What an allocator needs and a roster count cannot say: how long this
		// session has left before it ends itself.
		detail["expires_in"] = life.Remaining.Round(time.Second).String()
	}

	return probe.Snapshot{
		Live:   live,
		Ready:  ready,
		Reason: reason,
		Detail: detail,
	}
}

// observeTick folds one probe read into the stall detector and reports whether the
// run is live, and what its clock is doing. The clock word is reported whatever the
// verdict: an operator reading a healthy session still wants to know whether it is
// ticking or waiting in a lobby, and that is not a fault to explain.
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
		return true, "stopped"
	case paused:
		a.probeAt = now // a pause is not a stall; do not accumulate one under it
		return true, "paused"
	case now.Sub(a.probeAt) > parameter.ProbeStallInterval:
		return false, "stalled"
	}
	return true, "running"
}
