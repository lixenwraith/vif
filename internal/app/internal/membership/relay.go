package membership

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// relayDesk is a guest's half of admitting participants through its own link: its
// bots, which share its fate, and people who cannot reach the host, who borrow its
// uplink. The guest serves a world it proved the authority's; the coordinator still
// allocates and crosses the arrival, asked up the guest's path to it one question
// at a time.
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
func (m *Membership) serveRelay() {
	if m.cfg.RelayAddress == "" || m.cfg.JoinAddress == "" {
		return
	}
	port, err := m.socketPort()
	if err != nil {
		return
	}
	if router, ok := config.RelayRouter(m.cfg.RelayAddress); ok {
		m.publishDoor(router)
		return
	}
	bound, err := port.ServeSession(m.cfg.RelayAddress, m.relayAcceptor(m.admissions.Admit, true), m.releaseRelayedJoiner)
	if err != nil {
		m.log.Warn("session", "msg", "relay address not bound", "address", m.cfg.RelayAddress, "error", err.Error())
		m.ctx.SetStatusMessage("Relay: "+err.Error(), parameter.StatusAlert)
		return
	}
	m.tunnelMu.Lock()
	m.relayBound = bound
	m.tunnelMu.Unlock()
	m.log.Info("session", "msg", "relaying joins", "address", bound)
	m.ctx.SetStatusMessage("Relaying joins at "+bound, parameter.StatusNotice)
	// The chain names the door, so a run with no path to its authority finds it.
	m.tellAuthority(network.RelayJoin{Step: network.RelayDoor, Door: bound})
}

// publishDoor publishes this run's door through a router (network.DoorTunnel): each
// connection the router opens back runs the door's handshake as a stream the relay
// listener took would, on the port the run holds then (doorHandshake), and the link
// the router names is the door the chain carries.
func (m *Membership) publishDoor(router string) {
	ctx, cancel := context.WithCancel(context.Background())
	m.tunnelMu.Lock()
	if m.tunnelStop != nil {
		m.tunnelStop()
	}
	m.tunnelStop = cancel
	m.tunnelMu.Unlock()
	t := &network.DoorTunnel{Router: router, Session: m.cfg.SessionName}
	core.Go(func() {
		t.Run(ctx, func(door string) {
			m.log.Info("session", "msg", "relaying joins through a router", "router", router, "door", door)
			m.ctx.SetStatusMessage("Relaying joins at "+door, parameter.StatusNotice)
			m.tellAuthority(network.RelayJoin{Step: network.RelayDoor, Door: door})
		}, func(conn net.Conn) {
			port, accept := m.doorHandshake()
			if port == nil || port.ServeConn(conn, accept, m.releaseRelayedJoiner) != nil {
				_ = conn.Close()
			}
		}, func(err error) {
			m.log.Warn("session", "msg", "router door lost", "router", router, "error", err.Error())
		})
	})
}

// CloseDoor ends the door this run publishes through a router, if any, and whatever
// the -relay door took back for a fork: the session it served is being left.
func (m *Membership) CloseDoor() {
	m.tunnelMu.Lock()
	defer m.tunnelMu.Unlock()
	if m.tunnelStop != nil {
		m.tunnelStop()
		m.tunnelStop = nil
	}
	m.relayBound, m.returnDoor = "", nil
}

// doorHandshake is the port a connection at the -relay door joins and the
// handshake it runs there: the relay's, or once a pinned fork kept the people it
// relayed, the one that takes them back (returnAcceptor). Nil with no port.
func (m *Membership) doorHandshake() (*link.SocketPort, func(net.Conn) (link.PeerID, error)) {
	port := m.joinPort()
	if port == nil {
		return nil, nil
	}
	m.tunnelMu.Lock()
	name := m.returnDoor
	m.tunnelMu.Unlock()
	if name != nil {
		return port, m.returnAcceptor(*name)
	}
	return port, m.relayAcceptor(m.admissions.Admit, true)
}

