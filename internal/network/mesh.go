package network

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// Mesh is an in-process link graph: what a node sends, its directly connected
// neighbours drain on their next tick. It is the multi-participant harness — a real
// socket adds framing and latency, neither of which the domain rules depend on, and
// unlike a single stream it can express a topology that is not a star. A chain
// A—B—C is what proves an artifact reaches a participant its producer never
// connected to.
type Mesh struct {
	mu    sync.Mutex
	nodes map[PeerID]*MeshPort
}

// NewMesh returns an empty link graph.
func NewMesh() *Mesh { return &Mesh{nodes: make(map[PeerID]*MeshPort)} }

// Node returns the endpoint for a participant, creating it on first use.
func (m *Mesh) Node(id PeerID) *MeshPort {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nodeLocked(id)
}

func (m *Mesh) nodeLocked(id PeerID) *MeshPort {
	if p, ok := m.nodes[id]; ok {
		return p
	}
	p := &MeshPort{local: id, links: make(map[PeerID]*MeshPort), up: true}
	m.nodes[id] = p
	return p
}

// Link connects two participants directly. Each observes the other as a peer, which
// is the only membership a node has: a mesh participant knows its links, not the
// roster.
func (m *Mesh) Link(a, b PeerID) {
	if a == b {
		return
	}
	m.mu.Lock()
	x, y := m.nodeLocked(a), m.nodeLocked(b)
	m.mu.Unlock()
	if !x.addLink(y) {
		return
	}
	y.addLink(x)
	x.deliver(Inbound{Kind: InboundConnect, Peer: b})
	y.deliver(Inbound{Kind: InboundConnect, Peer: a})
}

// NewLoopbackPair links two participants and returns their endpoints. The
// two-participant case is a mesh with one link.
func NewLoopbackPair(a, b PeerID) (*MeshPort, *MeshPort) {
	m := NewMesh()
	m.Link(a, b)
	return m.Node(a), m.Node(b)
}

// MeshPort is one participant's endpoint. It implements the same poll contract as
// SocketPort: outbound by direct call, inbound drained once per game tick.
type MeshPort struct {
	local PeerID

	mu    sync.Mutex
	links map[PeerID]*MeshPort
	queue []Inbound
	up    bool
}

// ParticipantID is the canonical source order used by the barrier.
func (p *MeshPort) ParticipantID() uint32 { return uint32(p.local) }

// BarrierDelayTicks returns the default playout lead used by the in-process link.
func (p *MeshPort) BarrierDelayTicks() uint64 { return parameter.NetworkBarrierDelayTicks }

// addLink records a neighbour, reporting whether it was new.
func (p *MeshPort) addLink(peer *MeshPort) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.links[peer.local]; exists {
		return false
	}
	p.links[peer.local] = peer
	return true
}

// deliver appends to this node's inbound queue
func (p *MeshPort) deliver(in Inbound) {
	p.mu.Lock()
	if p.up {
		p.queue = append(p.queue, in)
	}
	p.mu.Unlock()
}

// Send delivers to one direct neighbour; a participant that is not a neighbour is
// unreachable in one hop, which is what a relay exists to solve.
func (p *MeshPort) Send(peerID uint32, msgType uint8, payload []byte) bool {
	p.mu.Lock()
	peer, ok := p.links[PeerID(peerID)]
	up := p.up
	p.mu.Unlock()
	if !up || !ok {
		return false
	}
	return p.deliverTo(peer, msgType, payload)
}

// Broadcast sends to every direct neighbour.
func (p *MeshPort) Broadcast(msgType uint8, payload []byte) { p.BroadcastExcept(0, msgType, payload) }

// BroadcastExcept sends to every direct neighbour but one. Relaying an artifact back
// down the link it arrived on is pure duplicate traffic: the sender already holds it.
func (p *MeshPort) BroadcastExcept(exclude uint32, msgType uint8, payload []byte) {
	p.mu.Lock()
	up := p.up
	targets := make([]*MeshPort, 0, len(p.links))
	for id, peer := range p.links {
		if uint32(id) != exclude {
			targets = append(targets, peer)
		}
	}
	p.mu.Unlock()
	if !up {
		return
	}
	for _, peer := range targets {
		p.deliverTo(peer, msgType, payload)
	}
}

// deliverTo copies the payload: the caller's buffer is reused between ticks.
func (p *MeshPort) deliverTo(peer *MeshPort, msgType uint8, payload []byte) bool {
	if !peer.IsRunning() {
		return false
	}
	body := append([]byte(nil), payload...)
	peer.deliver(Inbound{
		Kind: InboundMessage,
		Peer: p.local,
		Msg:  NewMessage(MessageType(msgType), body),
	})
	return true
}

// Inject replays a frame into this node's inbound queue as if a neighbour had sent
// it. It is the mesh's half of the join gate: a participant that read session
// traffic off its stream before the port owned it hands the frames over here.
func (p *MeshPort) Inject(peer uint32, msgType uint8, payload []byte) {
	p.deliver(Inbound{
		Kind: InboundMessage,
		Peer: PeerID(peer),
		Msg:  NewMessage(MessageType(msgType), append([]byte(nil), payload...)),
	})
}

// PeerCount reports directly linked participants; D-14 reads it to latch the map.
func (p *MeshPort) PeerCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.up {
		return 0
	}
	return len(p.links)
}

// IsRunning reports node state
func (p *MeshPort) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.up
}

// Drain moves this node's pending notifications into dst
func (p *MeshPort) Drain(dst []Inbound) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := min(len(dst), len(p.queue))
	copy(dst, p.queue[:n])
	p.queue = append(p.queue[:0], p.queue[n:]...)
	return n
}

// Close drops this node from the graph. Both ends of every severed link observe a
// disconnect naming the participant they lost, so each runs the same departure path
// against the same identity. Closing one node leaves the rest of the graph running:
// a mesh outlives any single participant.
func (p *MeshPort) Close() error {
	p.mu.Lock()
	if !p.up {
		p.mu.Unlock()
		return nil
	}
	p.up = false
	links := make([]*MeshPort, 0, len(p.links))
	for id, peer := range p.links {
		links = append(links, peer)
		p.queue = append(p.queue, Inbound{Kind: InboundDisconnect, Peer: id})
	}
	clear(p.links)
	p.mu.Unlock()

	for _, peer := range links {
		peer.mu.Lock()
		delete(peer.links, p.local)
		peer.mu.Unlock()
		peer.deliver(Inbound{Kind: InboundDisconnect, Peer: p.local})
	}
	return nil
}
