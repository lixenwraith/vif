package network

import (
	"bufio"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
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

	// served is a stream this end admitted through a join handshake, so the
	// participant on it is behind this one; a dialled or peer link is not.
	served bool

	// I/O, and the frame header scratch each loop owns
	conn   net.Conn
	stream *flateStream
	rhdr   [HeaderSize]byte
	whdr   [HeaderSize]byte

	// Send queue
	sendCh chan *Message

	readTimeout  time.Duration
	writeTimeout time.Duration

	// Lifecycle
	closeCh   chan struct{}
	closeOnce sync.Once
	done      chan struct{} // closed once the writer has flushed and closed the socket

	mu sync.RWMutex
}

// newPeer creates a peer from an established connection
func newPeer(id PeerID, conn net.Conn, cfg *Config) *Peer {
	p := &Peer{
		ID:           id,
		Addr:         conn.RemoteAddr().String(),
		conn:         conn,
		stream:       openFlateStream(conn, cfg.ReadBufferSize, cfg.WriteBufferSize),
		sendCh:       make(chan *Message, cfg.SendQueueSize),
		closeCh:      make(chan struct{}),
		done:         make(chan struct{}),
		readTimeout:  cfg.ReadTimeout,
		writeTimeout: cfg.WriteTimeout,
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

// closeFlush bounds the flush a closing link gives the frames already queued on
// it, so a peer that stopped reading cannot hold a shutdown for a write timeout.
const closeFlush = time.Second

// Close initiates graceful shutdown: the writer sends what was queued before it,
// then closes the socket. A notice broadcast just before a close still arrives.
func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.State.Store(uint32(StateDisconnecting))
		_ = p.conn.SetWriteDeadline(time.Now().Add(closeFlush))
		close(p.closeCh)
	})
}

// readLoop reads messages until the stream ends or its next frame is later than the
// read timeout: the far end answers this end's probes, so that link has stopped.
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
		msg, err := decode(p.stream, MaxPayloadSize, p.rhdr[:])
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

// writeLoop sends queued messages, and on close the ones still queued, and owns
// the socket's close.
func (p *Peer) writeLoop() {
	defer close(p.done)
	defer p.conn.Close()
	defer p.Close()

	linger := time.NewTimer(0)
	<-linger.C
	defer linger.Stop()
	for {
		var msg *Message
		select {
		case <-p.closeCh:
			p.flushQueued(false)
			return
		case msg = <-p.sendCh:
		}
		if p.writeTimeout > 0 {
			_ = p.conn.SetWriteDeadline(time.Now().Add(p.writeTimeout))
		}
		// One flush per burst: a tick queues its epoch, syncs and digests together,
		// and the linger lets the rest of them join the first in one write.
		linger.Reset(writeLinger)
	batch:
		for {
			msg.Seq = p.OutSeq.Add(1)
			msg.Ack = p.InSeq.Load()
			if err := msg.encode(p.stream, p.whdr[:]); err != nil {
				return
			}
			select {
			case msg = <-p.sendCh:
			case <-linger.C:
				break batch
			case <-p.closeCh:
				p.flushQueued(true)
				return
			}
		}
		if err := p.stream.Flush(); err != nil {
			return
		}
	}
}

// flushQueued sends what is encoded and still queued, under closeFlush. encoded
// says a burst's frames wait in the stream; with nothing to add to them there is
// nothing to flush, and a sync marker alone would hold the close on a peer that
// stopped reading for the whole bound.
func (p *Peer) flushQueued(encoded bool) {
	_ = p.conn.SetWriteDeadline(time.Now().Add(closeFlush))
	for {
		select {
		case msg := <-p.sendCh:
			msg.Seq = p.OutSeq.Add(1)
			msg.Ack = p.InSeq.Load()
			if msg.encode(p.stream, p.whdr[:]) != nil {
				return
			}
			encoded = true
		default:
			if encoded {
				_ = p.stream.Flush()
			}
			return
		}
	}
}

// flateStream carries a link's frames through one deflate stream per direction, so
// each frame is compressed against the ones before it rather than alone. Until
// opened it passes bytes through plain, which is how a join's handshake travels.
type flateStream struct {
	net.Conn
	r  io.Reader
	bw *bufio.Writer
	w  *flate.Writer
}

// openFlateStream opens conn's stream in place, so every holder of a join's stream
// reads through the one decoder that has seen its bytes.
func openFlateStream(conn net.Conn, readBuf, writeBuf int) *flateStream {
	s, ok := conn.(*flateStream)
	if !ok {
		s = &flateStream{Conn: conn}
	}
	if s.w == nil {
		s.bw = bufio.NewWriterSize(s.Conn, max(writeBuf, 4096))
		s.w, _ = flate.NewWriter(s.bw, parameter.NetworkStreamLevel) // a valid level cannot fail
		s.r = flate.NewReader(bufio.NewReaderSize(s.Conn, max(readBuf, 4096)))
	}
	return s
}