// reopenRelayDoor moves the -relay door to the session a pinned fork goes on in, for
// the people it relayed and kept the places of, who redial it once their links here
// go and take those places back (returnAcceptor). It runs before the port it stood on
// closes, which drops those links, so it is open at their first dial; one a router
// publishes stays published, and takes them on whichever port the run holds.
func (m *Membership) reopenRelayDoor(old engine.NetworkPort) error {
	m.tunnelMu.Lock()
	bound, name, tunnel := m.relayBound, m.returnDoor, m.tunnelStop != nil
	m.relayBound = ""
	m.tunnelMu.Unlock()
	if tunnel || bound == "" || name == nil {
		return nil
	}
	if p, ok := old.(interface{ CloseSession(string) bool }); ok {
		p.CloseSession(bound)
	}
	err := errors.New("the session it goes on in has no socket port")
	if port := m.joinPort(); port != nil {
		bound, err = port.ServeSession(bound, m.returnAcceptor(*name), m.releaseRelayedJoiner)
	}
	if err != nil {
		m.log.Warn("session", "msg", "relay door not kept for the players it relayed", "error", err.Error())
		m.CloseDoor() // nobody comes back through it, so the session keeps it for nobody
		return err
	}
	m.tunnelMu.Lock()
	m.relayBound = bound
	m.tunnelMu.Unlock()
	m.log.Info("session", "msg", "relay door kept for the players this run relayed", "address", bound)
	return nil
}

// returnAcceptor is the -relay door of the session a pinned fork goes on in: the
// host's handshake, for a dial whose key holds the place of a person this run relayed
// into the session it left (reopenRelayDoor). It answers to that session's name, which
// they dial, and asks no passphrase: the key it proves is the place's. Nobody new is
// let in at it; :host opens a door that lets them in.
func (m *Membership) returnAcceptor(name string) func(net.Conn) (link.PeerID, error) {
	c := network.Coordinator{
		Preview: func(a network.Admission) (network.SessionOffer, error) {
			return m.previewSeat(a, seatReturning)
		},
		Assign: func(a network.Admission) (network.SessionOffer, error) {
			return m.assignWith(a, seatReturning)
		},
		Release:     m.releaseParticipant,
		Admit:       m.admissions.Admit,
		Report:      m.noteJoinerReport,
		Prepared:    m.showJoinerPrepared,
		Name:        name,
		Authority:   m.firstTermKey,
		Pass:        m.checkPass,
		Scenario:    m.scenarioBody,
		PressWindow: parameter.NetworkJoinPressWindow,
	}
	return network.HostAcceptor(c, network.DefaultConfig().ConnectTimeout)
}

// errNotReturning turns away a dial whose key holds no place in the session a pinned
// fork goes on in for the players its run relayed, or that only watches it: at its
// -relay door, or through one of theirs (relaySeatingLocked). It takes them back,
// and nobody new.
var errNotReturning = &network.Refusal{Code: network.RefusalUnknown,
	Reason: "this session takes back only the players its host relayed before its own host left"}

// relaySeatingLocked is whom a join this run's session takes through one of its
// players may seat: anyone, but in the one a pinned fork goes on in for the players
// it relayed a key's own place alone, theirs and their bots', as at its -relay door
// (returnAcceptor), until its player opens it to others with :host. Caller MUST hold
// updateMutex.
func (m *Membership) relaySeatingLocked() seating {
	if m.cfg.HostAddress != "" || !m.keepsRelayed() {
		return seatAnyone
	}
	return seatKeyed
}

// keepsRelayed reports that this run's session is the one a pinned fork went on in
// for the players it relayed, whose -relay door takes back them alone.
func (m *Membership) keepsRelayed() bool {
	m.tunnelMu.Lock()
	defer m.tunnelMu.Unlock()
	return m.returnDoor != nil
}

// keptPlayersLocked reports that such a session still holds a place of a player it
// relayed, linked or on the way back: what :join would drop, and a restart strand.
// Caller MUST hold updateMutex.
func (m *Membership) keptPlayersLocked() bool {
	local := link.PeerID(m.world.LocalParticipant())
	return m.keepsRelayed() && slices.ContainsFunc(m.worldRosterLocked(), func(p network.RosterEntry) bool {
		return p.ID != local && p.Holder == 0
	})
}

// newcomerDoors is every door port answers at but the -relay door a pinned fork
// kept, which lets nobody new in: the doors :host names and the LAN beacon offers.
func (m *Membership) newcomerDoors(port *link.SocketPort) []net.Addr {
	doors := port.Doors()
	m.tunnelMu.Lock()
	kept := m.relayBound
	if m.returnDoor == nil {
		kept = ""
	}
	m.tunnelMu.Unlock()
	if kept == "" {
		return doors
	}
	return slices.DeleteFunc(doors, func(d net.Addr) bool { return d.String() == kept })
}

