// Reachability: binding a port, proving it answers, publishing it, and dialling it.
//
// `-authority migrate` moved authorship and nothing else. The successor authored,
// alone, because it did not bind the port its predecessor held — it is usually on
// another machine and could not — and no artifact in the protocol carried an
// address, so no survivor had ever been told where it was. The outcome was one solo
// game per survivor and the only thing succession decided was which of them believed
// it was hosting one.
//
// The sequence that closes it, in the order it runs:
//
//  1. A guest binds a listening port before it answers the offer: `-listen` pins
//     one, the default is the coordinator's own port, and a machine already using
//     that port falls back to an OS-assigned one. It declares what it actually
//     bound, which is why the bind happens before the reply rather than after.
//     A bind that fails entirely, or `-no-advertise`, leaves the participant a
//     leaf — it plays normally and is never a candidate.
//  2. It warns that the address will be shared, which is the window in which
//     quitting costs nothing.
//  3. The coordinator dials the declared address once. That round trip is the bind
//     confirmation: it turns "the guest says it is listening" into "the session has
//     reached it there", which is what makes the address worth publishing. A guest
//     behind NAT or a firewall fails it and stays a leaf. The address is resolved
//     against the join connection's own remote address, because a guest knows which
//     port it bound and not which address the world reaches it at.
//  4. One hold later, and only if the guest is still connected, the authority
//     publishes: the address on MsgPeerList, and the confirmation itself as the
//     barrier-bound EventParticipantReachable crossing.
//  5. Every participant but the designated successor dials the successor, so the
//     one instance that will need links has them before it needs them. A successor
//     chain rather than a full mesh: N links instead of N², and the map is the same
//     either way, so the shape is one dial policy.
//  6. A survivor that loses the authority and has no link to the successor retries
//     down the succession list, repeating the list once a second for as long as the
//     succession window lasts.
//
// Why the two published things are different kinds. The address may change — a peer
// rebinds, an ephemeral port is different next time — so it travels on a broadcast
// that anyone may miss and nothing decides anything from. The confirmation is a
// succession input, and DesignatedSuccessor holds only while every survivor computes
// it from identical state, so it travels as a crossing applied at one agreed tick on
// every instance. That is the same guarantee the roster has and it is what a
// broadcast could not give: a broadcast is identical eventually, and the moment the
// set is read is the moment the authority died.

package app

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// declaredPeer is one participant's address as the coordinator received it, and
// where the confirmation of it has got to.
type declaredPeer struct {
	addr      string
	confirmed time.Time // zero until the dial round trip succeeded
	published bool
}

// reach is this instance's half of the design above. It owns no world state: what
// it produces is one crossing and one broadcast, and what it consumes is the map.
type reach struct {
	a *App

	mu       sync.Mutex
	listener net.Listener
	declared string
	peers    map[network.PeerID]*declaredPeer
	dialing  map[network.PeerID]bool
	nextPass time.Time

	statConfirmed *atomic.Int64
	statAttempts  *atomic.Int64
	statListening *atomic.Bool
}

func newReach(a *App) *reach {
	reg := a.world.Resources.Status
	return &reach{
		a:             a,
		peers:         make(map[network.PeerID]*declaredPeer, parameter.MaxPlayers),
		dialing:       make(map[network.PeerID]bool, parameter.MaxPlayers),
		statConfirmed: reg.Ints.Get("network.reachable"),
		statAttempts:  reg.Ints.Get("network.rejoin_attempts"),
		statListening: reg.Bools.Get("network.listening"),
	}
}

