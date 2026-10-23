package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// relayDesk is a guest's half of admitting participants through its own link: its
// bots, which share its fate, and people who cannot reach the host, who borrow its
// uplink. The guest serves a world it proved the authority's; the coordinator still
// allocates and crosses the arrival, asked on the guest's link one question at a
// time.
type relayDesk struct {
	turn sync.Mutex // held for one whole round trip

	mu      sync.Mutex
	nonce   uint32
	answer  chan network.RelayAdmit // the round trip in flight, nil when none
	offers  map[network.PeerID]network.SessionOffer
	reports map[network.PeerID]network.JoinerReport
}

// serveRelay opens the address -relay named, once this run is in a session: a
// person who cannot reach the host joins there and this run carries its traffic.
func (a *App) serveRelay() {
	if a.cfg.RelayAddress == "" || a.cfg.JoinAddress == "" {
		return
	}
	port, err := a.socketPort()
	if err != nil {
		return
	}
	local := network.PeerID(a.localParticipant())
	bound, err := port.ServeSession(a.cfg.RelayAddress, a.relayAcceptor(local), a.releaseRelayedJoiner)
	if err != nil {
		a.log.Warn("session", "msg", "relay address not bound", "address", a.cfg.RelayAddress, "error", err.Error())
		a.ctx.SetStatusMessage("Relay: "+err.Error(), parameter.StatusAlert)
		return
	}
	a.log.Info("session", "msg", "relaying joins", "address", bound)
	a.ctx.SetStatusMessage("Relaying joins at "+bound, parameter.StatusNotice)
}

// relayAcceptor is the join handshake this run serves for its bots and for the
// people it relays, as a coordinator's with the authority asked for each answer.
func (a *App) relayAcceptor(local network.PeerID) func(net.Conn) (network.PeerID, error) {
	return network.HostAcceptor(network.Coordinator{
		Preview:     func([]string) (network.SessionOffer, error) { return a.relayPreview(local) },
		Assign:      func(claims []string) (network.SessionOffer, error) { return a.relayAssign(local, claims) },
		Release:     a.relayRelease,
		Admit:       a.admissions.Admit,
		Report:      func(id network.PeerID, r network.JoinerReport) error { return a.relayReport(local, id, r) },
		Name:        a.cfg.SessionName,
		Scenario:    a.scenarioBody,
		PressWindow: parameter.NetworkJoinPressWindow,
	}, network.DefaultConfig().ConnectTimeout)
}

// relayPreview is what a dial through this run is shown while it prepares: the
// session as this run follows it, naming nobody, with this run as the far end. The
// authority is asked nothing until the seat, which is where it allocates and refuses.
func (a *App) relayPreview(local network.PeerID) (network.SessionOffer, error) {
	if a.authority != nil && a.authority.Migrating() {
		return network.SessionOffer{}, ErrSessionHandoff
	}
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	o := a.offerLocked(anchor, 0)
	// What the authority's offers say, which this run's own carried: it does not
	// keep the roster, nor set the policy.
	o.Roster = slices.Clone(a.sessionOffer.Roster)
	o.FixedAuthority, o.Players, o.Relay = a.sessionOffer.FixedAuthority, a.sessionOffer.Players, local
	return o, nil
}

// relayAssign asks the authority for an identity this run holds, or the place one of
// the joiner's claims takes back, and names this run as the far end of its stream.
func (a *App) relayAssign(local network.PeerID, claims []string) (network.SessionOffer, error) {
	ans, err := a.askAuthority(network.RelayJoin{Step: network.RelayAssign, Claims: claims})
	if err != nil {
		return network.SessionOffer{}, err
	}
	offer := ans.Offer
	offer.Relay = local
	d := &a.relay
	d.mu.Lock()
	if d.offers == nil {
		d.offers = make(map[network.PeerID]network.SessionOffer)
	}
	d.offers[offer.Assigned] = offer
	d.mu.Unlock()
	return offer, nil
}

