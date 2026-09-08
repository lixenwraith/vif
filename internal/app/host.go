package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// sessionControl adapts App to engine.SessionController. Every method is the locked
// form: the operator command surface runs inside App.handleIntent's critical
// section, so a controller method that took the world lock would deadlock the
// instance at the moment the command fired.
type sessionControl struct{ a *App }

func (c sessionControl) BeginHosting(addr string) error { return c.a.beginHostingLocked(addr) }
func (c sessionControl) SessionSummary() string         { return c.a.sessionSummaryLocked() }

// BeginHosting opens a running instance to participants, for a caller that holds
// no lock. The operator command path reaches beginHostingLocked instead.
func (a *App) BeginHosting(addr string) error {
	var err error
	a.world.RunSafe(func() { err = a.beginHostingLocked(addr) })
	return err
}

// beginHostingLocked opens a running instance to participants: the same session
// every other path builds, started at a tick that is not zero. What it adds is the
// transport, which this App then owns for the rest of the run. Binding under the
// world lock costs a tick, bounded by one listen(2) — the same deliberate operator
// cost `:log on` pays. Caller MUST hold updateMutex.
func (a *App) beginHostingLocked(addr string) error {
	if addr == "" {
		return errors.New("host: no address")
	}
	if a.sessionTransportLocked() != nil {
		return errors.New("host: this run is already in a session")
	}
	a.sessionMu.Lock()
	if a.midRunPort != nil {
		a.sessionMu.Unlock()
		return errors.New("host: this run is already hosting")
	}
	a.sessionMu.Unlock()

	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("host %q: %w", addr, err)
	}

	// The address is recorded before the listener exists, so every later reader —
	// the accept goroutine's anchor, the status line — sees it already set, and
	// nothing writes it again once a peer can arrive. The roster ceiling needs no
	// such handling: an unset -players already means the whole roster, and a run
	// that opens a session mid-game has no lobby for the flag's other meaning.
	a.cfg.HostAddress = addr
	port := network.NewSocketPort(a.hostNetworkConfig())

	// Armed before the listener exists: a run that opens a session mid-game has no
	// startup lobby, so every dial it ever sees is a mid-run join.
	a.lateJoins.Store(true)

	// Everything the accept goroutine reads is published before the listener that
	// wakes it exists: Start returns with the loop already running, and a dial in
	// that instant reaches OnAdmit. Its capture read then blocks on the world lock
	// this call holds, which is what makes the attach below happen first.
	a.sessionMu.Lock()
	a.midRunPort = port
	a.sessionRoster = []network.SessionParticipant{{ID: hostParticipantID, Slot: 0}}
	roster := slices.Clone(a.sessionRoster)
	a.sessionMu.Unlock()

	// A lobby binds this through the roster it closes on; a session opened mid-run
	// has no lobby, and an unattributed cursor is one no successor can see leave.
	a.bindCursorOwnersLocked(roster, hostParticipantID)

	// The run that opens a session authors its first term. Everything downstream
	// reads authorship from here rather than from the identity the handshake
	// assigns, which is what lets a later handoff move it.
	a.openAuthorityLocked(network.SessionOffer{
		Anchor: a.joinAnchorLocked(), Host: hostParticipantID, Assigned: hostParticipantID,
		Term: network.FirstTerm, Participants: roster,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}, hostParticipantID)

	// Attaching latches the world as shared (D-14) and installs the departure and
	// digest hooks; activating closes the pre-session crossing window so this
	// instance's own artifacts start taking the session's playout lead.
	a.attachTransportLocked(port)
	a.activateNetworkSessionLocked()

	if err := port.Start(); err != nil {
		a.sessionMu.Lock()
		a.midRunPort, a.sessionRoster = nil, nil
		a.sessionMu.Unlock()
		a.lateJoins.Store(false)
		a.cfg.HostAddress = ""
		a.world.Resources.Network = nil
		return fmt.Errorf("host %s: %w", addr, err)
	}

	// The authority's cadence starts with the session. Nothing is published while
	// no peer is connected — publish returns on an empty roster — so a host waiting
	// alone pays a ticker and no world reads.
	a.corrections.startPump()

	bound := addr
	if b := port.Addr(); b != nil {
		bound = b.String()
	}
	vlog.Info("app", "msg", "hosting opened mid-run",
		"address", bound, "tick", a.Position().Tick, "capacity", a.sessionCapacity()+1)
	a.ctx.SetStatusMessage("Hosting on "+bound, 0, false)
	return nil
}

