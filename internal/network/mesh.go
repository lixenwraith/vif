package network

import (
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// meshEpoch is the origin of the mesh's virtual clock. A mesh has no wall time
// of its own — its links are function calls — so a round trip measured on one
// would come back as a few microseconds whatever the harness had shaped. The
// clock advances one game tick per Drain instead, which makes an in-process link
// measurable and, more to the point, makes a shaped one *reproducible*: the same
// ticks produce the same round trip on every run and on every machine.
var meshEpoch = time.Unix(1<<30, 0)

// meshProbeDrains is the probe interval in drains, which is the mesh's tick.
const meshProbeDrains = uint64(parameter.NetworkProbeInterval / parameter.GameUpdateInterval)

// LinkShape is a deterministic bottleneck between two mesh nodes: a one-way
// delay in ticks, a periodic drop, and a release budget in bytes per tick.
//
// It is not a network simulator and does not try to be. What it reproduces is
// the three things `tc netem` shapes and the correction path actually depends
// on — when a frame becomes visible, whether it arrives at all, and how many
// bytes per tick the link will pass — with no clock and no scheduler, so a
// constrained-link test is as reproducible as any other. The staged netem gate
// is what says the same code behaves the same way over a real socket.
//
// The shape applies to what a node *receives*, which mirrors shaping a
// participant's own ingress. A symmetric link is both ends shaped alike.
type LinkShape struct {
	// LatencyTicks delays a frame by this many drains before it becomes visible.
	LatencyTicks uint64

	// LossEvery drops one inbound frame in N; zero drops none. Deterministic
	// rather than random, so a failing case is a case rather than a seed.
	LossEvery int

	// BytesPerTick is the release budget, spent as a token bucket rather than
	// reset each tick. That distinction is the whole of whether the shape is a
	// bottleneck at all: a budget that resets lets one frame of any size through
	// every tick, so a link declared at 900 bytes a tick would happily pass a
	// 64 KiB chunk twenty times a second. Accumulating instead makes a frame wait
	// until the link has actually earned it, and the ones behind it wait with it.
	BytesPerTick int
}

// burstBytes bounds how much unspent budget a shaped link may accumulate while
// it is idle. It is at least one maximum frame, or a shape could hold a frame it
// can never afford; beyond that it is a few ticks' worth, so a quiet link does
// not save up an unbounded burst to spend at once.
func (s LinkShape) burstBytes() int {
	return max(s.BytesPerTick*4, MaxPayloadSize+HeaderSize)
}

// meshFrame is one queued notification and when the shape lets it through.
type meshFrame struct {
	in        Inbound
	releaseAt uint64
	bytes     int
}

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
	p := &MeshPort{
		local:    id,
		links:    make(map[PeerID]*MeshPort),
		up:       true,
		meters:   make(map[PeerID]*linkMeter),
		inBytes:  make(map[PeerID]uint64),
		outBytes: make(map[PeerID]uint64),
	}
	m.nodes[id] = p
	return p
}

// Shape installs the same bottleneck on both ends of a link, which is the
// symmetric case a round trip is measured across.
func (m *Mesh) Shape(a, b PeerID, s LinkShape) {
	m.mu.Lock()
	x, y := m.nodeLocked(a), m.nodeLocked(b)
	m.mu.Unlock()
	x.SetShape(s)
	y.SetShape(s)
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
	queue []meshFrame
	up    bool

	// The virtual clock and the link measurement that reads it. drains is the
	// clock: one game tick per Drain, which is what a tick loop calls once.
	drains   uint64
	shape    LinkShape
	credit   int
	arrivals int
	probeSeq uint32
	meters   map[PeerID]*linkMeter
	report   LinkReport
	inBytes  map[PeerID]uint64
	outBytes map[PeerID]uint64
}

// SetShape installs a bottleneck on what this node receives.
func (p *MeshPort) SetShape(s LinkShape) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shape, p.credit = s, s.BytesPerTick
}

// now is this node's virtual clock: the tick it has drained, as an instant.
// Caller MUST hold mu.
func (p *MeshPort) nowLocked() time.Time {
	return meshEpoch.Add(time.Duration(p.drains) * parameter.GameUpdateInterval)
}

// SetLinkReport publishes what this node tells a probing peer about its picture.
func (p *MeshPort) SetLinkReport(r LinkReport) {
	p.mu.Lock()
	p.report = r
	p.mu.Unlock()
}

// LinkMetric returns one link's estimate; the zero value is an unmeasured link.
func (p *MeshPort) LinkMetric(peer uint32) linkpace.Metrics {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := p.meters[PeerID(peer)]; ok {
		return m.link.Metrics()
	}
	return linkpace.Metrics{}
}

// ObserveTransfer folds a completed bulk transfer into one link's estimate, the
// same measurement a join makes over a socket.
func (p *MeshPort) ObserveTransfer(peer uint32, bytes int64, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.meterLocked(PeerID(peer)).link.ObserveTransfer(bytes, elapsed)
}