// relayReport takes what a joiner said of itself: this run's own bot, advertising
// nothing, or a person, kept for the authority to admit it on.
func (a *App) relayReport(local, id network.PeerID, r network.JoinerReport) error {
	if r.Holder != 0 && (r.Holder != local || r.Listen != "") {
		return errors.New("a bot joins through its own holder, and advertises no listener")
	}
	d := &a.relay
	d.mu.Lock()
	if d.reports == nil {
		d.reports = make(map[network.PeerID]network.JoinerReport)
	}
	d.reports[id] = r
	d.mu.Unlock()
	return nil
}

// relayRelease returns an identity whose handshake failed.
func (a *App) relayRelease(id network.PeerID) {
	a.relay.mu.Lock()
	delete(a.relay.offers, id)
	delete(a.relay.reports, id)
	a.relay.mu.Unlock()
	a.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: id})
}

// releaseRelayedJoiner completes the gate for a bot the relay acceptor admitted. It
// runs on the handshake's goroutine, as the coordinator's gate does.
func (a *App) releaseRelayedJoiner(id network.PeerID) {
	a.relay.mu.Lock()
	offer, report := a.relay.offers[id], a.relay.reports[id]
	delete(a.relay.offers, id)
	delete(a.relay.reports, id)
	a.relay.mu.Unlock()
	port, err := a.socketPort()
	if err == nil {
		err = a.sendRelayedGate(port, offer, report)
	}
	if err != nil {
		a.log.Warn("session", "msg", "relayed join failed", "peer", id, "error", err.Error())
		if port != nil {
			port.Disconnect(uint32(id))
		}
		a.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: id})
	}
}

// sendRelayedGate sends a joiner the world this run has proved the authority's, and
// has the authority cross its arrival once it has installed it.
func (a *App) sendRelayedGate(port *network.SocketPort, offer network.SessionOffer, report network.JoinerReport) error {
	id := offer.Assigned
	tick, bytes, _, err := a.gateJoiner(port, offer, false, a.corrections.ProvedWorld)
	if err != nil {
		return err
	}
	if _, err := a.askAuthority(network.RelayJoin{Step: network.RelayReady, Leaf: id, Report: report}); err != nil {
		return err
	}
	a.log.Info("session", "msg", "relayed participant admitted",
		"peer", id, "snapshot_tick", tick, "bytes", bytes)
	return nil
}

// askAuthority makes one round trip to the authority on this run's link to it. A run
// with no such link is mid-succession, which the joiner retries past.
func (a *App) askAuthority(req network.RelayJoin) (network.RelayAdmit, error) {
	d := &a.relay
	d.turn.Lock()
	defer d.turn.Unlock()
	answer := make(chan network.RelayAdmit, 1)
	d.mu.Lock()
	d.nonce++
	req.Nonce, d.answer = d.nonce, answer
	d.mu.Unlock()
	stop := func() {
		d.mu.Lock()
		d.answer = nil
		d.mu.Unlock()
	}
	if !a.tellAuthority(req) {
		stop()
		return network.RelayAdmit{}, ErrSessionHandoff
	}
	timer := time.NewTimer(parameter.NetworkJoinReadyTimeout) // [wall] a link bound
	defer timer.Stop()
	var ans network.RelayAdmit
	select {
	case ans = <-answer:
	case <-timer.C:
		stop()
		select {
		case ans = <-answer: // answered as the wait ran out
		default:
			return ans, errors.New("the session's authority did not answer a relayed join in time")
		}
	}
	if err := ans.Refused(); err != nil {
		return ans, err
	}
	return ans, nil
}

// tellAuthority sends one step to the authority without waiting for an answer.
func (a *App) tellAuthority(req network.RelayJoin) bool {
	port, err := a.socketPort()
	if err != nil {
		return false
	}
	body, err := json.Marshal(req)
	return err == nil && port.Send(uint32(a.authorityID()), uint8(network.MsgRelayJoin), body)
}

// onRelayJoin takes one relayed-admission frame off the tick: a holder's question
// when this run is the coordinator, the coordinator's answer when it holds bots.
// Caller MUST hold updateMutex.
func (a *App) onRelayJoin(kind uint8, from uint32, body []byte) {
	switch network.MessageType(kind) {
	case network.MsgRelayAdmit:
		a.receiveRelayAdmit(network.PeerID(from), body)
	case network.MsgRelayJoin:
		a.decideRelayJoinLocked(network.PeerID(from), body)
	}
}

