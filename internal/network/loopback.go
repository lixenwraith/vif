package network

import "sync"

// Loopback is an in-process transport pair: what one side sends, the other
// drains on its next tick. It is the two-participant harness — a real socket adds
// framing and latency, neither of which the domain rules depend on.
type Loopback struct {
	mu    sync.Mutex
	peer  *Loopback
	id    PeerID // the peer id this side reports for the other
	queue []Inbound
	up    bool
}

// NewLoopbackPair returns two connected endpoints. Each reports the other under
// the given peer id, so a roster keyed on PeerID distinguishes them.
func NewLoopbackPair(a, b PeerID) (*Loopback, *Loopback) {
	x := &Loopback{id: b, up: true}
	y := &Loopback{id: a, up: true}
	x.peer, y.peer = y, x
	x.deliver(Inbound{Kind: InboundConnect, Peer: b})
	y.deliver(Inbound{Kind: InboundConnect, Peer: a})
	return x, y
}

// deliver appends to this side's inbound queue
func (l *Loopback) deliver(in Inbound) {
	l.mu.Lock()
	l.queue = append(l.queue, in)
	l.mu.Unlock()
}

// Send delivers to the far side, whatever peer id the caller names: a two-party
// link has exactly one destination.
func (l *Loopback) Send(peerID uint32, msgType uint8, payload []byte) bool {
	if !l.up {
		return false
	}
	// Copy: the caller's buffer is reused between ticks
	body := append([]byte(nil), payload...)
	l.peer.deliver(Inbound{
		Kind: InboundMessage,
		Peer: l.peer.id,
		Msg:  NewMessage(MessageType(msgType), body),
	})
	return true
}

// Broadcast is Send to the one far side
func (l *Loopback) Broadcast(msgType uint8, payload []byte) { l.Send(uint32(l.id), msgType, payload) }

// PeerCount reports one while the link is up; D-14 reads it to latch the map
func (l *Loopback) PeerCount() int {
	if l.up {
		return 1
	}
	return 0
}

// IsRunning reports link state
func (l *Loopback) IsRunning() bool { return l.up }

// Drain moves this side's pending notifications into dst
func (l *Loopback) Drain(dst []Inbound) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := min(len(dst), len(l.queue))
	copy(dst, l.queue[:n])
	l.queue = append(l.queue[:0], l.queue[n:]...)
	return n
}

// Close drops the link; both sides observe a disconnect on their next drain
func (l *Loopback) Close() {
	if !l.up {
		return
	}
	l.up = false
	l.peer.deliver(Inbound{Kind: InboundDisconnect, Peer: l.peer.id})
	l.deliver(Inbound{Kind: InboundDisconnect, Peer: l.id})
}