// sessionTransport returns the attached endpoint, nil when this run has none. The
// question is whether a transport exists, not whether a peer is on it: a host
// waiting alone is in a session and a second :host would open a second one.
func (a *App) sessionTransport() engine.NetworkPort {
	var port engine.NetworkPort
	a.world.RunSafe(func() { port = a.sessionTransportLocked() })
	return port
}

// sessionTransportLocked is the same read for a caller that holds the world lock.
// Caller MUST hold updateMutex.
func (a *App) sessionTransportLocked() engine.NetworkPort {
	if r := a.world.Resources.Network; r != nil {
		return r.Port
	}
	return nil
}

// SessionSummary is a one-line description of what this run is part of.
func (a *App) SessionSummary() string {
	var out string
	a.world.RunSafe(func() { out = a.sessionSummaryLocked() })
	return out
}

// sessionSummaryLocked is the same for the operator command path.
// Caller MUST hold updateMutex.
func (a *App) sessionSummaryLocked() string {
	if a.sessionTransportLocked() == nil {
		return "Solo run; :host <addr> opens it to participants"
	}
	peers := a.world.Resources.Status.Ints.Get("network.peers").Load()
	participant := a.world.LocalParticipant()
	addr := a.cfg.HostAddress
	role := "host"
	if a.cfg.JoinAddress != "" {
		addr, role = a.cfg.JoinAddress, "guest"
	}
	reg := a.world.Resources.Status
	// The D-14 latch used to sit in the status bar beside every connection state,
	// where it was a constant: on for every session and off for every solo run. It
	// is a fact about the run rather than a thing to watch, so it is named here.
	latch := "map open"
	if reg.Bools.Get("network.map_latched").Load() {
		latch = "map latched"
	}
	line := fmt.Sprintf("Session %s %s, participant %d, %d peer(s), tick %d, %s",
		role, addr, participant, peers, a.Position().Tick, latch)
	if a.authority != nil {
		if s := a.authority.summary(); s != "" {
			line += "; " + s
		}
	}
	// Reachability, which is what decides whether losing the authority moves the
	// session or forks it. A participant that binds nothing plays normally and is
	// never elected, so it is worth saying which one this is.
	if n := reg.Ints.Get("network.reachable").Load(); n > 0 {
		line += fmt.Sprintf(", %d confirmed reachable", n)
	}
	if reg.Bools.Get("network.listening").Load() {
		line += ", listening"
	}
	if reg.Bools.Get("network.host_lost").Load() {
		return line + "; HOST LOST, continuing locally from the last authoritative state"
	}

	// The operating point, read from the published cells rather than from the
	// scheduler itself. This runs under the world lock and the scheduler takes it
	// on the other side of its own — a capture is a world read — so reading the
	// atomics is not a shortcut here, it is the only order that cannot deadlock.
	cadence := reg.Ints.Get("snapshot.cadence_ticks").Load()
	if cadence == 0 {
		return line
	}
	state := "nominal"
	switch {
	case reg.Bools.Get("snapshot.cadence_floor_breached").Load():
		state = "BELOW THE CONVERGENCE FLOOR"
	case reg.Bools.Get("snapshot.cadence_constrained").Load():
		state = "constrained"
	}
	return line + fmt.Sprintf(
		"; cadence %d ticks, keyframe every %d (%d ticks), link %d ms ±%d, %d B/s, uplink %d B/s, floor %d B/s, %s",
		cadence,
		reg.Ints.Get("snapshot.cadence_keyframe_interval").Load(),
		reg.Ints.Get("snapshot.cadence_keyframe_period_ticks").Load(),
		reg.Ints.Get("network.link_rtt_ms").Load(),
		reg.Ints.Get("network.link_jitter_ms").Load(),
		reg.Ints.Get("network.link_bps").Load(),
		reg.Ints.Get("snapshot.cadence_uplink_bps").Load(),
		reg.Ints.Get("snapshot.cadence_floor_bps").Load(),
		state)
}