// relayAcceptor is the join handshake this run serves for its bots, whose streams
// ask no credential, and at a door for the people it relays: its -relay door and,
// when its player relays for others, its own port (admitHosted). The authority is
// asked for each answer; once this run holds the term the door admits as the
// host's does (relayDoorHosts), read at each step since the term moves.
func (m *Membership) relayAcceptor(admit func(net.Addr) error, door bool) func(net.Conn) (link.PeerID, error) {
	local := func() link.PeerID { return link.PeerID(m.localParticipant()) }
	c := network.Coordinator{
		Preview: func(a network.Admission) (network.SessionOffer, error) {
			if m.relayDoorHosts() {
				return m.previewParticipant(a)
			}
			return m.relayPreview(local())
		},
		Assign: func(a network.Admission) (network.SessionOffer, error) {
			if m.relayDoorHosts() {
				return m.assignParticipant(a)
			}
			return m.relayAssign(local(), a)
		},
		Nonce: func() (string, error) {
			if m.relayDoorHosts() {
				return network.NewNonce(), nil
			}
			return m.relayNonce()
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
		Authority:   m.firstTermKey,
		Pass:        m.checkPass,
		Scenario:    m.scenarioBody,
		PressWindow: parameter.NetworkJoinPressWindow,
	}
	if door {
		c.Passphrase, c.Gated = m.passphraseDoor, m.gated
	}
	return network.HostAcceptor(c, network.DefaultConfig().ConnectTimeout)
}

// relayDoorHosts reports that the run behind the -relay door authors its session, so
// the door is the host's: a guest that took the session over has no authority to
// ask, and its own port already takes joins as the host's did.
func (m *Membership) relayDoorHosts() bool {
	return m.authority != nil && m.authority.State().Authoring()
}

// relayPreview is what a dial through this run is shown while it prepares: the
// session as this run follows it, naming nobody, with this run as the far end. The
// authority is asked nothing until the seat, which is where it allocates and refuses.
func (m *Membership) relayPreview(local link.PeerID) (network.SessionOffer, error) {
	if m.authority != nil && m.authority.Migrating() {
		return network.SessionOffer{}, errSessionHandoff
	}
	anchor := m.JoinAnchor()
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	o := handshake(m.offerLocked(anchor, 0))
	// What the authority's offers say, which this run's own carried: it does not
	// keep the roster, nor set the ceiling.
	o.Roster = slices.Clone(m.sessionOffer.Roster)
	o.Players, o.Relay = m.sessionOffer.Players, local
	return o, nil
}

// relayNonce asks the authority for the challenge the door's dial proves its keys
// over, so the proofs this run carries on are checked there and not here alone.
func (m *Membership) relayNonce() (string, error) {
	ans, err := m.askAuthority(network.RelayJoin{Step: network.RelayNonce})
	if err != nil {
		return "", err
	}
	if ans.Challenge == "" {
		return "", errors.New("the session's authority issued no challenge for a relayed join")
	}
	return ans.Challenge, nil
}

// relayAssign asks the authority for an identity this run holds, or the place one of
// the keys the joiner proved takes back, and names this run as the far end of its
// stream. The proofs and the challenge they answer travel whole.
func (m *Membership) relayAssign(local link.PeerID, a network.Admission) (network.SessionOffer, error) {
	ans, err := m.askAuthority(network.RelayJoin{Step: network.RelayAssign, Keys: a.Proofs, Pass: a.Shown, Challenge: a.Nonce})
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
func (m *Membership) relayReport(local, id link.PeerID, r network.JoinerReport) error {
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

// relaying reports a join this run carries to the authority for id, whose link is
// that join's whatever departure of the run it replaces applies meanwhile.
func (m *Membership) relaying(id link.PeerID) bool {
	m.relay.mu.Lock()
	defer m.relay.mu.Unlock()
	_, ok := m.relay.offers[id]
	return ok
}

// relayRelease returns an identity whose handshake failed.
func (m *Membership) relayRelease(id link.PeerID) {
	m.relay.mu.Lock()
	delete(m.relay.offers, id)
	delete(m.relay.reports, id)
	m.relay.mu.Unlock()
	m.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: id})
}

// releaseRelayedJoiner completes the gate for a participant the relay acceptor
// admitted, the host's own gate once this run hosts. It runs on the handshake's
// goroutine, as the coordinator's gate does.
func (m *Membership) releaseRelayedJoiner(id link.PeerID) {
	if m.relayDoorHosts() {
		m.admitLateJoiner(id)
		return
	}
	m.relay.mu.Lock()
	offer, report := m.relay.offers[id], m.relay.reports[id]
	delete(m.relay.reports, id)
	m.relay.mu.Unlock()
	// The authority's answer was a handshake's, naming no address: the gate carries
	// the chain as this run holds it, which is the authority's as last published.
	offer.Chain = m.sessionChain()
	port, err := m.socketPort()
	if err == nil {
		err = m.sendRelayedGate(port, offer, report)
	}
	m.relay.mu.Lock()
	delete(m.relay.offers, id) // kept through the gate, for a departure applying inside it (relaying)
	m.relay.mu.Unlock()
	if err != nil {
		m.log.Warn("session", "msg", "relayed join failed", "peer", id, "error", err.Error())
		if port != nil {
			port.Disconnect(uint32(id))
		}
		m.tellAuthority(network.RelayJoin{Step: network.RelayAbandon, Leaf: id})
	}
}

// sendRelayedGate sends a joiner the world this run has proved the authority's, the
// held tick's while the session stands held, and has the authority cross its arrival
// once it has installed it.
func (m *Membership) sendRelayedGate(port *link.SocketPort, offer network.SessionOffer, report network.JoinerReport) error {
	id := offer.Assigned
	tick, bytes, _, err := m.gateJoiner(port, offer, m.ctx.TimeCtl.Held(), m.corrections.ProvedWorld)
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

// askAuthority makes one round trip to the authority up this run's path to it. A run
// with no path is mid-succession, which the joiner retries past.
func (m *Membership) askAuthority(req network.RelayJoin) (network.RelayAdmit, error) {
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
		return network.RelayAdmit{}, errSessionHandoff
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

// tellAuthority sends one step toward the authority without waiting for an answer:
// on this run's link to it, else on the relay's it follows, which passes it on.
func (m *Membership) tellAuthority(req network.RelayJoin) (sent bool) {
	m.world.RunSafe(func() { sent = m.tellAuthorityLocked(req) })
	return sent
}

// tellAuthorityLocked is tellAuthority for a caller that holds the world lock.
// Caller MUST hold updateMutex.
func (m *Membership) tellAuthorityLocked(req network.RelayJoin) bool {
	return m.sendDeskLocked(m.upLinkLocked(), network.MsgRelayJoin, req)
}

// upLinkLocked is the link this run reaches its authority on, zero for no path.
// Caller MUST hold updateMutex.
func (m *Membership) upLinkLocked() (up link.PeerID) {
	m.linksLocked(func(l relayLinks) { up = link.PeerID(l.Up()) })
	return up
}

// sendDeskLocked sends one relayed-admission frame on the link to a participant.
// Caller MUST hold updateMutex.
func (m *Membership) sendDeskLocked(to link.PeerID, kind link.MessageType, frame any) bool {
	port := m.sessionTransportLocked()
	if port == nil || to == 0 {
		return false
	}
	body, err := json.Marshal(frame)
	return err == nil && port.Send(uint32(to), uint8(kind), body)
}

// onRelayJoin takes one relayed-admission frame off the tick: a holder's question
// when this run is the coordinator, the coordinator's answer when it holds bots,
// and either on its way past a relay between the two.
// Caller MUST hold updateMutex.
func (m *Membership) onRelayJoin(kind uint8, from uint32, body []byte) {
	switch link.MessageType(kind) {
	case network.MsgRelayAdmit:
		m.receiveRelayAdmit(link.PeerID(from), body)
	case network.MsgRelayJoin:
		m.decideRelayJoinLocked(link.PeerID(from), body)
	}
}

// receiveRelayAdmit wakes the round trip an answer belongs to, or sends one for a
// relay behind this run down the link its question came up on. An identity
// assigned to a join that stopped waiting, or that no link leads to any more, goes
// back to the authority. Caller MUST hold updateMutex.
func (m *Membership) receiveRelayAdmit(from link.PeerID, body []byte) {
	var ans network.RelayAdmit
	if json.Unmarshal(body, &ans) != nil || from != m.authorityID() && from != m.upLinkLocked() {
		return
	}
	if n := len(ans.Path); n > 0 {
		down := ans
		down.Path = ans.Path[:n-1]
		if !m.sendDeskLocked(ans.Path[n-1], network.MsgRelayAdmit, down) && ans.Offer.Assigned != 0 {
			m.tellAuthorityLocked(network.RelayJoin{Step: network.RelayAbandon, Leaf: ans.Offer.Assigned, Path: ans.Path})
		}
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
		m.tellAuthorityLocked(network.RelayJoin{Step: network.RelayAbandon, Leaf: ans.Offer.Assigned})
	}
}

// passRelayJoinLocked sends a step asked on a link behind this run on toward the
// authority, that link added to its path so the answer finds its way back. With no
// path up, or from a link this run does not carry, a step that waits is refused as
// mid-handoff, which its joiner retries past. Caller MUST hold updateMutex.
func (m *Membership) passRelayJoinLocked(from link.PeerID, req network.RelayJoin) {
	carried := false
	m.linksLocked(func(l relayLinks) { carried = l.Carries(uint32(from)) })
	if up := m.upLinkLocked(); carried && up != from && len(req.Path) < parameter.NetworkRelayHopLimit {
		pass := req
		pass.Path = append(slices.Clone(req.Path), from)
		if m.sendDeskLocked(up, network.MsgRelayJoin, pass) {
			return
		}
	}
	if req.Step == network.RelayAbandon || req.Step == network.RelayDoor {
		return
	}
	ans := network.RelayAdmit{Nonce: req.Nonce, Path: req.Path}
	ans.Refuse(errSessionHandoff)
	m.sendDeskLocked(from, network.MsgRelayAdmit, ans)
}

// decideRelayJoinLocked answers one step a holder asks: on its own link, or up the
// relays its path names, the first of them the holder. A run that does not
// coordinate passes it on. Caller MUST hold updateMutex.
func (m *Membership) decideRelayJoinLocked(from link.PeerID, body []byte) {
	var req network.RelayJoin
	if err := json.Unmarshal(body, &req); err != nil {
		m.log.Warn("session", "msg", "relayed join step unreadable", "relay", from, "error", err.Error())
		return
	}
	if !m.world.IsSessionCoordinator() {
		m.passRelayJoinLocked(from, req)
		return
	}
	relay := from
	if len(req.Path) > 0 {
		relay = req.Path[0]
	}
	// A participant this run holds a link to asks on it: no relay speaks for one
	spoken := relay != from && m.linkedLocked(relay)
	switch req.Step {
	case network.RelayAbandon:
		if !spoken {
			m.abandonRelayedLocked(relay, req.Leaf)
		}
		return
	case network.RelayDoor:
		if !spoken {
			m.reach.NoteDoor(relay, req.Door)
		}
		return
	}
	ans := network.RelayAdmit{Nonce: req.Nonce, Path: req.Path}
	var err error
	switch {
	case spoken:
		err = errors.New("a participant asks on its own link, and no relay for it")
	case req.Step == network.RelayNonce:
		ans.Challenge = m.issueRelayNonce(relay)
	case req.Step == network.RelayAssign && !m.lateJoins.Load():
		err = errSessionStarting
	case req.Step == network.RelayAssign:
		var keys []string
		var pass *network.PassBody
		if keys, err = m.relayedKeys(relay, req); err == nil && req.Pass != nil {
			pass, err = m.checkPass(req.Pass, keys)
		}
		if err == nil {
			ans.Offer, err = m.assignFor(m.joinAnchorLocked(), relay, m.departingLocked(), keys, pass, nil, m.relaySeatingLocked())
			m.closeStaleLink(err, m.sessionTransportLocked())
		}
	case req.Step == network.RelayReady:
		err = m.admitRelayedLocked(relay, req.Leaf, req.Report)
	default:
		err = fmt.Errorf("relayed join step %d is not one this build takes", req.Step)
	}
	if err != nil {
		ans.Refuse(err)
	}
	m.sendDeskLocked(from, network.MsgRelayAdmit, ans)
}

// linkedLocked reports a participant on a link of this run's own.
// Caller MUST hold updateMutex.
func (m *Membership) linkedLocked(id link.PeerID) bool {
	p, ok := m.sessionTransportLocked().(interface{ Peers() []uint32 })
	return ok && slices.Contains(p.Peers(), uint32(id))
}

// relayNonce is one challenge issued to a relay's door, good for one dial's proofs
// for relayNonceLife, so a relay that carries a dial proves nothing itself.
type relayNonce struct {
	relay  link.PeerID
	issued time.Time // [wall] a handshake's bound
}

// relayNonceLife is how long a challenge waits for its proofs: a dial's connect and
// its ready period; relayNoncesKept bounds the challenges outstanding at once.
const (
	relayNonceLife  = parameter.NetworkJoinConnectTimeout + parameter.NetworkJoinPressWindow
	relayNoncesKept = 64
)

// issueRelayNonce makes the challenge a relay's door asks its next dial, sweeping
// the ones that ran out; past the bound the oldest gives way.
func (m *Membership) issueRelayNonce(relay link.PeerID) string {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	now := time.Now() // [wall]
	if m.relayNonces == nil {
		m.relayNonces = make(map[string]relayNonce)
	}
	for n, r := range m.relayNonces {
		if now.Sub(r.issued) > relayNonceLife {
			delete(m.relayNonces, n)
		}
	}
	for len(m.relayNonces) >= relayNoncesKept {
		oldest, at := "", now
		for n, r := range m.relayNonces {
			if r.issued.Before(at) {
				oldest, at = n, r.issued
			}
		}
		delete(m.relayNonces, oldest)
	}
	nonce := network.NewNonce()
	m.relayNonces[nonce] = relayNonce{relay: relay, issued: now}
	return nonce
}

// relayedKeys checks the proofs a relay carried on against the challenge this
// coordinator issued its door, spent here, and returns the keys they prove.
func (m *Membership) relayedKeys(relay link.PeerID, req network.RelayJoin) ([]string, error) {
	m.sessionMu.Lock()
	r, ok := m.relayNonces[req.Challenge]
	delete(m.relayNonces, req.Challenge)
	m.sessionMu.Unlock()
	if !ok || r.relay != relay || time.Since(r.issued) > relayNonceLife { // [wall]
		return nil, &network.Refusal{Code: network.RefusalCredential, Reason: "the relayed dial answered no challenge of this session's"}
	}
	return network.VerifyProofs(req.Keys, req.Challenge, nil)
}

// relayedSlot is the slot of a participant joining through relay, if it is one: a
// new place the assignment held under that relay, or one taken back through it,
// which keeps the holder it left with.
func (m *Membership) relayedSlot(relay, leaf link.PeerID) (uint8, bool) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	i := slices.IndexFunc(m.sessionRoster, func(p network.RosterEntry) bool { return p.ID == leaf })
	if i < 0 {
		return 0, false
	}
	through := m.sessionRoster[i].Holder
	if r, back := m.returning[leaf]; back {
		through = r.via
	}
	return m.sessionRoster[i].Slot, through == relay
}

// admitRelayedLocked crosses the arrival of a participant that installed its relay's
// world. Its report settles what the assignment only guessed: a bot keeps the
// relay as its holder, a person holds nothing and may declare an address.
// Caller MUST hold updateMutex.
func (m *Membership) admitRelayedLocked(relay, leaf link.PeerID, report network.JoinerReport) error {
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

// abandonRelayedLocked retires a failed relayed join as the barrier holds it, not as
// the holder guessed: a place taken back and not yet crossed in goes back to its
// Wraith, an identity never crossed in to the pool at once, as a direct joiner's does,
// since a new game drops a dismissal with no cursor to remove and the place would
// stay taken, and one that crossed in is dismissed. Caller MUST hold updateMutex.
func (m *Membership) abandonRelayedLocked(relay, leaf link.PeerID) {
	slot, ok := m.relayedSlot(relay, leaf)
	if !ok || m.giveBack(leaf) {
		return
	}
	for _, sys := range m.world.Systems() {
		if s, ok := sys.(interface{ CrossedIn(uint32) bool }); ok && !s.CrossedIn(uint32(leaf)) {
			m.leaveRoster(leaf, true)
			return
		}
	}
	m.pushDepartureLocked(event.ParticipantDepartedPayload{Participant: uint32(leaf), Slot: slot, Dismissed: true},
		core.DomainPlayer)
}
