package network

import (
	"errors"
	"fmt"
	"io"
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
	config         *Config
	listenMu       sync.Mutex // the listeners, which Addr reads from any goroutine
	listener       net.Listener
	extraListeners []net.Listener
	peers          *PeerManager

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
	onConnect func(id PeerID, served bool),
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
		// A successor can retain only its listener after losing the last upstream link.
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
		// the participant plays as a leaf.
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
	if t.config.PreboundListener == nil && t.config.ListenAddress == "" {
		return nil
	}
	return t.listen(t.config.ListenAddress, t.config.AcceptPeer)
}

// startServer accepts joiners at every door Address names, on no listener when it
// was given neither a door nor a port: a session opened only for this process's own
// streams (ServeConn).
func (t *Transport) startServer() error {
	if ln := t.config.PreboundListener; ln != nil {
		return t.serve(ln, nil)
	}
	if t.config.Address == "" {
		return nil
	}
	doors, err := Doors(t.config.Address)
	var opened []net.Listener
	for i := 0; err == nil && i < len(doors); i++ {
		var ln net.Listener
		if ln, err = Listen(doors[i].Addr, t.config); err == nil {
			opened = append(opened, ln)
		}
	}
	if err != nil {
		for _, ln := range opened {
			_ = ln.Close()
		}
		t.running.Store(false)
		return err
	}
	for _, ln := range opened {
		_ = t.serve(ln, nil) // running: serve refuses only a stopped transport
	}
	return nil
}

// listen serves the port the caller bound, else binds addr.
func (t *Transport) listen(addr string, peer func(net.Conn) (PeerID, error)) error {
	ln := t.config.PreboundListener
	if ln == nil {
		var err error
		if ln, err = Listen(addr, t.config); err != nil {
			return err
		}
	}
	return t.serve(ln, peer)
}

// serve takes joins on ln, which this transport then owns: at Start, later on a host
// started with none, or beside a participant's own port as a second door, the first
// listener staying the one Addr names. It takes joins with the session's handshake
// and, given peer, links from the session's participants beside them.
func (t *Transport) serve(ln net.Listener, peer func(net.Conn) (PeerID, error)) error {
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if !t.running.Load() {
		return errors.New("transport is stopped")
	}
	if t.listener == nil {
		t.listener = ln
	} else {
		t.extraListeners = append(t.extraListeners, ln)
	}
	t.wg.Add(1)
	go t.acceptLoop(ln, door{peer: peer, join: t.config.AcceptSession, admitted: t.config.OnAdmit})
	return nil
}

// door is what a listener serves: links from participants already in the session
// (peer), joins (join), or both, and what runs once a join is a peer. Every dialer
// speaks first, a peer link with MsgConnect and a join with its session's name, so
// a door taking both reads which this is off the first byte.
type door struct {
	peer, join func(net.Conn) (PeerID, error)
	admitted   func(PeerID)
}

// open picks the handshake conn runs, and the stream that handshake reads: one the
// first byte was read from gives it back. served is a join's.
func (d door) open(conn net.Conn, timeout time.Duration) (accept func(net.Conn) (PeerID, error), served bool, link net.Conn, err error) {
	switch {
	case d.join == nil:
		return d.peer, false, conn, nil
	case d.peer == nil:
		return d.join, true, conn, nil
	}
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	}
	first := &firstByte{Conn: conn}
	if _, err := io.ReadFull(conn, first.b[:]); err != nil {
		return nil, false, nil, err
	}
	switch MessageType(first.b[0]) {
	case MsgConnect:
		return d.peer, false, first, nil
	case MsgSessionRoute:
		return d.join, true, first, nil
	}
	return nil, false, nil, fmt.Errorf("transport: %s opened with message %#x, neither a peer link nor a join",
		conn.RemoteAddr(), first.b[0])
}

// firstByte is a stream whose first byte door.open read, read again first.
type firstByte struct {
	net.Conn
	b    [1]byte
	read bool
}

func (c *firstByte) Read(p []byte) (int, error) {
	if c.read || len(p) == 0 {
		return c.Conn.Read(p)
	}
	c.read, p[0] = true, c.b[0]
	return 1, nil
}