// releaseMidRunJoiner completes the gate for a participant the accept loop just
// admitted. It runs on the accept goroutine, so it must not assume the world lock is
// free and must not hold it longer than one capture. A tick-zero lobby releases
// everyone together and does not come through here.
func (a *App) releaseMidRunJoiner(id network.PeerID) {
	// One at a time. The handshakes that reach here run concurrently, and this gate
	// waits on a ready count that is cumulative over the session: two of them at
	// once could not tell which joiner had confirmed, and the link measurement each
	// takes would be timed against the other's transfer.
	a.midRunGate.Lock()
	defer a.midRunGate.Unlock()

	a.sessionMu.Lock()
	port := a.midRunPort
	a.sessionMu.Unlock()
	if port == nil {
		// A dedicated host never opened a socket of its own: its endpoint is the
		// one NetworkService contributed for -serve.
		port, _ = a.socketPort()
	}
	if port == nil {
		return
	}
	if err := a.sendMidRunGate(port, id); err != nil {
		vlog.Warn("app", "msg", "mid-run join failed", "participant", id, "error", err.Error())
		// The stream is already a peer by the time this runs, so refusing the join
		// means dropping it: a participant holding a handshake it could not finish
		// would otherwise stay in the session receiving crossings for a world it
		// never installed. Dropping it runs the ordinary departure path, which is
		// what returns its identity to the pool.
		port.Disconnect(uint32(id))
		a.releaseParticipant(id)
	}
}

// sendMidRunGate sends one joiner the closed roster and the world it names, then
// crosses its arrival. The world is the cadence's keyframe rather than a read taken
// for this join, so two joins arriving together share one read. "Fresh enough" is a
// playout lead past the admission, not the current tick: an epoch flushed just
// before the admission reaches nobody and is in no capture taken at that tick.
func (a *App) sendMidRunGate(port *network.SocketPort, id network.PeerID) error {
	offer, err := a.midRunOffer(id)
	if err != nil {
		return err
	}

	// Read before the gate is sent: ReadyCount is cumulative over the session, so
	// what this join waits for is an increase rather than a value.
	ready := port.ReadyCount()

	minTick := a.Position().Tick + parameter.NetworkBarrierDelayTicks
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] a link bound
	body, tick, err := a.corrections.keyframeAt(minTick, deadline)
	if err != nil {
		return err
	}
	offer.SnapshotTick, offer.SnapshotBytes = tick, len(body)
	chunks, err := network.EncodeSnapshotChunks(tick, body)
	if err != nil {
		return err
	}
	start, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	if !port.Send(uint32(id), uint8(network.MsgStart), start) {
		return fmt.Errorf("could not release participant %d", id)
	}
	// The transfer is the measurement, so it is timed from the first chunk to the
	// joiner's confirmation. Nothing else on a fresh link has pushed enough bytes
	// to say what it carries.
	transferStart := time.Now() // [wall] a link measurement, not a game clock
	for i, chunk := range chunks {
		if !port.Send(uint32(id), uint8(network.MsgStateSnapshot), chunk) {
			return fmt.Errorf("could not send capture chunk %d/%d", i+1, len(chunks))
		}
	}
	if err := a.awaitJoinerReady(port, id, ready); err != nil {
		return err
	}
	// Refused *after* the install rather than before it, because the install is
	// what completes the measurement. A participant refused here is dropped by the
	// caller and its identity returned to the pool, which is the same unwind a
	// join that could not finish its gate takes.
	if err := a.admitLink(port, id, len(body), time.Since(transferStart)); err != nil {
		return err
	}

	assignment, _ := offer.Participant(id)
	a.crossParticipantArrival(id, assignment.Slot)
	vlog.Info("app", "msg", "mid-run participant admitted",
		"participant", id, "slot", assignment.Slot, "snapshot_tick", tick, "bytes", len(body))
	return nil
}

// midRunOffer allocates nothing: the acceptor already assigned this participant, so
// the roster is closed as it stands and addressed to it.
func (a *App) midRunOffer(id network.PeerID) (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	offer := a.offerLocked(anchor, id)
	return offer, offer.Validate()
}