// bindAdvertised binds the port this participant will be dialled on and returns it
// with the address to declare.
//
// want is what the operator asked for, empty for the default; hostAddr is the
// address this instance dialled, whose port is that default. A declared address
// with no host in it is completed by the coordinator from the connection's own
// remote address — a guest knows which port it bound and not which address the
// world reaches it at, and only the far end of an established stream knows both.
func bindAdvertised(want, hostAddr string, cfg *network.Config) (net.Listener, string) {
	if want == "" {
		_, port, err := net.SplitHostPort(hostAddr)
		if err != nil || port == "" {
			return nil, ""
		}
		want = ":" + port
	}
	explicit := true
	if host, _, err := net.SplitHostPort(want); err == nil && unspecifiedHost(host) {
		explicit = false
	}
	ln, err := network.BindPeer(want, cfg)
	if err != nil {
		// Not fatal, by decision: refusing to play because a port was taken turns a
		// privacy or firewall condition into a lockout. The participant is a leaf.
		vlog.Warn("app", "msg", "no listening port; this participant is a leaf",
			"address", want, "error", err.Error())
		return nil, ""
	}
	bound := ln.Addr().String()
	if explicit {
		return ln, bound
	}
	_, port, err := net.SplitHostPort(bound)
	if err != nil {
		return ln, bound
	}
	return ln, ":" + port
}

// unspecifiedHost reports an address that names a port and no reachable host.
func unspecifiedHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// adoptListener records the port this instance bound and warns about what will be
// done with it. The warning is the window §5.3 promises: the address is not
// published until the coordinator has confirmed it and held it, so a participant
// that quits inside this has shared nothing.
func (r *reach) adoptListener(ln net.Listener, declared string) {
	if ln == nil {
		return
	}
	r.mu.Lock()
	r.listener, r.declared = ln, declared
	r.mu.Unlock()
	r.statListening.Store(true)
	r.a.ctx.SetStatusMessage(
		fmt.Sprintf("Listening on %s; the session will share it with the other participants", ln.Addr()),
		4*parameter.StatusMessageDefaultTimeout, false)
	vlog.Info("app", "msg", "peer listener bound",
		"bound", ln.Addr().String(), "declared", declared)
}

// declaredAddr is what this instance told its coordinator, empty for a leaf.
func (r *reach) declaredAddr() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.declared
}

// noteDeclared records one joiner's declared address and starts the confirmation
// dial. Only the coordinator runs it: it is the far end of the stream the address
// arrived on, so it is the only instance that knows both the port and the address.
func (r *reach) noteDeclared(id network.PeerID, report network.JoinerReport) {
	if r == nil || report.Listen == "" || id == 0 {
		return
	}
	addr, ok := resolveDeclared(report.Listen, report.Remote)
	if !ok {
		return
	}
	r.mu.Lock()
	if _, held := r.peers[id]; held {
		r.mu.Unlock()
		return
	}
	r.peers[id] = &declaredPeer{addr: addr}
	r.mu.Unlock()

	// Off the accept path: a firewalled address answers by timing out, and the
	// handshake this runs beside must not wait for it.
	go r.confirm(id, addr)
}

// resolveDeclared completes a declared address from the connection it arrived on.
func resolveDeclared(declared, remote string) (string, bool) {
	host, port, err := net.SplitHostPort(declared)
	if err != nil || port == "" || port == "0" {
		return "", false
	}
	if !unspecifiedHost(host) {
		return declared, true
	}
	peer, _, err := net.SplitHostPort(remote)
	if err != nil || peer == "" {
		return "", false
	}
	return net.JoinHostPort(peer, port), true
}

// confirm makes the bind-confirmation round trip. What it proves is that the
// session reached this participant at an address of its own, which is the whole
// difference between an address a guest claimed and one worth publishing.
func (r *reach) confirm(id network.PeerID, addr string) {
	local, term := r.a.authorityID(), r.a.authorityTerm()
	if _, _, err := network.DialPeerLink(addr, r.a.peerDialConfig(), local, term, true); err != nil {
		vlog.Info("app", "msg", "participant address not confirmed",
			"participant", uint64(id), "address", addr, "error", err.Error())
		r.mu.Lock()
		delete(r.peers, id)
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	if p := r.peers[id]; p != nil {
		p.confirmed = time.Now()
	}
	r.mu.Unlock()
	vlog.Info("app", "msg", "participant address confirmed",
		"participant", uint64(id), "address", addr)
}

// forget drops a departed participant from everything this instance holds about it.
func (r *reach) forget(id network.PeerID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.peers, id)
	delete(r.dialing, id)
	r.mu.Unlock()
}

