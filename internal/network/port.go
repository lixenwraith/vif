package network

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// SocketPort owns stream goroutines and exposes only a tick-drained inbound queue.
type SocketPort struct {
	config    Config
	transport *Transport
	inbound   chan Inbound
	changes   chan struct{}
	errors    chan error

	dropped  atomic.Uint64
	refused  atomic.Uint64
	received atomic.Uint64
	ever     atomic.Bool
	ready    atomic.Int64

	// The link measurement is entirely the port's. Probes leave on a timer of
	// their own rather than on a tick, because a stalled or paused instance still
	// has a link and a link that has gone quiet is exactly what a probe is for;
	// echoes are answered inside onMessage, before the frame could reach a tick,
	// so what the round trip measures is the wire.
	meterMu sync.Mutex
	meters  map[PeerID]*linkMeter
	report  atomic.Pointer[LinkReport]

	probeOnce sync.Once
	closeOnce sync.Once
	probeStop chan struct{}
	probeDone chan struct{}

	onError func(error)
}

// NewSocketPort creates a poll endpoint without starting network I/O.
func NewSocketPort(cfg *Config) *SocketPort {
	base := DefaultConfig()
	if cfg != nil {
		copy := *cfg
		base = &copy
	}
	if base.RecvQueueSize <= 0 {
		base.RecvQueueSize = DefaultConfig().RecvQueueSize
	}
	p := &SocketPort{
		config:  *base,
		inbound: make(chan Inbound, base.RecvQueueSize),
		changes: make(chan struct{}, 1),
		errors:  make(chan error, 8),
		onError: base.OnError,
	}
	p.config.OnError = p.reportError
	p.meters = make(map[PeerID]*linkMeter)
	p.probeStop = make(chan struct{})
	p.probeDone = make(chan struct{})
	p.transport = NewTransport(&p.config)
	p.transport.SetHandlers(p.onConnect, p.onDisconnect, p.onMessage)
	return p
}

// Start begins the configured listener or client connection, and the link
// measurement that runs beside it.
func (p *SocketPort) Start() error {
	if err := p.transport.Start(); err != nil {
		return err
	}
	p.probeOnce.Do(func() { go p.probeLoop() })
	return nil
}

// Close stops all socket I/O and the probe loop with it. A port closed twice —
// a failed startup unwinding over a deferred close — is the ordinary case, and a
// port that never started has no loop to wait for.
func (p *SocketPort) Close() error {
	p.closeOnce.Do(func() {
		neverStarted := false
		p.probeOnce.Do(func() { neverStarted = true })
		close(p.probeStop)
		if !neverStarted {
			<-p.probeDone
		}
	})
	return p.transport.Stop()
}

// ParticipantID is the canonical source order used by the barrier.
func (p *SocketPort) ParticipantID() uint32 { return uint32(p.config.ParticipantID) }

// BarrierDelayTicks returns the playout lead negotiated by the handshake.
func (p *SocketPort) BarrierDelayTicks() uint64 { return p.config.BarrierDelayTicks }

// Send queues one framed message for a canonical participant.
func (p *SocketPort) Send(peerID uint32, msgType uint8, payload []byte) bool {
	return p.transport.Send(PeerID(peerID), NewMessage(MessageType(msgType), payload))
}

// Broadcast queues one independently sequenced frame per peer. A peer whose send
// queue is full refuses the frame; the count is retained rather than discarded,
// because a refused crossing is a divergence the session must be able to see.
func (p *SocketPort) Broadcast(msgType uint8, payload []byte) {
	p.BroadcastExcept(0, msgType, payload)
}

// BroadcastExcept queues one frame per peer but one, for relaying an artifact onward
// without returning it to the link it arrived on.
func (p *SocketPort) BroadcastExcept(exclude uint32, msgType uint8, payload []byte) {
	refused := p.transport.BroadcastExcept(PeerID(exclude), NewMessage(MessageType(msgType), payload))
	if refused > 0 {
		p.refused.Add(uint64(refused))
	}
}

// Connected reports whether one participant's stream is still attached. It answers
// about that participant rather than about the session, which is what a join
// waiting on one joiner needs: a peer count says nothing when others are present.
func (p *SocketPort) Connected(peerID uint32) bool {
	return p.transport.Connected(PeerID(peerID))
}