// receiveRelayAdmit wakes the round trip an answer belongs to. An identity assigned
// to a join that stopped waiting goes back to the authority.
func (a *App) receiveRelayAdmit(from network.PeerID, body []byte) {
	var ans network.RelayAdmit
	if json.Unmarshal(body, &ans) != nil || from != a.authorityID() {
		return
	}
	d := &a.relay
	d.mu.Lock()
	waiting := d.answer != nil && ans.Nonce == d.nonce
	if waiting {
		d.answer <- ans
		d.answer = nil
	}
	d.mu.Unlock()
	if !waiting && ans.Offer.Assigned != 0 {
		a.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: ans.Offer.Assigned})
	}
}

// decideRelayJoinLocked answers one step a holder asks on its own link. Caller MUST
// hold updateMutex.
func (a *App) decideRelayJoinLocked(relay network.PeerID, body []byte) {
	var req network.RelayJoin
	if err := json.Unmarshal(body, &req); err != nil {
		a.log.Warn("session", "msg", "relayed join step unreadable", "relay", relay, "error", err.Error())
		return
	}
	coordinator := a.world.IsSessionCoordinator()
	if req.Step == network.RelayAbandon {
		if coordinator {
			a.abandonRelayedLocked(relay, req.Leaf)
		}
		return
	}
	ans := network.RelayAdmit{Nonce: req.Nonce}
	var err error
	switch {
	case !coordinator:
		err = ErrSessionHandoff
	case req.Step == network.RelayAssign && !a.lateJoins.Load():
		err = ErrSessionStarting
	case req.Step == network.RelayAssign:
		ans.Offer, err = a.assignFor(a.joinAnchorLocked(), relay, req.Claims)
		a.closeStaleLink(err, a.sessionTransportLocked())
	case req.Step == network.RelayReady:
		err = a.admitRelayedLocked(relay, req.Leaf, req.Report)
	default:
		err = fmt.Errorf("relayed join step %d is not one this build takes", req.Step)
	}
	if err != nil {
		ans.Refuse(err)
	}
	out, _ := json.Marshal(ans) // plain data cannot fail to encode
	if r := a.world.Resources.Network; r != nil && r.Port != nil {
		r.Port.Send(uint32(relay), uint8(network.MsgRelayAdmit), out)
	}
}

// relayedSlot is the slot of a participant joining through relay, if it is one.
func (a *App) relayedSlot(relay, leaf network.PeerID) (uint8, bool) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == leaf })
	if i < 0 || a.sessionRoster[i].Holder != relay {
		return 0, false
	}
	return a.sessionRoster[i].Slot, true
}

// admitRelayedLocked crosses the arrival of a participant that installed its relay's
// world. Its report settles what the assignment only guessed: a bot keeps the
// relay as its holder, a person holds nothing and may declare an address.
// Caller MUST hold updateMutex.
func (a *App) admitRelayedLocked(relay, leaf network.PeerID, report network.JoinerReport) error {
	slot, ok := a.relayedSlot(relay, leaf)
	if !ok {
		return fmt.Errorf("participant %d is not joining through participant %d", leaf, relay)
	}
	if err := a.noteJoinerReport(leaf, report); err != nil {
		return err
	}
	a.crossArrivalLocked(leaf, slot)
	a.log.Info("session", "msg", "relayed participant admitted", "peer", leaf, "relay", relay, "slot", slot)
	return nil
}

// abandonRelayedLocked retires a relayed join that failed. Whether its arrival
// crossed is the holder's guess, so it is dismissed as any participant can be: a
// departure for a cursor never made removes nothing. A place taken back whose
// arrival has not crossed goes back to its Wraith. Caller MUST hold updateMutex.
func (a *App) abandonRelayedLocked(relay, leaf network.PeerID) {
	if slot, ok := a.relayedSlot(relay, leaf); ok && !a.giveBack(leaf) {
		a.pushDepartureLocked(event.ParticipantDepartedPayload{Participant: uint32(leaf), Slot: slot, Dismissed: true},
			core.DomainPlayer)
	}
}
