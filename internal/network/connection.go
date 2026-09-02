package network

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// PeerID uniquely identifies a connected peer
type PeerID uint32

// ConnState represents connection lifecycle state
type ConnState uint8

const (
	StateDisconnected ConnState = iota
	StateConnecting
	StateConnected
	StateDisconnecting
)

// Peer represents a remote endpoint
type Peer struct {
	ID       PeerID
	Addr     string
	State    atomic.Uint32 // ConnState
	LastSeen atomic.Int64  // UnixNano

	// Sequence tracking
	OutSeq atomic.Uint32 // Next outbound sequence
	InSeq  atomic.Uint32 // Last processed inbound sequence

	// Byte accounting, cumulative for the life of this connection. InBytes is
	// complete frames this end has decoded; OutBytes is complete frames it has
	// queued. The pair is what makes a link measurable: a peer echoes its InBytes
	// back, and the difference against this end's OutBytes is the backlog — bytes
	// offered to the link and not yet arrived, which is the difference between a
	// link that is fast and a sender that was idle.
	//
	// OutBytes counts what was queued rather than what the socket wrote, and that
	// is the number the backlog wants: a frame waiting in the send queue is as
	// undelivered as one waiting in a router.
	InBytes  atomic.Uint64
	OutBytes atomic.Uint64

	// I/O
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer

	// Send queue
	sendCh chan *Message

	readTimeout       time.Duration
	writeTimeout      time.Duration
	heartbeatInterval time.Duration

	// Lifecycle
	closeCh   chan struct{}
	closeOnce sync.Once

	mu sync.RWMutex
}

// newPeer creates a peer from an established connection
func newPeer(id PeerID, conn net.Conn, cfg *Config) *Peer {
	p := &Peer{
		ID:                id,
		Addr:              conn.RemoteAddr().String(),
		conn:              conn,
		reader:            bufio.NewReaderSize(conn, cfg.ReadBufferSize),
		writer:            bufio.NewWriterSize(conn, cfg.WriteBufferSize),
		sendCh:            make(chan *Message, cfg.SendQueueSize),
		closeCh:           make(chan struct{}),
		readTimeout:       cfg.DisconnectTimeout,
		writeTimeout:      cfg.WriteTimeout,
		heartbeatInterval: cfg.HeartbeatInterval,
	}
	if p.readTimeout <= 0 {
		p.readTimeout = cfg.ReadTimeout
	}
	p.State.Store(uint32(StateConnected))
	p.LastSeen.Store(time.Now().UnixNano())
	return p
}

// Send queues a message for transmission
// Returns false if peer is disconnected or queue full
func (p *Peer) Send(msg *Message) bool {
	if ConnState(p.State.Load()) != StateConnected {
		return false
	}

	select {
	case p.sendCh <- msg:
		p.OutBytes.Add(uint64(HeaderSize + len(msg.Payload)))
		return true
	default:
		return false // Queue full
	}
}

// Close initiates graceful shutdown
func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.State.Store(uint32(StateDisconnecting))
		close(p.closeCh)
		_ = p.conn.Close()
	})
}

// readLoop reads messages from the connection
func (p *Peer) readLoop(handler func(PeerID, *Message)) {
	defer p.Close()

	for {
		select {
		case <-p.closeCh:
			return
		default:
		}

		if p.readTimeout > 0 {
			_ = p.conn.SetReadDeadline(time.Now().Add(p.readTimeout))
		}
		msg, err := Decode(p.reader)
		if err != nil {
			return
		}

		p.LastSeen.Store(time.Now().UnixNano())
		p.InBytes.Add(uint64(HeaderSize + len(msg.Payload)))

		// Track inbound sequence
		if msg.Seq > p.InSeq.Load() {
			p.InSeq.Store(msg.Seq)
		}

		handler(p.ID, msg)
	}
}

// writeLoop sends queued messages
func (p *Peer) writeLoop() {
	defer p.Close()
	var heartbeat <-chan time.Time
	var ticker *time.Ticker
	if p.heartbeatInterval > 0 {
		ticker = time.NewTicker(p.heartbeatInterval)
		heartbeat = ticker.C
		defer ticker.Stop()
	}

	for {
		var msg *Message
		select {
		case <-p.closeCh:
			return
		case msg = <-p.sendCh:
		case <-heartbeat:
			msg = NewMessage(MsgHeartbeat, nil)
		}
		msg.Seq = p.OutSeq.Add(1)
		msg.Ack = p.InSeq.Load()
		if p.writeTimeout > 0 {
			_ = p.conn.SetWriteDeadline(time.Now().Add(p.writeTimeout))
		}
		if err := msg.Encode(p.writer); err != nil {
			return
		}
		if err := p.writer.Flush(); err != nil {
			return
		}
	}
}

// PeerManager handles multiple peer connections
type PeerManager struct {
	mu       sync.RWMutex
	peers    map[PeerID]*Peer
	maxPeers int
	config   *Config

	// Callbacks
	onConnect    func(PeerID)
	onDisconnect func(PeerID)
	onMessage    func(PeerID, *Message)
}