// awaitJoinerReady waits for the joiner to confirm it installed the world.
//
// The wait is bounded and the bound is the point: a participant that cannot install
// and answer within it is one whose crossings would arrive after the ticks they
// name, and admitting it would trade a failed join for a divergence.
func (a *App) awaitJoinerReady(port *network.SocketPort, id network.PeerID, was int) error {
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] a link bound, not a game one
	for time.Now().Before(deadline) {
		if port.ReadyCount() > was {
			return nil
		}
		if !port.Connected(uint32(id)) {
			return fmt.Errorf("participant %d dropped during its join", id)
		}
		select {
		case <-port.Changes():
		case <-time.After(2 * time.Millisecond):
		}
	}
	return fmt.Errorf("participant %d did not confirm its install within %s",
		id, parameter.NetworkJoinReadyTimeout)
}

// crossParticipantArrival announces a mid-run arrival as a D-3 crossing rather than
// as a local reaction to a connect: the cursor it creates is a shared entity, so
// every instance must create it at one agreed tick or their creation order diverges
// (D-11). The coordinator is the only producer, as it is for a departure.
func (a *App) crossParticipantArrival(id network.PeerID, slot uint8) {
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventParticipantJoined,
			&event.ParticipantJoinedPayload{Participant: uint32(id), Slot: slot},
			event.OriginSession, core.DomainPlayer)
	})
}

// resumeJoinedSession hands the traffic the gate held to the port and closes the gap
// between the installed world and the tick the session has reached by simulating it.
// The gap is the transfer and the install, so it is a function of world size and
// link speed; left open it is permanent, and every crossing goes out k ticks late.
// Call after the transport takes the stream and before game time is released.
func (a *App) resumeJoinedSession() error {
	if a.pendingJoin == nil {
		return nil
	}
	port, err := a.injectPort()
	if err != nil {
		return err
	}
	held := a.pendingJoin.Deferred()
	host := uint32(a.pendingJoin.HostID())
	for _, msg := range held {
		port.Inject(host, uint8(msg.Type), msg.Payload)
	}

	// A tick-zero lobby has no gap by construction: the host is frozen at tick zero
	// until every participant is ready, so it has produced nothing, and a probe tick
	// here would put this instance one tick ahead of a session that has not started.
	if !a.sessionOffer.CarriesSnapshot() || a.sessionOffer.SnapshotTick == 0 {
		a.reportJoinLag(0)
		return nil
	}

	// The gap is only partly readable from what the gate held: epochs closed during
	// the install sat in the socket until the port started, and the barrier learns of
	// those only once something drains them. So it closes by rounds — catch up to the
	// newest tick known, let that draining reveal the next, stop when it stops moving.
	caught := uint64(0)
	for range joinCatchUpRounds {
		local := a.Position().Tick
		target := max(newestHeldEpoch(held), a.awaitSessionTick(local))
		if target <= local {
			break
		}
		step := target - local
		if caught+step > parameter.NetworkJoinCatchUpTicks {
			return fmt.Errorf("join: the session is more than %d ticks ahead of the world it sent",
				parameter.NetworkJoinCatchUpTicks)
		}
		a.scheduler.RunTicks(int(step))
		caught += step
	}
	return a.finishCatchUp(held, caught)
}