func (s *flateStream) Read(p []byte) (int, error) {
	if s.r == nil {
		return s.Conn.Read(p)
	}
	return s.r.Read(p)
}

func (s *flateStream) Write(p []byte) (int, error) {
	if s.w == nil {
		return s.Conn.Write(p)
	}
	return s.w.Write(p)
}

// Flush sends everything written so far.
func (s *flateStream) Flush() error {
	if s.w == nil {
		return nil
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	return s.bw.Flush()
}

// PeerManager handles multiple peer connections
type PeerManager struct {
	mu       sync.RWMutex
	peers    map[PeerID]*Peer
	maxPeers int
	config   *Config

	// Callbacks
	onConnect    func(id PeerID, served bool)
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
	onConnect func(id PeerID, served bool),
	onDisconnect func(PeerID),
	onMessage func(PeerID, *Message),
) {
	pm.onConnect = onConnect
	pm.onDisconnect = onDisconnect
	pm.onMessage = onMessage
}

// AddConnectionAs registers a stream under its session-assigned participant ID;
// served says the far end joined the session through this one.
func (pm *PeerManager) AddConnectionAs(conn net.Conn, id PeerID, served bool) (PeerID, error) {
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
	peer.served = served
	pm.peers[id] = peer

	// Start I/O loops
	go peer.readLoop(pm.handleMessage)
	go peer.writeLoop()
	go pm.monitorPeer(peer)

	if pm.onConnect != nil {
		pm.onConnect(id, served)
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

// Close disconnects all peers and waits for their queued frames, flushed side by
// side under closeFlush.
func (pm *PeerManager) Close() {
	pm.mu.Lock()
	peers := pm.peers
	pm.peers = make(map[PeerID]*Peer)
	pm.mu.Unlock()

	for _, peer := range peers {
		peer.Close()
	}
	for _, peer := range peers {
		<-peer.done
	}
}

// dial connects to a target by the transport it names, or through the
// configuration's own Dial.
func dial(target string, cfg *Config) (net.Conn, error) {
	if cfg.Dial != nil {
		return cfg.Dial()
	}
	e, err := ParseEndpoint(target)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	switch {
	case e.Scheme == SchemeWebSocket:
		conn, err = dialWebSocket(e.Addr, cfg.ConnectTimeout, cfg.TLS)
	case !buildDialsTCP:
		err = errors.New("TCP unavailable in a browser")
	default:
		conn, err = (&net.Dialer{Timeout: cfg.ConnectTimeout}).Dial("tcp", e.Addr)
	}
	return conn, plainNetError(err)
}

// Pipe is an in-process stream with a socket's receive buffering: a write completes
// once the far end holds it, not once it reads it, so a joiner installing its world
// does not stall, and then time out, the writer serving it.
func Pipe() (net.Conn, net.Conn) {
	a, b := net.Pipe()
	return spool(a), spool(b)
}

// inProcess is the far end of a Pipe as its near end sees it: no dialler, so no
// address to spend a join budget for.
type inProcess struct{}

func (inProcess) Network() string { return "pipe" }
func (inProcess) String() string  { return "pipe" }

// spooled reads its pipe end ahead into memory, as a kernel reads a socket.
type spooled struct {
	net.Conn
	wake     chan struct{}
	mu       sync.Mutex
	buf      []byte
	off      int
	err      error
	deadline time.Time
}

func spool(c net.Conn) *spooled {
	s := &spooled{Conn: c, wake: make(chan struct{}, 1)}
	go s.pump()
	return s
}

func (s *spooled) pump() {
	chunk := make([]byte, 32<<10)
	for {
		n, err := s.Conn.Read(chunk)
		s.mu.Lock()
		s.buf = append(s.buf, chunk[:n]...)
		s.err = err
		s.mu.Unlock()
		s.signal()
		if err != nil {
			return
		}
	}
}

func (s *spooled) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *spooled) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		n := copy(p, s.buf[s.off:])
		if s.off += n; s.off == len(s.buf) {
			s.buf, s.off = s.buf[:0], 0
		}
		err, deadline := s.err, s.deadline
		s.mu.Unlock()
		if n > 0 || len(p) == 0 {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
		var expire <-chan time.Time
		var timer *time.Timer
		if !deadline.IsZero() {
			wait := time.Until(deadline)
			if wait <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(wait)
			expire = timer.C
		}
		select {
		case <-s.wake:
		case <-expire:
			return 0, os.ErrDeadlineExceeded
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (s *spooled) RemoteAddr() net.Addr { return inProcess{} }

func (s *spooled) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.deadline = t
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *spooled) SetDeadline(t time.Time) error {
	_ = s.SetReadDeadline(t)
	return s.Conn.SetWriteDeadline(t)
}
