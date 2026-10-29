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
	link "github.com/lixenwraith/vif/pkg/network"
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
	dialing  map[link.PeerID]bool
	// admitted is when each participant's own link here was last admitted, which
	// lands within a pass; a dial to it meanwhile would cross that link.
	admitted map[link.PeerID]time.Time
	nextPass time.Time
	passes   int
	remotes  map[link.PeerID]string // where each joiner's stream came from, for a door it declares later

	// hostFor is the authority followed through another participant's link that
	// hostTries dials have gone to, the next no sooner than nextHost; see dialHost.
	hostFor   link.PeerID
	hostTries int
	nextHost  time.Time
	// successor is the first candidate succTries dials have gone to, the next no
	// sooner than nextSucc, and succLinked whether a link to it stood; see dialSuccessor.
	successor  link.PeerID
	succTries  int
	nextSucc   time.Time
	succLinked bool

	statChain     *atomic.Int64
	statAttempts  *atomic.Int64
	statListening *atomic.Bool
}

func newReach(inst Instance, reg *status.Registry) *Reach {
	return &Reach{
		inst:          inst,
		dialing:       make(map[link.PeerID]bool, parameter.MaxPlayers),
		admitted:      make(map[link.PeerID]time.Time, parameter.MaxPlayers),
		remotes:       make(map[link.PeerID]string, parameter.MaxPlayers),
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
	ln, err := link.BindPeer(want)
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
// whole, and keeps where its stream came from for the door it may open later. Only
// the coordinator runs it: it is the far end of the stream the address arrived on.
func (r *Reach) NoteDeclared(id link.PeerID, report network.JoinerReport) {
	if r == nil || id == 0 {
		return
	}
	r.mu.Lock()
	r.remotes[id] = report.Remote
	r.mu.Unlock()
	if report.Listen == "" {
		return
	}
	addr, ok := resolveDeclared(report.Listen, report.Remote)
	if !ok {
		return
	}
	r.authority.appendChain(id, addr)
}

// NoteDoor puts one participant's door in the chain: a bare port completed from the
// stream it joined on, as a declared port is, or a link reached as it is written.
// Only the coordinator runs it, under the world lock the step arrived under.
func (r *Reach) NoteDoor(id link.PeerID, door string) {
	if r == nil || id == 0 || door == "" {
		return
	}
	r.mu.Lock()
	remote := r.remotes[id]
	r.mu.Unlock()
	addr, ok := resolveDeclared(door, remote)
	if !ok {
		if e, err := link.ParseEndpoint(door); err != nil || e.Addr == "" || e.Scheme == link.SchemeSite {
			return
		}
		addr = door
	}
	r.authority.noteDoor(id, addr)
	r.log.Info("net", "msg", "relay door noted", "peer", uint64(id), "door", addr)
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
func (r *Reach) Forget(id link.PeerID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.dialing, id)
	delete(r.remotes, id)
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
	chain := r.authority.Chain()
	r.statChain.Store(int64(len(chain) - len(slices.DeleteFunc(chain, func(e network.ChainEntry) bool { return e.Addr != "" }))))
	if walking {
		r.retrySuccession()
		return
	}
	r.mu.Lock()
	r.passes = 0
	r.mu.Unlock()
	r.statAttempts.Store(0)
	r.dialSuccessor()
	r.dialHost()
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
// to author already has one when it does. One out of reach is dialled as a followed
// host is, each attempt after a longer wait, then left (NetworkHostDialAttempts):
// the walk dials it again should the authority go. Another successor, or a link to
// this one that stood and dropped, starts the attempts over.
func (r *Reach) dialSuccessor() {
	u := r.authority
	successor, ok := u.successor()
	if !ok || successor == 0 || successor == u.Local() {
		return
	}
	dialer, ok := r.inst.Transport().(engine.PeerDialingPort)
	if !ok {
		return
	}
	linked := dialer.Connected(uint32(successor))
	now := time.Now() // [wall] a dial is a link, not a tick
	r.mu.Lock()
	if r.successor != successor || linked || r.succLinked {
		r.successor, r.succTries, r.nextSucc = successor, 0, time.Time{}
	}
	r.succLinked = linked
	due := !linked && r.succTries < parameter.NetworkHostDialAttempts && !now.Before(r.nextSucc)
	if due {
		r.succTries++
		r.nextSucc = now.Add(parameter.NetworkRejoinPassInterval << (r.succTries - 1))
	}
	r.mu.Unlock()
	if !due {
		return
	}
	if addr, found := u.Chain().Lookup(successor); found {
		r.dial(dialer, successor, addr)
	}
}

// dialHost opens this instance's own link to the authority it follows through
// another participant, at the address the chain names for it, so its artifacts go
// one hop rather than two and the relay is let go; the coordinator a relayed
// person joined behind has no chain address and is followed as it was. Attempts
// back off and stop (NetworkHostDialAttempts): a host unreachable from here stays so.
func (r *Reach) dialHost() {
	host, ok := r.authority.followed()
	if !ok {
		return
	}
	dialer, ok := r.inst.Transport().(engine.PeerDialingPort)
	if !ok {
		return
	}
	if dialer.Connected(uint32(host)) {
		r.authority.reachedHost(host)
		return
	}
	addr, found := r.authority.Chain().Lookup(host)
	if !found {
		return
	}
	now := time.Now() // [wall] a dial is a link, not a tick
	r.mu.Lock()
	if r.hostFor != host {
		r.hostFor, r.hostTries, r.nextHost = host, 0, time.Time{}
	}
	due := r.hostTries < parameter.NetworkHostDialAttempts && !now.Before(r.nextHost)
	if due {
		r.hostTries++
		r.nextHost = now.Add(parameter.NetworkRejoinPassInterval << (r.hostTries - 1))
	}
	r.mu.Unlock()
	if due {
		r.dial(dialer, host, addr)
	}
}

// retrySuccession dials each candidate whose turn has come and is not linked yet,
// once a second, for the record of one that hosts or a link that carries this
// instance. From the second pass the notice goes again on every link: the first
// went as the loss was seen, which a peer still linked to the lost host drops
// unread, as a full queue drops any frame; whoever hosts by now answers it.
func (r *Reach) retrySuccession() {
	now := time.Now()
	r.mu.Lock()
	if now.Before(r.nextPass) {
		r.mu.Unlock()
		return
	}
	r.nextPass = now.Add(parameter.NetworkRejoinPassInterval)
	r.passes++
	again := r.passes > 1
	r.mu.Unlock()
	if again {
		r.authority.sendReport()
	}
	dialer, ok := r.inst.Transport().(engine.PeerDialingPort)
	if !ok {
		return
	}

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
func (r *Reach) dial(dialer engine.PeerDialingPort, id link.PeerID, addr string) {
	r.mu.Lock()
	if r.dialing[id] || time.Since(r.admitted[id]) < parameter.NetworkRejoinPassInterval { // [wall]
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
func (g *PeerLinkGate) Local() link.PeerID {
	r := g.reach.Load()
	if r == nil {
		return 0
	}
	return link.PeerID(r.inst.LocalParticipant())
}

// admit is the acceptor hook: it answers nothing until the run is bound.
func (g *PeerLinkGate) Admit(from link.PeerID) error {
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
func (r *Reach) admitPeerLink(from link.PeerID) error {
	local := link.PeerID(r.inst.LocalParticipant())
	if from == 0 || from == local {
		return fmt.Errorf("peer link: participant %d is not another participant", from)
	}
	if !slices.ContainsFunc(r.authority.currentRoster(),
		func(p network.RosterEntry) bool { return p.ID == from }) {
		return fmt.Errorf("peer link: participant %d is not in this session", from)
	}
	// Two participants dialling each other would each keep the link it took first and
	// close the other, leaving neither when they took different ones: both keep the
	// lower identity's.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dialing[from] && local < from {
		return fmt.Errorf("peer link: participant %d dials %d itself: %w", local, from, link.ErrLinkStands)
	}
	r.admitted[from] = time.Now() // [wall]
	return nil
}
