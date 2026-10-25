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
	link "github.com/lixenwraith/vif/pkg/network"
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
	offers  map[link.PeerID]network.SessionOffer
	reports map[link.PeerID]network.JoinerReport
}

// serveRelay opens the address -relay named, once this run is in a session: a
// person who cannot reach the host joins there and this run carries its traffic.
func (m *membership) serveRelay() {
	if m.cfg.RelayAddress == "" || m.cfg.JoinAddress == "" {
		return
	}
	port, err := m.socketPort()
	if err != nil {
		return
	}
	bound, err := port.ServeSession(m.cfg.RelayAddress, m.relayAcceptor(m.admissions.Admit), m.releaseRelayedJoiner)
	if err != nil {
		m.log.Warn("session", "msg", "relay address not bound", "address", m.cfg.RelayAddress, "error", err.Error())
		m.ctx.SetStatusMessage("Relay: "+err.Error(), parameter.StatusAlert)
		return
	}
	m.log.Info("session", "msg", "relaying joins", "address", bound)
	m.ctx.SetStatusMessage("Relaying joins at "+bound, parameter.StatusNotice)
}

// relayAcceptor is the join handshake this run serves for its bots and for the
// people it relays, at its -relay door and, when its player relays for others, at
// its own port (admitHosted): a coordinator's, with the authority asked for each
// answer. Once this run holds the term it is that coordinator and the door admits
// as the host's does (relayDoorHosts), read at each step since the term moves.
func (m *membership) relayAcceptor(admit func(net.Addr) error) func(net.Conn) (link.PeerID, error) {
	local := func() link.PeerID { return link.PeerID(m.localParticipant()) }
	return network.HostAcceptor(network.Coordinator{
		Preview: func(claims []string) (network.SessionOffer, error) {
			if m.relayDoorHosts() {
				return m.previewParticipant(claims)
			}
			return m.relayPreview(local())
		},
		Assign: func(claims []string) (network.SessionOffer, error) {
			if m.relayDoorHosts() {
				return m.assignParticipant(claims)
			}
			return m.relayAssign(local(), claims)
		},
		Release: func(id link.PeerID) {
			if m.relayDoorHosts() {
				m.releaseParticipant(id)
				return
			}
			m.relayRelease(id)
		},
		Admit: admit,
		Report: func(id link.PeerID, r network.JoinerReport) error {
			if m.relayDoorHosts() {
				return m.noteJoinerReport(id, r)
			}
			return m.relayReport(local(), id, r)
		},
		Prepared:    m.showJoinerPrepared,
		Name:        m.cfg.SessionName,
		Scenario:    m.scenarioBody,
		PressWindow: parameter.NetworkJoinPressWindow,
	}, network.DefaultConfig().ConnectTimeout)
}

// relayDoorHosts reports that the run behind the -relay door authors its session, so
// the door is the host's: a guest that took the session over has no authority to
// ask, and its own port already takes joins as the host's did.
func (m *membership) relayDoorHosts() bool {
	return m.authority != nil && m.authority.State().Authoring()
}

// relayPreview is what a dial through this run is shown while it prepares: the
// session as this run follows it, naming nobody, with this run as the far end. The
// authority is asked nothing until the seat, which is where it allocates and refuses.
func (m *membership) relayPreview(local link.PeerID) (network.SessionOffer, error) {
	if m.authority != nil && m.authority.Migrating() {
		return network.SessionOffer{}, ErrSessionHandoff
	}
	anchor := m.JoinAnchor()
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	o := m.offerLocked(anchor, 0)
	// What the authority's offers say, which this run's own carried: it does not
	// keep the roster, nor set the policy.
	o.Roster = slices.Clone(m.sessionOffer.Roster)
	o.FixedAuthority, o.Players, o.Relay = m.sessionOffer.FixedAuthority, m.sessionOffer.Players, local
	return o, nil
}