// NewPeerManager creates a peer manager
func NewPeerManager(cfg *Config) *PeerManager {
	return &PeerManager{
		peers:    make(map[PeerID]*Peer),
		maxPeers: cfg.MaxPeers,
		config:   cfg,
	}
}

// SetHandlers configures event callbacks
func (pm *PeerManager) SetHandlers(
	onConnect func(PeerID),
	onDisconnect func(PeerID),
	onMessage func(PeerID, *Message),
) {
	pm.onConnect = onConnect
	pm.onDisconnect = onDisconnect
	pm.onMessage = onMessage
}

// AddConnectionAs registers a stream under its session-assigned participant ID.
func (pm *PeerManager) AddConnectionAs(conn net.Conn, id PeerID) (PeerID, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if id == 0 {
		_ = conn.Close()
		return 0, errors.New("peer id must be non-zero")
	}
	if len(pm.peers) >= pm.maxPeers {
		_ = conn.Close()
		return 0, errors.New("max peers reached")
	}
	if _, exists := pm.peers[id]; exists {
		_ = conn.Close()
		return 0, fmt.Errorf("peer %d already connected", id)
	}

	peer := newPeer(id, conn, pm.config)
	pm.peers[id] = peer

	// Start I/O loops
	go peer.readLoop(pm.handleMessage)
	go peer.writeLoop()
	go pm.monitorPeer(peer)

	if pm.onConnect != nil {
		pm.onConnect(id)
	}

	return id, nil
}

// handleMessage routes received messages
func (pm *PeerManager) handleMessage(id PeerID, msg *Message) {
	if pm.onMessage != nil {
		pm.onMessage(id, msg)
	}
}

// monitorPeer watches for disconnection
func (pm *PeerManager) monitorPeer(peer *Peer) {
	<-peer.closeCh
	peer.State.Store(uint32(StateDisconnected))

	pm.mu.Lock()
	delete(pm.peers, peer.ID)
	pm.mu.Unlock()

	if pm.onDisconnect != nil {
		pm.onDisconnect(peer.ID)
	}
}

// Send transmits a message to a specific peer
func (pm *PeerManager) Send(id PeerID, msg *Message) bool {
	pm.mu.RLock()
	peer, ok := pm.peers[id]
	pm.mu.RUnlock()

	if !ok {
		return false
	}
	return peer.Send(msg)
}

// Broadcast sends a message to all connected peers and reports how many could
// not take it. A refused send is a frame the peer never sees; for a crossing that
// is a permanent lockstep divergence, so the count is returned rather than dropped.
func (pm *PeerManager) Broadcast(msg *Message) int { return pm.BroadcastExcept(0, msg) }

// BroadcastExcept is Broadcast skipping one participant, for relaying an artifact
// onward without returning it to the link it arrived on. Participant zero is never
// assigned, so it excludes nothing.
func (pm *PeerManager) BroadcastExcept(exclude PeerID, msg *Message) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	refused := 0
	for id, peer := range pm.peers {
		if id == exclude {
			continue
		}
		// Clone message for independent sequence numbers
		clone := *msg
		if !peer.Send(&clone) {
			refused++
		}
	}
	return refused
}

// Connected reports whether one peer is still in the manager.
func (pm *PeerManager) Connected(id PeerID) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	_, ok := pm.peers[id]
	return ok
}

// Disconnect drops one peer, reporting whether it was connected. The monitor
// goroutine notices the close and runs the ordinary departure path, so a peer
// dropped here leaves exactly the way one that lost its link does.
func (pm *PeerManager) Disconnect(id PeerID) bool {
	pm.mu.RLock()
	peer, ok := pm.peers[id]
	pm.mu.RUnlock()
	if !ok {
		return false
	}
	peer.Close()
	return true
}

// Peers returns the connected participants, for a caller that has to act on each
// link rather than on the session — a per-peer probe, a per-peer schedule.
func (pm *PeerManager) Peers() []PeerID {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make([]PeerID, 0, len(pm.peers))
	for id := range pm.peers {
		out = append(out, id)
	}
	slices.Sort(out) // a stable order, so a per-peer loop is reproducible
	return out
}

// Bytes reports one link's cumulative decoded and queued frame bytes.
func (pm *PeerManager) Bytes(id PeerID) (in, out uint64, ok bool) {
	pm.mu.RLock()
	peer, exists := pm.peers[id]
	pm.mu.RUnlock()
	if !exists {
		return 0, 0, false
	}
	return peer.InBytes.Load(), peer.OutBytes.Load(), true
}

// PeerCount returns current connected peer count
func (pm *PeerManager) PeerCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.peers)
}

// Close disconnects all peers
func (pm *PeerManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, peer := range pm.peers {
		peer.Close()
	}
	pm.peers = make(map[PeerID]*Peer)
}

// dial establishes a connection with optional TLS
func dial(addr string, cfg *Config) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: cfg.ConnectTimeout,
	}

	if cfg.TLS != nil {
		return tls.DialWithDialer(dialer, "tcp", addr, cfg.TLS)
	}
	return dialer.Dial("tcp", addr)
}
