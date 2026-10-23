package converge

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/internal/vlog"
)

// Reach is this instance's listening port, the addresses it has learned, and the
// links it opens from them. See doc/multi-player.md §5.3.
type Reach struct {
	inst      Instance
	authority *Authority
	log       *vlog.Log

	mu       sync.Mutex
	listener net.Listener
	declared string
	dialing  map[network.PeerID]bool
	nextPass time.Time

	statChain     *atomic.Int64
	statAttempts  *atomic.Int64
	statListening *atomic.Bool
}

func newReach(inst Instance, reg *status.Registry) *Reach {
	return &Reach{
		inst:          inst,
		dialing:       make(map[network.PeerID]bool, parameter.MaxPlayers),
		statChain:     reg.Ints.Get("network.chain"),
		statAttempts:  reg.Ints.Get("network.rejoin_attempts"),
		statListening: reg.Bools.Get("network.listening"),
	}
}

// BindAdvertised binds the port this participant is dialled on and returns the
// address to declare. want is -listen, empty for the coordinator's own port. An
// address with no host is completed by the coordinator from the connection.
func BindAdvertised(want, hostAddr string) (net.Listener, string) {
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
	ln, err := network.BindPeer(want)
	if err != nil {
		// A port a participant could not take makes it a leaf, never an error.
		vlog.Warn("net", "msg", "no listening port; this participant is a leaf",
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

// unspecifiedHost reports an address naming a port and no reachable host.
func unspecifiedHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// AdoptListener records the bound port and says what will be done with it.
func (r *Reach) AdoptListener(ln net.Listener, declared string) {
	if ln == nil {
		return
	}
	r.mu.Lock()
	r.listener, r.declared = ln, declared
	r.mu.Unlock()
	r.statListening.Store(true)
	r.inst.SetStatusMessage(
		fmt.Sprintf("Listening on %s; the session will share it with the other peers", ln.Addr()), parameter.StatusNotice)
	r.log.Info("net", "msg", "peer listener bound",
		"bound", ln.Addr().String(), "declared", declared)
}

// DeclaredAddr is what this instance told its coordinator, empty for a leaf.
func (r *Reach) DeclaredAddr() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.declared
}

// NoteDeclared puts one joiner in the chain, which the correction loop publishes
// whole. Only the coordinator runs it: it is the far end of the stream the address
// arrived on.
func (r *Reach) NoteDeclared(id network.PeerID, report network.JoinerReport) {
	if r == nil || report.Listen == "" || id == 0 {
		return
	}
	addr, ok := resolveDeclared(report.Listen, report.Remote)
	if !ok {
		return
	}
	r.authority.appendChain(id, addr)
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

// Forget drops a departed participant's in-flight dial.
func (r *Reach) Forget(id network.PeerID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.dialing, id)
	r.mu.Unlock()
}

// Close releases the listening port, which is the run's rather than the transport's,
// and the address declared on it: a run that left its session is reachable at
// nothing that session was told.
func (r *Reach) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	ln := r.listener
	r.listener, r.declared = nil, ""
	r.mu.Unlock()
	r.statListening.Store(false)
	if ln != nil {
		_ = ln.Close()
	}
}

// drive runs between two ticks, from the same loop the succession does. A bot dials
// nobody: it shares its holder's fate, so the link it joined on is the one it needs.
// walking is a survivor walking the list, or one cut off.
func (r *Reach) drive(walking bool) {
	if r == nil || r.authority.seat() {
		return
	}
	r.statChain.Store(int64(len(r.authority.Chain())))
	if walking {
		r.retrySuccession()
		return
	}
	r.statAttempts.Store(0)
	r.dialSuccessor()
}

// cutOff reports a participant with no path to its authority that still has a
// session to reach: the relay it went through left, or carries it no more. It
// dials every candidate for one that carries it (NetworkSystem.sendUp), and goes
// on alone if none does (Authority.driveCutOff).
func (r *Reach) cutOff() bool {
	if r == nil || r.authority.seat() {
		return false
	}
	u := r.authority
	u.mu.Lock()
	in := u.term > 0 && !u.fork && u.local != u.holder && u.contested == 0
	u.mu.Unlock()
	return in && !r.inst.Reaches()
}

// dialSuccessor keeps the chain's first link open, so the instance that will have
// to author already has one when it does.
func (r *Reach) dialSuccessor() {
	u := r.authority
	successor, ok := u.successor()
	if !ok || successor == 0 || successor == u.Local() {
		return
	}
	dialer, ok := r.inst.Transport().(engine.PeerDialingPort)
	if !ok || dialer.Connected(uint32(successor)) {
		return
	}
	if addr, found := u.Chain().Lookup(successor); found {
		r.dial(dialer, successor, addr)
	}
}

// retrySuccession dials each candidate whose turn has come and is not linked yet,
// repeating once a second: the record of one that hosts, or a link that carries
// this instance, arrives on it. Every participant holds the chain, so losing a
// candidate too is survivable.
func (r *Reach) retrySuccession() {
	dialer, ok := r.inst.Transport().(engine.PeerDialingPort)
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

	chain := r.authority.Chain()
	for _, id := range r.authority.walkTargets(now) {
		if dialer.Connected(uint32(id)) {
			continue
		}
		if addr, found := chain.Lookup(id); found {
			r.dial(dialer, id, addr)
		}
	}
	r.statAttempts.Add(1)
}

// dial opens one peer link off this loop, one attempt per participant at a time, as
// a connect nothing answers takes the whole timeout, and on the transport it was
// decided on, which a :leave may have replaced since. A link opened during a
// succession is announced: the record naming the new host was flooded before the
// link existed, so the far end has to be asked.
func (r *Reach) dial(dialer engine.PeerDialingPort, id network.PeerID, addr string) {
	r.mu.Lock()
	if r.dialing[id] {
		r.mu.Unlock()
		return
	}
	r.dialing[id] = true
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.dialing, id)
			r.mu.Unlock()
		}()
		if err := dialer.DialPeer(addr); err != nil {
			r.log.Debug("net", "msg", "peer dial failed",
				"peer", uint64(id), "address", addr, "error", err.Error())
			return
		}
		r.log.Info("net", "msg", "peer link opened",
			"peer", uint64(id), "address", addr)
		r.authority.sendReport()
	}()
}

