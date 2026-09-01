// Package app: hosting a run that is already going.
//
// The startup lobby freezes tick zero until a fixed roster has arrived, which is
// the only join the session protocol used to have. This file is the other one: an
// instance that is already playing opens a socket, and a participant that dials it
// receives the world rather than reproducing it.
//
// The ordering is the whole design and it is not the obvious one. A joiner is
// admitted as a peer *before* the world is read for it, so the crossings this
// instance produces during the transfer reach it instead of falling into the gap
// between the capture and the admission; the joiner holds them until the world they
// apply to exists, and the barrier discards the ones the capture already contains.
// The alternative — read the world, then admit — loses every artifact produced in
// between, silently, which is the failure this whole plan exists to stop having.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// BeginHosting opens a running instance to participants.
//
// It is the same session every other path builds — the same acceptor, the same
// identity allocation, the same capture — started at a tick that is not zero. What
// it adds is the transport, because a solo run has none: the port is created,
// started and attached here, and this App owns it for the rest of the run.
func (a *App) BeginHosting(addr string) error {
	if addr == "" {
		return errors.New("host: no address")
	}
	if a.sessionTransport() != nil {
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

	a.cfg.HostAddress = addr
	netCfg := a.hostNetworkConfig()
	netCfg.OnAdmit = a.releaseMidRunJoiner
	port := network.NewSocketPort(netCfg)
	if err := port.Start(); err != nil {
		a.cfg.HostAddress = ""
		return fmt.Errorf("host %s: %w", addr, err)
	}

	a.sessionMu.Lock()
	a.midRunPort = port
	a.sessionRoster = []network.SessionParticipant{{ID: hostParticipantID, Slot: 0}}
	a.sessionMu.Unlock()

	// AttachTransport latches the world as shared (D-14) and installs the departure
	// and digest hooks; activating closes the pre-session crossing window so this
	// instance's own artifacts start taking the session's playout lead.
	a.AttachTransport(port)
	a.activateNetworkSession()

	bound := addr
	if b := port.Addr(); b != nil {
		bound = b.String()
	}
	vlog.Info("app", "msg", "hosting opened mid-run",
		"address", bound, "tick", a.Position().Tick, "participants", a.remoteParticipantCount()+1)
	a.ctx.SetStatusMessage("Hosting on "+bound, 0, false)
	return nil
}

// sessionTransport returns the attached endpoint, nil when this run has none. The
// question is whether a transport exists, not whether a peer is on it: a host
// waiting alone is in a session and a second :host would open a second one.
func (a *App) sessionTransport() engine.NetworkPort {
	var port engine.NetworkPort
	a.world.RunSafe(func() {
		if r := a.world.Resources.Network; r != nil {
			port = r.Port
		}
	})
	return port
}

// SessionSummary is a one-line description of what this run is part of.
func (a *App) SessionSummary() string {
	if a.sessionTransport() == nil {
		return "Solo run; :host <addr> opens it to participants"
	}
	var peers int64
	var participant uint32
	a.world.RunSafe(func() {
		peers = a.world.Resources.Status.Ints.Get("network.peers").Load()
		participant = a.localParticipantLocked()
	})
	addr := a.cfg.HostAddress
	role := "host"
	if a.cfg.JoinAddress != "" {
		addr, role = a.cfg.JoinAddress, "guest"
	}
	return fmt.Sprintf("Session %s %s, participant %d, %d peer(s), tick %d",
		role, addr, participant, peers, a.Position().Tick)
}

// releaseMidRunJoiner completes the gate for a participant the accept loop has
// just admitted. It runs on the accept goroutine, so it must not assume the world
// lock is free and must not hold it longer than one capture.
//
// A tick-zero lobby does not come through here: it closes on a roster and releases
// everyone together, and its gate is startHostSessionOn's.
func (a *App) releaseMidRunJoiner(id network.PeerID) {
	a.sessionMu.Lock()
	port := a.midRunPort
	a.sessionMu.Unlock()
	if port == nil {
		return
	}
	if err := a.sendMidRunGate(port, id); err != nil {
		vlog.Warn("app", "msg", "mid-run join failed", "participant", id, "error", err.Error())
		a.releaseParticipant(id)
		return
	}
}

// sendMidRunGate sends one joiner the closed roster and the world it names, then
// crosses its arrival.
func (a *App) sendMidRunGate(port *network.SocketPort, id network.PeerID) error {
	offer, err := a.midRunOffer(id)
	if err != nil {
		return err
	}

	// Read before the gate is sent: ReadyCount is cumulative over the session, so
	// what this join waits for is an increase rather than a value.
	ready := port.ReadyCount()

	body, tick, err := a.encodeJoinCapture()
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
	for i, chunk := range chunks {
		if !port.Send(uint32(id), uint8(network.MsgStateSnapshot), chunk) {
			return fmt.Errorf("could not send capture chunk %d/%d", i+1, len(chunks))
		}
	}
	if err := a.awaitJoinerReady(port, id, ready); err != nil {
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
		if port.PeerCount() == 0 {
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

// crossParticipantArrival announces a mid-run arrival as a D-3 crossing.
//
// It is not a local reaction to a connect and cannot be: the cursor it creates is a
// shared entity, so every instance has to create it at one agreed tick or their
// shared creation order diverges from that point on (D-11). The coordinator is the
// only producer, for the same reason it is the only producer of a departure.
func (a *App) crossParticipantArrival(id network.PeerID, slot uint8) {
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventParticipantJoined,
			&event.ParticipantJoinedPayload{Participant: uint32(id), Slot: slot},
			event.OriginSession, core.DomainPlayer)
	})
}

// resumeJoinedSession is the joiner's half of the ordering: the session traffic
// the gate held is handed to the port, and the gap between the world this instance
// installed and the tick the session has reached is closed by simulating it.
//
// The gap is real and it is not an error. A capture is read at tick T and installed
// some milliseconds later, by which time the session is at T+k; k is the transfer
// and the install, so it is a function of world size and link speed rather than of
// how long the session has been running. Left open it would be permanent, and a
// participant k ticks behind produces every crossing k ticks late — under the
// playout lead that is still on time, over it the session diverges from the first
// artifact this participant sends. So the gap is closed by running those k ticks
// here, with the held artifacts applying at the ticks they name, before this
// instance's own clock starts.
//
// Call after the transport has taken the stream and before game time is released.
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

	// The gap is real and it is not an error, but it is only partly readable from
	// what the gate held. Epochs the host closed while this instance was reading its
	// world land in the held set; epochs it closed during the install sat in the
	// socket until the port started, and the barrier only learns of those once a
	// tick drains them. So the gap is closed by rounds: catch up to the newest tick
	// known so far, let that draining reveal the next, and stop when it stops moving.
	//
	// A tick-zero lobby has no gap by construction — the host is frozen at tick zero
	// until every participant is ready, so it has produced nothing — and running a
	// probe tick there would put this instance one tick ahead of a session that has
	// not started.
	if !a.sessionOffer.CarriesSnapshot() || a.sessionOffer.SnapshotTick == 0 {
		a.reportJoinLag(0)
		return nil
	}

	// Held epochs are only part of the gap: the ones the host closed while this
	// instance was staging and committing sat in the socket, not on this stream. So
	// each round drains the transport without ticking, waits briefly for the next
	// epoch if none has arrived, and then runs exactly the ticks that names. The
	// host is still advancing while the catch-up runs, which is what the second and
	// third rounds are for.
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
	a.snapshotTelemetry.catchUp.Store(int64(caught))
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
