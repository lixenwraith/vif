package network

import (
	"net"
	"sync/atomic"
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
	p.config.OnError = p.report
	p.transport = NewTransport(&p.config)
	p.transport.SetHandlers(p.onConnect, p.onDisconnect, p.onMessage)
	return p
}

// Start begins the configured listener or client connection.
func (p *SocketPort) Start() error { return p.transport.Start() }

// Close stops all socket I/O.
func (p *SocketPort) Close() error { return p.transport.Stop() }

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
	if msg != nil && msg.Type == MsgHeartbeat {
		return
	}
	if msg != nil && msg.Type == MsgReady {
		p.ready.Add(1)
		p.signal()
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

func (p *SocketPort) report(err error) {
	if p.onError != nil {
		p.onError(err)
	}
	select {
	case p.errors <- err:
	default:
	}
	p.signal()
}