// Listen binds addr as a host serves it, so a caller learns it cannot have the port
// before anything depends on it; Config.PreboundListener then hands it to the port.
// A wss:// address serves cfg.TLS, and is refused without one.
func Listen(addr string, cfg *Config) (net.Listener, error) {
	e, err := ParseEndpoint(addr)
	if err == nil {
		err = e.Listenable()
	}
	if err != nil {
		return nil, err
	}
	var ln net.Listener
	switch {
	case e.Secure() && cfg.TLS == nil:
		return nil, errors.New("wss needs a certificate; start with -cert and -key")
	case e.Scheme == SchemeWebSocket:
		ln, err = listenWebSocket(e.Addr, cfg.ConnectTimeout, cfg.TLS)
	default:
		ln, err = net.Listen("tcp", e.Addr)
	}
	return ln, plainNetError(err)
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
	_, err = t.peers.AddConnectionAs(conn, id, false)
	return err
}

// serveSession adds an admission listener without replacing surviving peer links.
func (t *Transport) serveSession(addr string, accept func(net.Conn) (PeerID, error), admitted func(PeerID)) (string, error) {
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if !t.running.Load() {
		return "", errors.New("transport is stopped")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	t.extraListeners = append(t.extraListeners, ln)
	t.wg.Add(1)
	go t.acceptLoop(ln, door{join: accept, admitted: admitted})
	return ln.Addr().String(), nil
}

// serveConn runs one admission handshake on a stream this process opened to itself,
// as the accept loop runs one on a stream a listener took. It waits for a handshake
// slot rather than refusing: the dialer is this process, not a flood.
func (t *Transport) serveConn(conn net.Conn, accept func(net.Conn) (PeerID, error), admitted func(PeerID)) error {
	select {
	case t.handshakes <- struct{}{}:
	case <-t.stopCh:
		_ = conn.Close()
		return errors.New("transport is stopped")
	}
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if !t.running.Load() {
		<-t.handshakes
		_ = conn.Close()
		return errors.New("transport is stopped")
	}
	t.wg.Add(1)
	go t.handshake(conn, door{join: accept, admitted: admitted})
	return nil
}

// acceptLoop handles incoming connections
func (t *Transport) acceptLoop(ln net.Listener, d door) {
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
		if d.peer == nil && d.join == nil {
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
		go t.handshake(conn, d)
	}
}

// Release the unauthenticated budget before running the bounded participant gate.
func (t *Transport) handshake(conn net.Conn, d door) {
	defer t.wg.Done()
	t.holdPending(conn)

	accept, served, link, err := d.open(conn, t.config.ConnectTimeout)
	var id PeerID
	if err == nil {
		id, err = accept(link)
	}
	t.releasePending(conn)
	<-t.handshakes
	if err != nil {
		_ = conn.Close()
		t.report(err)
		return
	}
	// AddConnectionAs closes the connection on every failure of its own.
	if _, err := t.peers.AddConnectionAs(link, id, served); err != nil {
		t.report(err)
		return
	}
	if served && d.admitted != nil {
		d.admitted(id)
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
	if _, err := t.peers.AddConnectionAs(conn, id, false); err != nil {
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

// Totals are the bytes every link carried, by kind before deflate and on the wire.
func (t *Transport) Totals() LinkTotals { return t.peers.Totals() }

// Connected reports whether one peer is still attached.
func (t *Transport) Connected(id PeerID) bool { return t.peers.Connected(id) }

// Disconnect drops one peer, reporting whether it was connected.
func (t *Transport) Disconnect(id PeerID) bool { return t.peers.Disconnect(id) }

// Addr returns the bound listener address, nil for a client or before Start.
func (t *Transport) Addr() net.Addr {
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if t.listener == nil {
		return nil
	}
	return t.listener.Addr()
}

// Addrs are every door's bound address, the one Addr names first.
func (t *Transport) Addrs() []net.Addr {
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if t.listener == nil {
		return nil
	}
	addrs := []net.Addr{t.listener.Addr()}
	for _, ln := range t.extraListeners {
		addrs = append(addrs, ln.Addr())
	}
	return addrs
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

	t.listenMu.Lock()
	close(t.stopCh)
	for _, ln := range t.extraListeners {
		_ = ln.Close()
	}
	if t.listener != nil {
		t.listener.Close()
	}
	t.listenMu.Unlock()

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
