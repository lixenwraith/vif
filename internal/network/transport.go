package network

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// acceptRetryDelay paces the accept loop after a transient failure such as a
// descriptor limit, so the goroutine cannot busy-spin on a listener that keeps
// failing without being closed.
const acceptRetryDelay = 10 * time.Millisecond

// Transport handles network I/O for a specific role
type Transport struct {
	config   *Config
	listener net.Listener
	peers    *PeerManager

	// handshakes is the concurrency budget for the pre-admission handshake, and
	// pending is the connections currently spending one. The budget is what a
	// flood of silent dialers costs; the set is how Stop reaches them, because a
	// handshake blocked on its own read deadline would otherwise hold shutdown for
	// as long as that deadline allows.
	handshakes chan struct{}
	pendingMu  sync.Mutex
	pending    map[net.Conn]struct{}

	running atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewTransport creates a transport with the given configuration
func NewTransport(cfg *Config) *Transport {
	slots := cfg.MaxHandshakes
	if slots <= 0 {
		slots = DefaultConfig().MaxHandshakes
	}
	return &Transport{
		config:     cfg,
		peers:      NewPeerManager(cfg),
		handshakes: make(chan struct{}, slots),
		pending:    make(map[net.Conn]struct{}, slots),
		stopCh:     make(chan struct{}),
	}
}

// SetHandlers configures message and connection callbacks
func (t *Transport) SetHandlers(
	onConnect func(PeerID),
	onDisconnect func(PeerID),
	onMessage func(PeerID, *Message),
) {
	t.peers.SetHandlers(onConnect, onDisconnect, onMessage)
}

// Start begins listening (server) or connecting (client)
func (t *Transport) Start() error {
	if !t.running.CompareAndSwap(false, true) {
		return nil // Already running
	}

	switch t.config.Role {
	case RoleHost:
		return t.startServer()
	case RolePeer, RoleRelay:
		// A relay dials like any other participant. Its role is what it does with
		// the artifacts once they arrive, not how the stream was established.
		//
		// A participant with nothing to dial is not an error when it has a port of
		// its own: that is a session member waiting to be reached rather than a
		// misconfigured client, and it is what a successor left holding only its
		// own listener is.
		dialing := t.config.preconnected != nil || t.config.Address != ""
		if !dialing && t.config.AcceptPeer == nil {
			t.running.Store(false)
			return errors.New("transport: peer has neither an address to dial nor a port to listen on")
		}
		if dialing {
			if err := t.startClient(); err != nil {
				return err
			}
		}
		// And then it listens, if it was asked to. A bind that fails is not fatal:
		// the participant plays as a leaf, which is what every participant but the
		// coordinator was before reach.go existed.
		if t.config.AcceptPeer != nil {
			if err := t.serveAsPeer(); err != nil {
				t.report(err)
			}
		}
		return nil
	default:
		return nil // RoleNone, no-op
	}
}

// serveAsPeer starts this participant's own listener, from a port the caller
// already bound or from an address to bind now.
func (t *Transport) serveAsPeer() error {
	if ln := t.config.PreboundListener; ln != nil {
		t.listener = ln
		t.wg.Add(1)
		go t.acceptLoop(ln, t.config.AcceptPeer)
		return nil
	}
	if t.config.ListenAddress == "" {
		return nil
	}
	return t.listen(t.config.ListenAddress, t.config.AcceptPeer)
}

// startServer binds and accepts joiners.
func (t *Transport) startServer() error {
	if err := t.listen(t.config.Address, nil); err != nil {
		t.running.Store(false)
		return err
	}
	return nil
}

// listen binds one address and serves it. accept is the per-connection handshake;
// nil selects the coordinator's session handshake.
func (t *Transport) listen(addr string, accept func(net.Conn) (PeerID, error)) error {
	var ln net.Listener
	var err error

	if t.config.TLS != nil {
		ln, err = tls.Listen("tcp", addr, t.config.TLS)
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return err
	}

	t.listener = ln

	t.wg.Add(1)
	go t.acceptLoop(ln, accept)

	return nil
}

// DialPeer opens one outbound link to a participant that is already in this
// session, completing handshake and admitting the stream under the identity it
// answered with. It is the mesh half of reach.go: the coordinator is dialled by
// its guests, and a guest is dialled by whoever the chain says to.
func (t *Transport) DialPeer(addr string, local PeerID) error {
	if !t.running.Load() {
		return errors.New("transport: not running")
	}
	conn, id, err := DialPeerLink(addr, t.config, local)
	if err != nil {
		return err
	}
	if _, err := t.peers.AddConnectionAs(conn, id); err != nil {
		return err
	}
	if t.config.OnAdmit != nil {
		t.config.OnAdmit(id)
	}
	return nil
}

// acceptLoop handles incoming connections
func (t *Transport) acceptLoop(ln net.Listener, accept func(net.Conn) (PeerID, error)) {
	defer t.wg.Done()

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
			}
			// A closed listener never recovers; anything else is reported and
			// paced, so a persistent accept failure cannot spin this goroutine.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			t.report(err)
			select {
			case <-t.stopCh:
				return
			case <-time.After(acceptRetryDelay):
			}
			continue
		}

		// A participant is admitted only under the identity the session assigned it.
		// Accept order is not an identity: the barrier's per-source epoch window and
		// every roster lookup are keyed by canonical participant ID, so admitting a
		// stream under a connection-local number would corrupt both silently.
		handshake := accept
		if handshake == nil {
			handshake = t.config.AcceptSession
		}
		if handshake == nil {
			_ = conn.Close()
			t.report(errors.New("transport: host accepted a connection with no session handshake"))
			continue
		}

		// Refused rather than queued when the budget is spent. Queueing would put
		// the accept loop back behind a read the far end controls, which is the
		// whole of what moving the handshake off it was for; a dialer turned away
		// here can retry, and one that cannot is indistinguishable from the flood
		// that spent the budget.
		select {
		case t.handshakes <- struct{}{}:
		default:
			_ = conn.Close()
			t.report(fmt.Errorf("transport: refused %s, %d handshakes already in flight",
				conn.RemoteAddr(), cap(t.handshakes)))
			continue
		}
		t.wg.Add(1)
		go t.handshake(conn, handshake)
	}
}

