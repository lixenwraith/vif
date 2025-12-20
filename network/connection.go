package network

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
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

	// I/O
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer

	// Send queue
	sendCh chan *Message

	// Lifecycle
	closeCh   chan struct{}
	closeOnce sync.Once

	mu sync.RWMutex
}

// newPeer creates a peer from an established connection
func newPeer(id PeerID, conn net.Conn, sendQueueSize int) *Peer {
	p := &Peer{
		ID:      id,
		Addr:    conn.RemoteAddr().String(),
		conn:    conn,
		reader:  bufio.NewReaderSize(conn, 64*1024),
		writer:  bufio.NewWriterSize(conn, 64*1024),
		sendCh:  make(chan *Message, sendQueueSize),
		closeCh: make(chan struct{}),
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

	// Assign sequence number
	msg.Seq = p.OutSeq.Add(1)
	msg.Ack = p.InSeq.Load()

	select {
	case p.sendCh <- msg:
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
		p.conn.Close()
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

		msg, err := Decode(p.reader)
		if err != nil {
			return
		}

		p.LastSeen.Store(time.Now().UnixNano())

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

	for {
		select {
		case <-p.closeCh:
			return
		case msg := <-p.sendCh:
			if err := msg.Encode(p.writer); err != nil {
				return
			}
			if err := p.writer.Flush(); err != nil {
				return
			}
		}
	}
}

// PeerManager handles multiple peer connections
type PeerManager struct {
	mu       sync.RWMutex
	peers    map[PeerID]*Peer
	nextID   atomic.Uint32
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

// AddConnection registers a new peer from a raw connection
func (pm *PeerManager) AddConnection(conn net.Conn) (PeerID, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.peers) >= pm.maxPeers {
		conn.Close()
		return 0, errors.New("max peers reached")
	}

	id := PeerID(pm.nextID.Add(1))
	peer := newPeer(id, conn, pm.config.SendQueueSize)
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

// Broadcast sends a message to all connected peers
func (pm *PeerManager) Broadcast(msg *Message) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, peer := range pm.peers {
		// Clone message for independent sequence numbers
		clone := *msg
		peer.Send(&clone)
	}
}

// GetPeer retrieves a peer by ID
func (pm *PeerManager) GetPeer(id PeerID) (*Peer, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.peers[id]
	return p, ok
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