// PeerLinkGate defers admission to a Reach that does not exist yet: the listener is
// bound and its handshake installed before the run is built, because the join reply
// that declares the port goes out first and has to name the port actually bound.
type PeerLinkGate struct{ reach atomic.Pointer[Reach] }

// Bind hands the gate the reachability half once the run has one.
func (g *PeerLinkGate) Bind(r *Reach) { g.reach.Store(r) }

// Local is the identity a link opened here is answered with: zero until the run is
// bound, and until a joiner's seat names it.
func (g *PeerLinkGate) Local() network.PeerID {
	r := g.reach.Load()
	if r == nil {
		return 0
	}
	return network.PeerID(r.inst.LocalParticipant())
}

// admit is the acceptor hook: it answers nothing until the run is bound.
func (g *PeerLinkGate) Admit(from network.PeerID) error {
	r := g.reach.Load()
	if r == nil {
		return errors.New("peer link: this participant has no session yet")
	}
	return r.admitPeerLink(from)
}

// admitPeerLink admits a participant of this session that is not this instance.
// The world's roster, not the coordinator's lobby: only the coordinator fills that
// one. Not the term either — a survivor still electing is exactly who needs to open
// a link.
func (r *Reach) admitPeerLink(from network.PeerID) error {
	local := network.PeerID(r.inst.LocalParticipant())
	if from == 0 || from == local {
		return fmt.Errorf("peer link: participant %d is not another participant", from)
	}
	if !slices.ContainsFunc(r.authority.currentRoster(),
		func(p network.RosterEntry) bool { return p.ID == from }) {
		return fmt.Errorf("peer link: participant %d is not in this session", from)
	}
	return nil
}