// close releases the listening port. It is the App's, not the transport's: a
// participant that never reached a session still bound one.
func (r *reach) close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	ln := r.listener
	r.listener = nil
	r.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

// drive is the per-loop step, called from the same place the succession is driven:
// between two ticks, on every instance, whichever half of the protocol it is.
func (r *reach) drive(contested bool) {
	if r == nil || r.a.authority == nil {
		return
	}
	r.publishConfirmed()
	r.statConfirmed.Store(int64(len(r.a.authority.Reachable())))
	if contested {
		r.retrySuccession()
		return
	}
	r.statAttempts.Store(0)
	r.dialSuccessor()
}

// publishConfirmed promotes the confirmations whose hold has elapsed. Only the
// authority publishes, because only the authority made the dial.
func (r *reach) publishConfirmed() {
	u := r.a.authority
	if !u.IsAuthority() {
		return
	}
	dialer, _ := r.a.sessionTransport().(engine.PeerDialingPort)
	now := time.Now()

	var promote []network.PeerID
	addresses := u.Addresses()
	r.mu.Lock()

	for id, p := range r.peers {
		if p.published || p.confirmed.IsZero() || now.Sub(p.confirmed) < parameter.NetworkAdvertiseHold {
			continue
		}
		// "and only if the guest is still connected": a participant that left
		// inside the hold publishes nothing, which is what the hold is for.
		if dialer == nil || !dialer.Connected(uint32(id)) {
			delete(r.peers, id)
			continue
		}
		p.published = true
		addresses = addresses.With(id, p.addr)
		promote = append(promote, id)
	}
	r.mu.Unlock()
	if len(promote) == 0 {
		return
	}
	u.publishAddresses(addresses)
	slices.Sort(promote)
	for _, id := range promote {
		// A crossing rather than a field on the broadcast: the confirmed set is a
		// succession input and has to be applied at one agreed tick everywhere.
		r.a.world.RunSafe(func() {
			r.a.world.PushEventFull(event.EventParticipantReachable,
				&event.ParticipantReachablePayload{Participant: uint32(id)},
				event.OriginSession, core.DomainPlayer)
		})
	}
}

// dialSuccessor keeps the successor chain: every participant but the designated
// successor holds a link to it, so the one instance that will have to author has
// the links before it needs them.
//
// A chain rather than a full mesh, by decision: N links instead of N², and the
// address map is the same either way, so the shape is one dial policy and can be
// widened later without changing an artifact.
func (r *reach) dialSuccessor() {
	u := r.a.authority
	successor, ok := u.Successor()
	if !ok || successor == 0 || successor == u.local {
		return
	}
	dialer, ok := r.a.sessionTransport().(engine.PeerDialingPort)
	if !ok || dialer.Connected(uint32(successor)) {
		return
	}
	addr, found := u.Addresses().Lookup(successor)
	if !found {
		return
	}
	r.dial(successor, addr)
}

// retrySuccession is the reconnect a survivor makes when the authority has gone and
// it has no link to whoever is taking over.
//
// It walks the succession list — every survivor in the order the rule would elect
// them — and repeats the whole list once a second rather than hammering one address:
// the successor may be the second name rather than the first, and the first may be
// slow rather than absent. The attempt count is published so a player can see it is
// trying rather than stalled.
func (r *reach) retrySuccession() {
	dialer, ok := r.a.sessionTransport().(engine.PeerDialingPort)
	if !ok {
		return
	}
	now := time.Now()
	r.mu.Lock()
	if now.Before(r.nextPass) {
		r.mu.Unlock()
		return
	}
	r.nextPass = now.Add(parameter.NetworkRejoinPassInterval)
	r.mu.Unlock()

	addresses := r.a.authority.Addresses()
	for _, id := range r.a.authority.SuccessionOrder() {
		if dialer.Connected(uint32(id)) {
			return // already reachable; the record will arrive on that link
		}
		if addr, ok := addresses.Lookup(id); ok {
			r.dial(id, addr)
		}
	}
	r.statAttempts.Add(1)
}

