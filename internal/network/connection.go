package network

import (
	"bufio"
	"bytes"
	"cmp"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
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

	// Frame bytes for the life of this connection: InBytes decoded, OutBytes queued
	// rather than written, since a frame waiting in the send queue is as undelivered
	// as one waiting in a router. A peer echoes its InBytes back, and the difference
	// against this end's OutBytes is the backlog, which tells a fast link from an
	// idle sender.
	InBytes  atomic.Uint64
	OutBytes atomic.Uint64

	// outKind is OutBytes by what the frames carried, the reading a link's traffic is
	// planned by; the stream counts the wire bytes they became.
	outKind [KindCount]atomic.Uint64

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
		n := uint64(HeaderSize + len(msg.Payload))
		p.OutBytes.Add(n)
		p.outKind[msg.Type.Kind()].Add(n)
		return true
	default:
		return false // Queue full
	}
}

// totals is what this link carried: framed bytes by kind and the wire's.
func (p *Peer) totals() (t LinkTotals) {
	for k := range t.Framed {
		t.Framed[k] = p.outKind[k].Load()
	}
	t.WireIn, t.WireOut = p.stream.in.Load(), p.stream.out.Load()
	return t
}

// LinkTotals are the bytes a transport's links carried, over every link it has had:
// framed before the stream's deflate, by what they carried, and on the wire after it.
type LinkTotals struct {
	Framed          [KindCount]uint64
	WireIn, WireOut uint64
}

func (t *LinkTotals) add(o LinkTotals) {
	for k := range t.Framed {
		t.Framed[k] += o.Framed[k]
	}
	t.WireIn += o.WireIn
	t.WireOut += o.WireOut
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
		msg, err := p.stream.readFrame(MaxPayloadSize, p.rhdr[:])
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
			p.flushQueued()
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
			if err := p.stream.writeFrame(msg, p.whdr[:]); err != nil {
				return
			}
			select {
			case msg = <-p.sendCh:
			case <-linger.C:
				break batch
			case <-p.closeCh:
				p.flushQueued()
				return
			}
		}
		if err := p.stream.Flush(); err != nil {
			return
		}
	}
}

// flushQueued sends what is encoded and still queued, under closeFlush.
func (p *Peer) flushQueued() {
	_ = p.conn.SetWriteDeadline(time.Now().Add(closeFlush))
	for {
		select {
		case msg := <-p.sendCh:
			msg.Seq = p.OutSeq.Add(1)
			msg.Ack = p.InSeq.Load()
			if p.stream.writeFrame(msg, p.whdr[:]) != nil {
				return
			}
		default:
			_ = p.stream.Flush()
			return
		}
	}
}

// flateStream carries a link's frames once its handshake is done, as segments: a
// kind byte, then for a deflate run its wire length and frame count as uvarints and
// the frames deflated against every run before it, or one raw frame whose body the
// snapshot codec deflated already, which a run would only deflate again. Until
// opened it passes bytes through plain, which is how a join's handshake travels.
type flateStream struct {
	net.Conn
	src segmentSource // the deflate runs, as the inflater reads them
	r   io.Reader     // inflates src
	bw  *bufio.Writer
	w   *flate.Writer // deflates frames into seg
	seg bytes.Buffer  // the run not yet on the wire

	// plain counts the frame bytes deflated so far, which is the far inflater's
	// window position; frames counts the run in progress.
	plain  uint64
	frames int

	// in and out are the wire's bytes, read before inflate and written after deflate.
	in, out atomic.Uint64
}

const (
	segmentDeflate byte = 0
	segmentRaw     byte = 1

	// flateWindow is the inflater's history. A run whose last frame ends exactly on
	// it is delivered before its flush marker is read, so the marker would be read
	// with whatever follows: a raw frame due there travels in the run instead.
	flateWindow = 1 << 15
)

// openFlateStream opens conn's stream in place, so every holder of a join's stream
// reads through the one decoder that has seen its bytes.
func openFlateStream(conn net.Conn, readBuf, writeBuf int) *flateStream {
	s, ok := conn.(*flateStream)
	if !ok {
		s = &flateStream{Conn: conn}
	}
	if s.w == nil {
		s.bw = bufio.NewWriterSize(wireCounter{s.Conn, &s.out}, max(writeBuf, 4096))
		s.w, _ = flate.NewWriter(&s.seg, parameter.NetworkStreamLevel) // a valid level cannot fail
		s.src.br = bufio.NewReaderSize(wireCounter{s.Conn, &s.in}, max(readBuf, 4096))
		s.r = flate.NewReader(&s.src)
	}
	return s
}

// wireCounter counts the bytes one side of a stream moves over its socket.
type wireCounter struct {
	net.Conn
	n *atomic.Uint64
}

func (c wireCounter) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.n.Add(uint64(n))
	return n, err
}

func (c wireCounter) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.n.Add(uint64(n))
	return n, err
}

// Read is the plain handshake; an opened stream is read a frame at a time.
func (s *flateStream) Read(p []byte) (int, error) {
	if s.r != nil {
		return 0, errors.New("an opened stream is read by frame")
	}
	return s.Conn.Read(p)
}