// awaitSessionTick drains the transport until a peer epoch newer than local shows
// up, or the wait runs out. Every tick closes an epoch, so on a live session the
// wait is bounded by one tick interval; the ceiling is there for the session that
// has stopped producing, where the honest answer is "no newer tick" rather than a
// hang.
func (a *App) awaitSessionTick(local uint64) uint64 {
	deadline := time.Now().Add(joinEpochWait) // [wall] a link bound, not a game one
	for {
		a.world.RunSafe(func() {
			for _, sys := range a.world.Systems() {
				if d, ok := sys.(interface{ DrainPeers() }); ok {
					d.DrainPeers()
				}
			}
		})
		if observed := a.observedSessionTick(); observed > local {
			return observed
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(joinEpochPoll)
	}
}

// finishCatchUp reports what the join cost and refuses one it could not close.
func (a *App) finishCatchUp(held []*network.Message, caught uint64) error {
	remaining := a.sessionLagTicks()
	a.reportJoinLag(remaining)
	vlog.Info("app", "msg", "join caught up", "held_frames", len(held),
		"caught_up_ticks", caught, "tick", a.Position().Tick, "lag_ticks", remaining)
	a.telemetry.CatchUp.Store(int64(caught))
	if remaining > parameter.NetworkJoinLagTicks {
		return fmt.Errorf("join: still %d ticks behind the session after catching up, lead is %d",
			remaining, parameter.NetworkJoinLagTicks)
	}
	return nil
}

// joinCatchUpRounds is how many times a joining participant re-reads the session's
// tick while closing the gap. Each round drains what the previous one revealed;
// three is one more than the two windows a join actually has — the frames the gate
// held, and the frames the socket held while the world was being installed.
const joinCatchUpRounds = 3

// joinEpochWait is how long one catch-up round waits for the session's next epoch,
// and joinEpochPoll how often it looks. The wait is two tick intervals: a live
// session closes an epoch every tick, so anything longer is a session that has
// stopped rather than one this instance has not heard from yet.
const (
	joinEpochWait = 2 * parameter.GameUpdateInterval
	joinEpochPoll = time.Millisecond
)

// injectPort returns the attached endpoint as a frame injector. It is read from the
// world rather than from the service, because a harness attaches its own.
func (a *App) injectPort() (interface {
	Inject(peer uint32, msgType uint8, payload []byte)
}, error) {
	injector, ok := a.sessionTransport().(interface {
		Inject(peer uint32, msgType uint8, payload []byte)
	})
	if !ok {
		return nil, errors.New("join: the attached endpoint cannot replay the gate's held frames")
	}
	return injector, nil
}

// HostAddr is the address this run is hosting on, empty when it is not hosting one
// it opened itself. The bound form is reported, so a run opened on port zero names
// the port it actually got.
func (a *App) HostAddr() string {
	a.sessionMu.Lock()
	port := a.midRunPort
	a.sessionMu.Unlock()
	if port == nil {
		return ""
	}
	if bound := port.Addr(); bound != nil {
		return bound.String()
	}
	return a.cfg.HostAddress
}

// newestHeldEpoch is the highest production epoch among the frames the gate held.
// A frame that does not decode is skipped: the barrier will count it as a drop when
// it drains the same bytes, and guessing a tick from a broken one is worse than
// reading a smaller gap and measuring what is left.
func newestHeldEpoch(held []*network.Message) uint64 {
	var newest uint64
	for _, msg := range held {
		if msg.Type != network.MsgEvent {
			continue
		}
		batch, err := event.DecodeWireBatch(msg.Payload)
		if err != nil {
			continue
		}
		if batch.ProducedTick > newest {
			newest = batch.ProducedTick
		}
	}
	return newest
}

// observedSessionTick is the newest production epoch any peer has been seen
// closing. Every tick closes one, empty or not, so it is the session's tick as far
// as this instance's barrier has observed it.
func (a *App) observedSessionTick() uint64 {
	var newest uint64
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if e, ok := sys.(interface{ NewestPeerEpoch() uint64 }); ok {
				if n := e.NewestPeerEpoch(); n > newest {
					newest = n
				}
			}
		}
	})
	return newest
}

// sessionLagTicks is how far behind the observed session tick this instance stands.
func (a *App) sessionLagTicks() uint64 {
	newest := a.observedSessionTick()
	local := a.Position().Tick
	if newest <= local {
		return 0
	}
	return newest - local
}

// reportJoinLag publishes the measured lag beside the other transport counters.
func (a *App) reportJoinLag(ticks uint64) {
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if r, ok := sys.(interface{ ReportJoinLag(uint64) }); ok {
				r.ReportJoinLag(ticks)
			}
		}
	})
}

// closeMidRunPort releases a socket this run opened for itself.
func (a *App) closeMidRunPort() {
	a.sessionMu.Lock()
	port := a.midRunPort
	a.midRunPort = nil
	a.sessionMu.Unlock()
	if port != nil {
		_ = port.Close()
	}
}