// Disconnect drops one participant's stream, reporting whether it was connected.
// It is how a coordinator refuses a join that got as far as being admitted: a
// participant left holding a handshake it could not finish would otherwise stay in
// the session receiving crossings for a world it never installed.
func (p *SocketPort) Disconnect(peerID uint32) bool {
	return p.transport.Disconnect(PeerID(peerID))
}

// PeerCount reports the currently connected participants.
func (p *SocketPort) PeerCount() int { return p.transport.PeerCount() }

// IsRunning reports whether the listener or client transport is active.
func (p *SocketPort) IsRunning() bool { return p.transport.IsRunning() }

// ConnectionState distinguishes initial wait from a lost established peer.
func (p *SocketPort) ConnectionState() ConnState {
	if !p.IsRunning() {
		return StateDisconnected
	}
	if p.PeerCount() > 0 {
		return StateConnected
	}
	if p.ever.Load() {
		return StateDisconnected
	}
	return StateConnecting
}

// Addr returns the bound address, nil for a client or before Start.
func (p *SocketPort) Addr() net.Addr { return p.transport.Addr() }

// Peers returns the connected participants in a stable order, for a caller that
// schedules per link rather than per session.
func (p *SocketPort) Peers() []uint32 {
	ids := p.transport.Peers()
	out := make([]uint32, len(ids))
	for i, id := range ids {
		out[i] = uint32(id)
	}
	return out
}

// SetLinkReport publishes what this instance tells a probing peer about its own
// picture. It is called from the tick and read from the read goroutine, so it is
// stored whole and replaced rather than mutated.
func (p *SocketPort) SetLinkReport(r LinkReport) { p.report.Store(&r) }

// LinkMetric returns one peer's link estimate. The zero value is an unmeasured
// link, which a controller reads as "no evidence" rather than as a slow link.
func (p *SocketPort) LinkMetric(peer uint32) linkpace.Metrics {
	p.meterMu.Lock()
	defer p.meterMu.Unlock()
	if m, ok := p.meters[PeerID(peer)]; ok {
		return m.link.Metrics()
	}
	return linkpace.Metrics{}
}

// ObserveTransfer folds a completed bulk transfer into one link's estimate.
//
// A join's capture is a throughput measurement nothing else on the link can make
// that early: the bytes went out, the joiner answered when it had them all, and
// the sender was pushing the whole time. It is the number an admission decision
// has before a single probe has completed a round trip.
func (p *SocketPort) ObserveTransfer(peer uint32, bytes int64, elapsed time.Duration) {
	p.meterMu.Lock()
	defer p.meterMu.Unlock()
	p.meterLocked(PeerID(peer)).link.ObserveTransfer(bytes, elapsed)
}

// meterLocked returns one peer's measurement state, creating it on first use.
// Caller MUST hold meterMu.
func (p *SocketPort) meterLocked(id PeerID) *linkMeter {
	m, ok := p.meters[id]
	if !ok {
		m = newLinkMeter()
		p.meters[id] = m
	}
	return m
}

// probeLoop emits one probe per peer per interval.
//
// It is wall-paced and off the tick deliberately. A cadence is a property of the
// simulation and is counted in ticks; the *link* is not, and measuring it from a
// loop the simulation drives would make a stalled instance stop noticing that
// its link had gone.
func (p *SocketPort) probeLoop() {
	defer close(p.probeDone)
	ticker := time.NewTicker(parameter.NetworkProbeInterval) // [wall] the link, not the game
	defer ticker.Stop()
	for {
		select {
		case <-p.probeStop:
			return
		case <-ticker.C:
		}
		p.probePeers()
	}
}

// probePeers sends one probe to each connected participant and retires the
// meters of the ones that have gone.
func (p *SocketPort) probePeers() {
	peers := p.transport.Peers()
	now := time.Now() // [wall] the round trip's own origin, returned untouched by the echo

	p.meterMu.Lock()
	live := make(map[PeerID]struct{}, len(peers))
	seqs := make([]uint32, len(peers))
	for i, id := range peers {
		live[id] = struct{}{}
		seqs[i] = p.meterLocked(id).nextProbe()
	}
	for id := range p.meters {
		if _, ok := live[id]; !ok {
			delete(p.meters, id)
		}
	}
	p.meterMu.Unlock()

	for i, id := range peers {
		p.transport.Send(id, NewMessage(MsgLinkProbe, encodeProbe(seqs[i], now)))
	}
}