// Write deflates p into the run in progress.
func (s *flateStream) Write(p []byte) (int, error) {
	if s.w == nil {
		return s.Conn.Write(p)
	}
	n, err := s.w.Write(p)
	s.plain += uint64(n)
	return n, err
}

// writeFrame queues msg: in the run, or, with a body deflated already, raw once the
// run before it has gone out.
func (s *flateStream) writeFrame(msg *Message, header []byte) error {
	if !msg.Type.Precompressed() || (s.plain%flateWindow == 0 && s.plain > 0) {
		s.frames++
		return msg.encode(s, header)
	}
	if err := s.flushRun(); err != nil {
		return err
	}
	if err := s.bw.WriteByte(segmentRaw); err != nil {
		return err
	}
	return msg.encode(s.bw, header)
}

// flushRun puts the run in progress on the wire as one segment.
func (s *flateStream) flushRun() error {
	if s.frames == 0 {
		return nil
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	h := binary.AppendUvarint([]byte{segmentDeflate}, uint64(s.seg.Len()))
	h = binary.AppendUvarint(h, uint64(s.frames))
	if _, err := s.bw.Write(h); err != nil {
		return err
	}
	_, err := s.bw.Write(s.seg.Bytes())
	s.seg.Reset()
	s.frames = 0
	return err
}

// Flush sends everything written so far.
func (s *flateStream) Flush() error {
	if s.w == nil {
		return nil
	}
	if err := s.flushRun(); err != nil {
		return err
	}
	return s.bw.Flush()
}

// readFrame reads the next frame, whichever segment carries it; limit and header
// are decode's. A run's frames are owed by the inflater until its count is
// delivered, and its marker by the source until its wire length is read.
func (s *flateStream) readFrame(limit int, header []byte) (*Message, error) {
	if s.r == nil {
		return decode(s.Conn, limit, header)
	}
	for {
		if s.src.owed > 0 || s.src.left > 0 {
			msg, err := decode(s.r, limit, header)
			if err == nil {
				s.src.owed--
			}
			return msg, err
		}
		kind, err := s.src.header()
		if err != nil {
			return nil, err
		}
		if kind == segmentDeflate {
			continue
		}
		return decode(s.src.br, limit, header)
	}
}

// segmentSource is what the inflater reads: the deflate runs one after another and
// nothing of the raw frames between them. It is a byte reader so flate adds no
// buffer of its own, which would read past a run.
type segmentSource struct {
	br   *bufio.Reader
	left int // wire bytes of the current run not yet read
	owed int // frames of it the inflater has not delivered
}

// header reads the next segment's kind, and a run's length and count into left and
// owed.
func (s *segmentSource) header() (byte, error) {
	kind, err := s.br.ReadByte()
	if err != nil {
		return 0, err
	}
	if kind == segmentRaw {
		return kind, nil
	}
	if kind != segmentDeflate {
		return 0, fmt.Errorf("stream segment of kind %d", kind)
	}
	wire, err := binary.ReadUvarint(s.br)
	if err != nil {
		return 0, err
	}
	frames, err := binary.ReadUvarint(s.br)
	if err != nil {
		return 0, err
	}
	if wire == 0 || frames == 0 || wire > uint64(math.MaxInt32) || frames > uint64(math.MaxInt32) {
		return 0, fmt.Errorf("deflate run of %d wire bytes and %d frames", wire, frames)
	}
	s.left, s.owed = int(wire), s.owed+int(frames)
	return kind, nil
}

// next enters the run that follows. A raw frame there is the writer's fault: the
// inflater is only ever reading on to a frame a run holds.
func (s *segmentSource) next() error {
	kind, err := s.header()
	if err != nil {
		return err
	}
	if kind != segmentDeflate {
		return errors.New("a raw frame inside a deflate run")
	}
	return nil
}

func (s *segmentSource) ReadByte() (byte, error) {
	if s.left == 0 {
		if err := s.next(); err != nil {
			return 0, err
		}
	}
	b, err := s.br.ReadByte()
	if err == nil {
		s.left--
	}
	return b, err
}

func (s *segmentSource) Read(p []byte) (int, error) {
	if s.left == 0 {
		if err := s.next(); err != nil {
			return 0, err
		}
	}
	n, err := s.br.Read(p[:min(len(p), s.left)])
	s.left -= n
	return n, err
}

// PeerManager handles multiple peer connections
type PeerManager struct {
	mu       sync.RWMutex
	peers    map[PeerID]*Peer
	maxPeers int
	config   *Config
	gone     LinkTotals // what the links that closed carried

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
	pm.gone.add(peer.totals())
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

// Totals are the bytes every link carried, the closed ones included.
func (pm *PeerManager) Totals() LinkTotals {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	t := pm.gone
	for _, peer := range pm.peers {
		t.add(peer.totals())
	}
	return t
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
		conn, err = dialWebSocket(withHost(e.Addr), cfg.ConnectTimeout, cmp.Or(cfg.TLS, dialTLS()))
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