// relayAssign asks the authority for an identity this run holds, or the place one of
// the joiner's claims takes back, and names this run as the far end of its stream.
func (m *membership) relayAssign(local link.PeerID, claims []string) (network.SessionOffer, error) {
	ans, err := m.askAuthority(network.RelayJoin{Step: network.RelayAssign, Claims: claims})
	if err != nil {
		return network.SessionOffer{}, err
	}
	offer := ans.Offer
	offer.Relay = local
	d := &m.relay
	d.mu.Lock()
	if d.offers == nil {
		d.offers = make(map[link.PeerID]network.SessionOffer)
	}
	d.offers[offer.Assigned] = offer
	d.mu.Unlock()
	return offer, nil
}

// relayReport takes what a joiner said of itself: this run's own bot, advertising
// nothing, or a person, kept for the authority to admit it on.
func (m *membership) relayReport(local, id link.PeerID, r network.JoinerReport) error {
	if r.Holder != 0 && (r.Holder != local || r.Listen != "") {
		return errors.New("a bot joins through its own holder, and advertises no listener")
	}
	d := &m.relay
	d.mu.Lock()
	if d.reports == nil {
		d.reports = make(map[link.PeerID]network.JoinerReport)
	}
	d.reports[id] = r
	d.mu.Unlock()
	return nil
}

// relayRelease returns an identity whose handshake failed.
func (m *membership) relayRelease(id link.PeerID) {
	m.relay.mu.Lock()
	delete(m.relay.offers, id)
	delete(m.relay.reports, id)
	m.relay.mu.Unlock()
	m.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: id})
}

// releaseRelayedJoiner completes the gate for a participant the relay acceptor
// admitted, the host's own gate once this run hosts. It runs on the handshake's
// goroutine, as the coordinator's gate does.
func (m *membership) releaseRelayedJoiner(id link.PeerID) {
	if m.relayDoorHosts() {
		m.admitLateJoiner(id)
		return
	}
	m.relay.mu.Lock()
	offer, report := m.relay.offers[id], m.relay.reports[id]
	delete(m.relay.offers, id)
	delete(m.relay.reports, id)
	m.relay.mu.Unlock()
	port, err := m.socketPort()
	if err == nil {
		err = m.sendRelayedGate(port, offer, report)
	}
	if err != nil {
		m.log.Warn("session", "msg", "relayed join failed", "peer", id, "error", err.Error())
		if port != nil {
			port.Disconnect(uint32(id))
		}
		m.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: id})
	}
}

// sendRelayedGate sends a joiner the world this run has proved the authority's, and
// has the authority cross its arrival once it has installed it.
func (m *membership) sendRelayedGate(port *link.SocketPort, offer network.SessionOffer, report network.JoinerReport) error {
	id := offer.Assigned
	tick, bytes, _, err := m.gateJoiner(port, offer, false, m.corrections.ProvedWorld)
	if err != nil {
		return err
	}
	if _, err := m.askAuthority(network.RelayJoin{Step: network.RelayReady, Leaf: id, Report: report}); err != nil {
		return err
	}
	m.log.Info("session", "msg", "relayed participant admitted",
		"peer", id, "snapshot_tick", tick, "bytes", bytes)
	return nil
}

// askAuthority makes one round trip to the authority on this run's link to it. A run
// with no such link is mid-succession, which the joiner retries past.
func (m *membership) askAuthority(req network.RelayJoin) (network.RelayAdmit, error) {
	d := &m.relay
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
	if !m.tellAuthority(req) {
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
func (m *membership) tellAuthority(req network.RelayJoin) bool {
	port, err := m.socketPort()
	if err != nil {
		return false
	}
	body, err := json.Marshal(req)
	return err == nil && port.Send(uint32(m.authorityID()), uint8(network.MsgRelayJoin), body)
}

// onRelayJoin takes one relayed-admission frame off the tick: a holder's question
// when this run is the coordinator, the coordinator's answer when it holds bots.
// Caller MUST hold updateMutex.
func (m *membership) onRelayJoin(kind uint8, from uint32, body []byte) {
	switch link.MessageType(kind) {
	case network.MsgRelayAdmit:
		m.receiveRelayAdmit(link.PeerID(from), body)
	case network.MsgRelayJoin:
		m.decideRelayJoinLocked(link.PeerID(from), body)
	}
}

// receiveRelayAdmit wakes the round trip an answer belongs to. An identity assigned
// to a join that stopped waiting goes back to the authority.
func (m *membership) receiveRelayAdmit(from link.PeerID, body []byte) {
	var ans network.RelayAdmit
	if json.Unmarshal(body, &ans) != nil || from != m.authorityID() {
		return
	}
	d := &m.relay
	d.mu.Lock()
	waiting := d.answer != nil && ans.Nonce == d.nonce
	if waiting {
		d.answer <- ans
		d.answer = nil
	}
	d.mu.Unlock()
	if !waiting && ans.Offer.Assigned != 0 {
		m.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: ans.Offer.Assigned})
	}
}