// dial opens one peer link, at most one attempt per participant at a time. The
// dial itself is off this loop: a connect to an address nothing answers takes the
// whole connect timeout, and this runs between two ticks.
func (r *reach) dial(id network.PeerID, addr string) {
	r.mu.Lock()
	if r.dialing[id] {
		r.mu.Unlock()
		return
	}
	r.dialing[id] = true
	r.mu.Unlock()

	term := r.a.authorityTerm()
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.dialing, id)
			r.mu.Unlock()
		}()
		dialer, ok := r.a.sessionTransport().(engine.PeerDialingPort)
		if !ok {
			return
		}
		if err := dialer.DialPeer(addr, term); err != nil {
			vlog.Debug("app", "msg", "peer dial failed",
				"participant", uint64(id), "address", addr, "error", err.Error())
			return
		}
		vlog.Info("app", "msg", "peer link opened",
			"participant", uint64(id), "address", addr)
	}()
}

// peerLinkGate defers a peer link's admission to an App that does not exist yet.
//
// The listener is bound and its accept handshake installed before the App is
// constructed, because the join reply that declares the port goes out first and has
// to name the port that was actually bound. So the gate holds the App rather than
// closing over it, and refuses until it has one.
type peerLinkGate struct{ app atomic.Pointer[App] }

func (g *peerLinkGate) bind(a *App) { g.app.Store(a) }

func (g *peerLinkGate) admit(from network.PeerID, term network.AuthorityTerm) error {
	a := g.app.Load()
	if a == nil {
		return errors.New("peer link: this participant has no session yet")
	}
	return a.admitPeerLink(from, term)
}

// admitPeerLink is the admission a peer link passes on this instance: the caller is
// a participant of this session, it is not this instance, and its term is not
// behind. It allocates nothing — the two already hold identities the coordinator
// assigned — which is the whole difference between a peer link and a join.
func (a *App) admitPeerLink(from network.PeerID, term network.AuthorityTerm) error {
	local := network.PeerID(a.localParticipant())
	if from == 0 || from == local {
		return fmt.Errorf("peer link: participant %d is not another participant", from)
	}
	if held := a.authorityTerm(); term < held {
		return fmt.Errorf("peer link: participant %d dialled under term %d, this session holds %d",
			from, term, held)
	}
	a.sessionMu.Lock()
	known := slices.ContainsFunc(a.sessionRoster,
		func(p network.SessionParticipant) bool { return p.ID == from })
	a.sessionMu.Unlock()
	if !known {
		return fmt.Errorf("peer link: participant %d is not in this session", from)
	}
	return nil
}

// peerDialConfig is the transport configuration a peer dial and a bind confirmation
// use. It carries this build's identity, so a link to a peer running a different
// simulation is refused where it is opened rather than after it has been used.
func (a *App) peerDialConfig() *network.Config {
	cfg := network.DebugConfig(network.RolePeer, "")
	cfg.Identity = a.sessionIdentity()
	cfg.OnError = logSessionError
	return cfg
}

// reachAddresses and reachConfirmed are what an offer carries. They are read
// through the authority rather than from reach's own tables because a guest holds
// them too — adopted from the offer or the handoff it was admitted by — and an
// offer this instance writes has to say the same thing whichever it is.
func (a *App) reachAddresses() network.PeerAddresses {
	if a.authority == nil {
		return nil
	}
	return a.authority.Addresses()
}

func (a *App) reachConfirmed() []network.PeerID {
	if a.authority == nil {
		return nil
	}
	return a.authority.Reachable()
}