// handshake admits one accepted connection, off the accept loop.
//
// The budget is released as soon as AcceptSession returns rather than when this
// goroutine ends: the budget bounds unauthenticated work, and everything after
// that point concerns a peer the session has already assigned an identity to,
// which the roster ceiling bounds instead.
func (t *Transport) handshake(conn net.Conn, accept func(net.Conn) (PeerID, error)) {
	defer t.wg.Done()
	t.holdPending(conn)

	id, err := accept(conn)
	t.releasePending(conn)
	<-t.handshakes
	if err != nil {
		_ = conn.Close()
		t.report(err)
		return
	}
	// AddConnectionAs closes the connection on every failure of its own.
	if _, err := t.peers.AddConnectionAs(conn, id); err != nil {
		t.report(err)
		return
	}
	if t.config.OnAdmit != nil {
		t.config.OnAdmit(id)
	}
}

// holdPending and releasePending track a connection for the length of its
// handshake, so Stop can close it rather than waiting out its read deadline. A
// transport already stopping closes the connection immediately, which is what
// keeps a handshake started in that instant from outliving the listener.
func (t *Transport) holdPending(conn net.Conn) {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	select {
	case <-t.stopCh:
		_ = conn.Close()
	default:
	}
	t.pending[conn] = struct{}{}
}

func (t *Transport) releasePending(conn net.Conn) {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	delete(t.pending, conn)
}

// closePending unblocks every handshake still reading from its stream.
func (t *Transport) closePending() {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	for conn := range t.pending {
		_ = conn.Close()
	}
}

// startClient connects to server
func (t *Transport) startClient() error {
	conn, id := t.config.preconnected, t.config.preconnectedPeer
	if conn == nil {
		var err error
		if conn, err = dial(t.config.Address, t.config); err != nil {
			t.running.Store(false)
			return err
		}
	}
	// The coordinator's identity comes from the offer this stream already accepted;
	// a client that reached here without one has no session to join.
	if id == 0 {
		_ = conn.Close()
		t.running.Store(false)
		return errors.New("transport: peer has no coordinator identity from the join handshake")
	}
	if _, err := t.peers.AddConnectionAs(conn, id); err != nil {
		_ = conn.Close()
		t.running.Store(false)
		return err
	}
	return nil
}

// Peers returns the connected participants in a stable order.
func (t *Transport) Peers() []PeerID { return t.peers.Peers() }

// Bytes reports one link's cumulative decoded and queued frame bytes.
func (t *Transport) Bytes(id PeerID) (in, out uint64, ok bool) { return t.peers.Bytes(id) }

// Connected reports whether one peer is still attached.
func (t *Transport) Connected(id PeerID) bool { return t.peers.Connected(id) }

// Disconnect drops one peer, reporting whether it was connected.
func (t *Transport) Disconnect(id PeerID) bool { return t.peers.Disconnect(id) }

// Addr returns the bound listener address, nil for a client or before Start.
func (t *Transport) Addr() net.Addr {
	if t.listener == nil {
		return nil
	}
	return t.listener.Addr()
}

// report publishes an asynchronous accept or handshake failure.
func (t *Transport) report(err error) {
	if t.config.OnError != nil {
		t.config.OnError(err)
	}
}

// Stop halts the transport
func (t *Transport) Stop() error {
	if !t.running.CompareAndSwap(true, false) {
		return nil
	}

	close(t.stopCh)

	if t.listener != nil {
		t.listener.Close()
	}

	// Before the peers, because a handshake in flight is not a peer yet and would
	// otherwise hold the wait below for its own read deadline.
	t.closePending()
	t.peers.Close()
	t.wg.Wait()

	return nil
}

// Send transmits to a specific peer
func (t *Transport) Send(id PeerID, msg *Message) bool {
	return t.peers.Send(id, msg)
}

// BroadcastExcept sends to all peers but one and returns the number that refused.
func (t *Transport) BroadcastExcept(exclude PeerID, msg *Message) int {
	return t.peers.BroadcastExcept(exclude, msg)
}

// PeerCount returns connected peer count
func (t *Transport) PeerCount() int {
	return t.peers.PeerCount()
}

// IsRunning returns transport state
func (t *Transport) IsRunning() bool {
	return t.running.Load()
}