// decideRelayJoinLocked answers one step a holder asks on its own link. Caller MUST
// hold updateMutex.
func (m *membership) decideRelayJoinLocked(relay link.PeerID, body []byte) {
	var req network.RelayJoin
	if err := json.Unmarshal(body, &req); err != nil {
		m.log.Warn("session", "msg", "relayed join step unreadable", "relay", relay, "error", err.Error())
		return
	}
	coordinator := m.world.IsSessionCoordinator()
	if req.Step == network.RelayAbandon {
		if coordinator {
			m.abandonRelayedLocked(relay, req.Leaf)
		}
		return
	}
	ans := network.RelayAdmit{Nonce: req.Nonce}
	var err error
	switch {
	case !coordinator:
		err = ErrSessionHandoff
	case req.Step == network.RelayAssign && !m.lateJoins.Load():
		err = ErrSessionStarting
	case req.Step == network.RelayAssign:
		ans.Offer, err = m.assignFor(m.joinAnchorLocked(), relay, req.Claims)
		m.closeStaleLink(err, m.sessionTransportLocked())
	case req.Step == network.RelayReady:
		err = m.admitRelayedLocked(relay, req.Leaf, req.Report)
	default:
		err = fmt.Errorf("relayed join step %d is not one this build takes", req.Step)
	}
	if err != nil {
		ans.Refuse(err)
	}
	out, _ := json.Marshal(ans) // plain data cannot fail to encode
	if r := m.world.Resources.Network; r != nil && r.Port != nil {
		r.Port.Send(uint32(relay), uint8(network.MsgRelayAdmit), out)
	}
}

// relayedSlot is the slot of a participant joining through relay, if it is one.
func (m *membership) relayedSlot(relay, leaf link.PeerID) (uint8, bool) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	i := slices.IndexFunc(m.sessionRoster, func(p network.RosterEntry) bool { return p.ID == leaf })
	if i < 0 || m.sessionRoster[i].Holder != relay {
		return 0, false
	}
	return m.sessionRoster[i].Slot, true
}

// admitRelayedLocked crosses the arrival of a participant that installed its relay's
// world. Its report settles what the assignment only guessed: a bot keeps the
// relay as its holder, a person holds nothing and may declare an address.
// Caller MUST hold updateMutex.
func (m *membership) admitRelayedLocked(relay, leaf link.PeerID, report network.JoinerReport) error {
	slot, ok := m.relayedSlot(relay, leaf)
	if !ok {
		return fmt.Errorf("participant %d is not joining through participant %d", leaf, relay)
	}
	if err := m.noteJoinerReport(leaf, report); err != nil {
		return err
	}
	m.crossArrivalLocked(leaf, slot)
	m.log.Info("session", "msg", "relayed participant admitted", "peer", leaf, "relay", relay, "slot", slot)
	return nil
}

// abandonRelayedLocked retires a relayed join that failed. Whether its arrival
// crossed is the holder's guess, so it is dismissed as any participant can be: a
// departure for a cursor never made removes nothing. A place taken back whose
// arrival has not crossed goes back to its Wraith. Caller MUST hold updateMutex.
func (m *membership) abandonRelayedLocked(relay, leaf link.PeerID) {
	if slot, ok := m.relayedSlot(relay, leaf); ok && !m.giveBack(leaf) {
		m.pushDepartureLocked(event.ParticipantDepartedPayload{Participant: uint32(leaf), Slot: slot, Dismissed: true},
			core.DomainPlayer)
	}
}