// Peers returns the directly linked participants in a stable order.
func (p *MeshPort) Peers() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.up {
		return nil
	}
	out := make([]uint32, 0, len(p.links))
	for id := range p.links {
		out = append(out, uint32(id))
	}
	slices.Sort(out)
	return out
}

// meterLocked returns one peer's measurement state. Caller MUST hold mu.
func (p *MeshPort) meterLocked(id PeerID) *linkMeter {
	m, ok := p.meters[id]
	if !ok {
		m = newLinkMeter()
		p.meters[id] = m
	}
	return m
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

// deliver appends to this node's inbound queue, through whatever shape is
// installed: a periodic drop, and a delay before the frame becomes visible.
func (p *MeshPort) deliver(in Inbound) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.up {
		return
	}
	bytes := 0
	if in.Msg != nil {
		bytes = HeaderSize + len(in.Msg.Payload)
	}
	if in.Kind == InboundMessage && p.shape.LossEvery > 0 {
		p.arrivals++
		if p.arrivals%p.shape.LossEvery == 0 {
			return // the frame the shape swallowed; nothing repairs one
		}
	}
	p.queue = append(p.queue, meshFrame{
		in:        in,
		releaseAt: p.drains + p.shape.LatencyTicks,
		bytes:     bytes,
	})
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
	p.mu.Lock()
	p.outBytes[peer.local] += uint64(HeaderSize + len(payload))
	p.mu.Unlock()

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

// Drain moves this node's pending notifications into dst, and is the mesh's
// tick: it advances the virtual clock, emits the interval's probes, and releases
// whatever the shape now allows through.
//
// A round trip and an echo never reach dst. They are answered and folded in
// here, which is the same place a socket port answers them — before a frame
// could reach a tick — so the measurement is a property of the link on both
// transports rather than of how the harness happens to drive one.
func (p *MeshPort) Drain(dst []Inbound) int {
	p.mu.Lock()
	p.drains++
	if p.shape.BytesPerTick > 0 {
		p.credit = min(p.credit+p.shape.BytesPerTick, p.shape.burstBytes())
	}
	probes := p.probesDueLocked()
	p.mu.Unlock()

	for _, id := range probes {
		p.mu.Lock()
		peer, ok := p.links[id]
		payload := encodeProbe(p.probeSeq, p.nowLocked())
		p.mu.Unlock()
		if ok {
			p.deliverTo(peer, uint8(MsgLinkProbe), payload)
		}
	}

	n := 0
	for n < len(dst) {
		frame, ok := p.release()
		if !ok {
			break
		}
		if frame.in.Kind == InboundMessage && p.answerLocally(frame.in) {
			continue
		}
		dst[n] = frame.in
		n++
	}
	return n
}

// probesDueLocked advances the probe sequence when this drain is a probe
// interval and returns the peers to measure. Caller MUST hold mu.
func (p *MeshPort) probesDueLocked() []PeerID {
	if !p.up || len(p.links) == 0 || meshProbeDrains == 0 || p.drains%meshProbeDrains != 0 {
		return nil
	}
	p.probeSeq++
	out := make([]PeerID, 0, len(p.links))
	for id := range p.links {
		out = append(out, id)
		p.meterLocked(id).nextProbe()
	}
	slices.Sort(out)
	return out
}

// release pops the next frame the shape allows through, counting its bytes
// against this link's received total at the moment it actually arrives.
//
// The budget is head-of-line: a frame the remaining credit cannot carry holds up
// the ones behind it rather than being skipped. That is what a bottleneck does,
// and it is what makes the backlog the far end measures a real one.
func (p *MeshPort) release() (meshFrame, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return meshFrame{}, false
	}
	frame := p.queue[0]
	if frame.releaseAt > p.drains {
		return meshFrame{}, false
	}
	if p.shape.BytesPerTick > 0 {
		if p.credit < frame.bytes {
			return meshFrame{}, false
		}
		p.credit -= frame.bytes
	}
	p.queue = append(p.queue[:0], p.queue[1:]...)
	p.inBytes[frame.in.Peer] += uint64(frame.bytes)
	return frame, true
}

// answerLocally handles the two frames the game never sees, reporting whether
// this was one of them.
func (p *MeshPort) answerLocally(in Inbound) bool {
	if in.Msg == nil {
		return false
	}
	switch in.Msg.Type {
	case MsgLinkProbe:
		p.mu.Lock()
		peer, ok := p.links[in.Peer]
		echo := encodeEcho(in.Msg.Payload, p.inBytes[in.Peer], p.report)
		p.mu.Unlock()
		if ok && echo != nil {
			p.deliverTo(peer, uint8(MsgLinkEcho), echo)
		}
		return true
	case MsgLinkEcho:
		seq, sent, delivered, report, ok := decodeEcho(in.Msg.Payload)
		if !ok {
			return true
		}
		p.mu.Lock()
		p.meterLocked(in.Peer).observe(p.nowLocked(), sent, seq, delivered, p.outBytes[in.Peer], report)
		p.mu.Unlock()
		return true
	}
	return false
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
		p.queue = append(p.queue, meshFrame{in: Inbound{Kind: InboundDisconnect, Peer: id}})
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