// answerProbe replies to one probe on the goroutine that read it, which is what
// keeps the measurement a wire round trip rather than a statement about how
// often this instance runs a tick.
func (p *SocketPort) answerProbe(id PeerID, payload []byte) {
	in, _, ok := p.transport.Bytes(id)
	if !ok {
		return
	}
	var report LinkReport
	if r := p.report.Load(); r != nil {
		report = *r
	}
	if echo := encodeEcho(payload, in, report); echo != nil {
		p.transport.Send(id, NewMessage(MsgLinkEcho, echo))
	}
}

// observeEcho folds one answered probe into that link's estimate.
func (p *SocketPort) observeEcho(id PeerID, payload []byte) {
	seq, sent, delivered, report, ok := decodeEcho(payload)
	if !ok {
		return
	}
	_, out, live := p.transport.Bytes(id)
	if !live {
		return
	}
	p.meterMu.Lock()
	p.meterLocked(id).observe(time.Now(), sent, seq, delivered, out, report) // [wall]
	p.meterMu.Unlock()
}

// Drain implements the game-side poll contract without blocking a tick.
func (p *SocketPort) Drain(dst []Inbound) int {
	n := 0
	for n < len(dst) {
		select {
		case in := <-p.inbound:
			dst[n] = in
			n++
		default:
			return n
		}
	}
	return n
}

// Inject replays a frame the join handshake read off the stream before this port
// owned it. A mid-run joiner reads its start gate and its capture from the raw
// connection, and the host's crossings arrive on the same stream in the meantime;
// they are the epochs produced between admission and install, so they are held and
// handed to the port here rather than dropped.
func (p *SocketPort) Inject(peer uint32, msgType uint8, payload []byte) {
	p.onMessage(PeerID(peer), NewMessage(MessageType(msgType), payload))
}

// Changes wakes startup coordination after connect, disconnect or ready.
func (p *SocketPort) Changes() <-chan struct{} { return p.changes }

// ReadyCount reports peers that passed the tick-zero start gate.
func (p *SocketPort) ReadyCount() int { return int(p.ready.Load()) }

// Errors exposes asynchronous accept and handshake failures.
func (p *SocketPort) Errors() <-chan error { return p.errors }

// Dropped reports transport notifications lost to a full poll buffer.
func (p *SocketPort) Dropped() uint64 { return p.dropped.Load() }

// Refused reports outbound frames a peer's send queue could not take.
func (p *SocketPort) Refused() uint64 { return p.refused.Load() }

// Received reports complete non-control frames admitted by the read loop.
func (p *SocketPort) Received() uint64 { return p.received.Load() }

func (p *SocketPort) onConnect(id PeerID) {
	p.ever.Store(true)
	p.push(Inbound{Kind: InboundConnect, Peer: id})
	p.signal()
}

func (p *SocketPort) onDisconnect(id PeerID) {
	p.push(Inbound{Kind: InboundDisconnect, Peer: id})
	p.signal()
}

func (p *SocketPort) onMessage(id PeerID, msg *Message) {
	if msg == nil {
		return
	}
	switch msg.Type {
	case MsgHeartbeat:
		return
	case MsgReady:
		p.ready.Add(1)
		p.signal()
		return
	// The round trip never reaches the game. Answering here rather than from a
	// tick is what makes the number a property of the link, and swallowing the
	// echo here is what keeps network timing out of the simulation: the world's
	// only view of it is the estimate it may schedule transport from.
	case MsgLinkProbe:
		p.answerProbe(id, msg.Payload)
		return
	case MsgLinkEcho:
		p.observeEcho(id, msg.Payload)
		return
	}
	p.received.Add(1)
	p.push(Inbound{Kind: InboundMessage, Peer: id, Msg: msg})
	p.signal()
}

func (p *SocketPort) push(in Inbound) {
	select {
	case p.inbound <- in:
	default:
		p.dropped.Add(1)
	}
}

func (p *SocketPort) signal() {
	select {
	case p.changes <- struct{}{}:
	default:
	}
}

func (p *SocketPort) reportError(err error) {
	if p.onError != nil {
		p.onError(err)
	}
	select {
	case p.errors <- err:
	default:
	}
	p.signal()
}
