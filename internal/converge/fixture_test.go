// The world these criteria drive the protocol against, and the captures it holds.

package converge

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// stub is the Instance a criterion drives: a world that answers with the capture
// it was handed and records what the protocol wrote to it. It exists so the
// protocol can be asserted without a composition root — a real one would bring a
// scheduler, an FSM and a corpus to a question about page hashes.
type stub struct {
	mu     sync.Mutex
	stamp  event.Stamp
	local  uint32
	roster []network.RosterEntry
	port   engine.NetworkPort
	world  snapshot.SharedCapture

	installed []snapshot.SharedCapture
	adopted   []snapshot.CaptureHeader
	handoffs  []adopted
	abandoned [][]network.RosterEntry
	said      []string

	// The playout lead: what the barrier defers by, what the links ask for, and
	// what the authority did about it.
	leadCurrent uint64
	leadTarget  uint64
	leadOverrun []network.PeerID
	leads       []uint64
	dropped     []uint32
}

// adopted is one call the succession made on the run: the record it moved to, and
// whether this instance is the one that now authors.
type adopted struct {
	rec  network.HandoffRecord
	mine bool
}

func (s *stub) Position() event.Stamp {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stamp
}

// Driven is always true: a criterion paces its own publications, and a pump beside
// it would be a second thing deciding when a correction leaves.
func (s *stub) Driven() bool { return true }

func (s *stub) LocalParticipant() uint32 { return s.local }

func (s *stub) RosterSize() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.roster)
}

func (s *stub) WorldRoster() []network.RosterEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]network.RosterEntry(nil), s.roster...)
}

func (s *stub) Transport() engine.NetworkPort { return s.port }

func (s *stub) DrainOffTick() {}

func (s *stub) CaptureShared() (snapshot.SharedCapture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.world, nil
}

func (s *stub) InstallCapture(cap snapshot.SharedCapture) (engine.WorldDifference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installed = append(s.installed, cap)
	s.world = cap
	// Forward only, as App projects a capture behind the clock to the present.
	if cap.Header.Tick > s.stamp.Tick {
		s.stamp.Tick = cap.Header.Tick
	}
	return engine.WorldDifference{Entries: 1, Entities: 1}, nil
}

func (s *stub) VerifyCaptureIdentity(snapshot.CaptureHeader) error { return nil }

// AdoptAuthority records a header this instance's world already equals.
func (s *stub) AdoptAuthority(h snapshot.CaptureHeader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adopted = append(s.adopted, h)
}

// PlayoutLead answers what the criterion staged; SetPlayoutLead and
// DropParticipant record what the authority decided from it.
func (s *stub) PlayoutLead([]network.RosterEntry) (uint64, uint64, []network.PeerID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leadCurrent, s.leadTarget, s.leadOverrun
}

func (s *stub) SetPlayoutLead(ticks uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leads = append(s.leads, ticks)
}

func (s *stub) DropParticipant(id uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := !slices.Contains(s.dropped, id)
	s.dropped = append(s.dropped, id)
	return first
}

func (s *stub) AuthorityChanged(rec network.HandoffRecord, mine bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handoffs = append(s.handoffs, adopted{rec: rec, mine: mine})
}

func (s *stub) DropAbandonedCursors(roster []network.RosterEntry, _ network.PeerID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abandoned = append(s.abandoned, roster)
}

func (s *stub) SetStatusMessage(msg string, _ time.Duration, _ bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.said = append(s.said, msg)
}

// setWorld replaces what the next capture reads, which is how a criterion makes
// two instances disagree.
func (s *stub) setWorld(cap snapshot.SharedCapture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.world = cap
	s.stamp.Tick = cap.Header.Tick
}

// advance moves the tick the schedule and every containment rule read.
func (s *stub) advance(ticks uint64) {
	s.mu.Lock()
	s.stamp.Tick += ticks
	tick := s.stamp.Tick
	world := s.world
	s.mu.Unlock()
	world.Header.Tick = tick
	s.setWorld(seal(world))
}

func (s *stub) installs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.installed)
}

// run is one instance of the protocol over a stub world.
type run struct {
	world *stub
	c     *Corrections
	u     *Authority
	r     *Reach
	reg   *status.Registry

	// chunks reassembles a correction the way NetworkSystem does on a run that has
	// one: a capture is the one message whose size is a function of the world, so
	// it is the one that arrives in pieces.
	chunks network.SnapshotAssembly
}

// newRun builds one participant: a world at tick 1 holding a capture nobody has
// diverged from yet, and the three halves of the protocol over it.
func newRun(t *testing.T, local uint32, port engine.NetworkPort, roster []network.RosterEntry) *run {
	t.Helper()
	reg := status.NewRegistry()
	w := &stub{
		stamp:  event.Stamp{Run: 1, Tick: 1},
		local:  local,
		roster: roster,
		port:   port,
		world:  capture(1, 0),
	}
	c, u, r := New(w, snapshot.NewTelemetry(reg), reg)
	t.Cleanup(c.Close)
	return &run{world: w, c: c, u: u, r: r, reg: reg}
}

// open puts this run in a session under the first term, with the given coordinator.
func (r *run) open(host network.PeerID, chain network.SuccessionChain, fixed bool) {
	r.u.Open(network.SessionOffer{
		Host: host, Assigned: network.PeerID(r.world.local), Term: network.FirstTerm,
		Roster: r.world.WorldRoster(), Chain: chain, FixedAuthority: fixed,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}, network.PeerID(r.world.local))
}

func (r *run) stat(key string) int64 { return r.reg.Ints.Get(key).Load() }

// roster is the closed membership every participant in these criteria holds.
func roster(n int) []network.RosterEntry {
	out := make([]network.RosterEntry, 0, n)
	for i := range n {
		out = append(out, network.RosterEntry{ID: network.PeerID(i + 1), Slot: uint8(i)})
	}
	return out
}

// captureRows is how many placements the fixture world holds. It is several
// manifest pages' worth on purpose: a one-page repair has to be cheaper than the
// whole world, or every repair reaches the keyframe fallback and no criterion
// below is about what it says it is.
const captureRows = parameter.SnapshotManifestPageRows * 8

// capture is a shared world at one tick. spread moves one placement, which is the
// one thing these criteria need two worlds to disagree about: a page hash covers
// the value, so one differing row is a differing page, section and root.
func capture(tick uint64, spread int) snapshot.SharedCapture {
	cap := snapshot.SharedCapture{
		Header: snapshot.CaptureHeader{
			Schema: snapshot.Schema, Run: 1, Tick: tick, Seed: 0x5EEDBEEF, Session: 1,
			Term: network.FirstTerm, Authority: 1, TickInterval: 50 * time.Millisecond,
			MapWidth: 120, MapHeight: 40,
		},
		World: engine.SharedWorldState{NextEntity: uint64(captureRows) + 1},
	}
	for i := range captureRows {
		x := i % 100
		if i == 0 {
			x += spread
		}
		cap.World.Positions = append(cap.World.Positions,
			engine.StoreEntry[component.PositionComponent]{
				Entity: core.Entity(i + 1),
				Value:  component.PositionComponent{X: x, Y: i % 40},
			})
	}
	return seal(cap)
}

// seal stamps the integrity hash a receiver re-checks, which is what makes a
// synthesized capture installable rather than merely well-formed.
func seal(cap snapshot.SharedCapture) snapshot.SharedCapture {
	cap.Header.Integrity = 0
	h, err := snapshot.Integrity(cap)
	if err != nil {
		panic("fixture capture: " + err.Error())
	}
	cap.Header.Integrity = h
	return cap
}

// session links n participants into a mesh and returns their runs, every one
// already in the first term under coordinator 1.
func session(t *testing.T, n int, links [][2]int) []*run {
	t.Helper()
	mesh := network.NewMesh()
	for _, l := range links {
		mesh.Link(network.PeerID(l[0]), network.PeerID(l[1]))
	}
	runs := make([]*run, n)
	for i := range runs {
		runs[i] = newRun(t, uint32(i+1), mesh.Node(network.PeerID(i+1)), roster(n))
		runs[i].open(1, nil, false)
	}
	return runs
}

// deliver drains every node once and hands what arrived to the protocol, which is
// what NetworkSystem does inside a tick on a run that has one. Repeating it is how
// a criterion lets an exchange complete: a manifest out, a request back, a repair
// out again is three rounds.
func deliver(runs []*run, rounds int) {
	buf := make([]network.Inbound, 64)
	for range rounds {
		for _, r := range runs {
			port, ok := r.world.port.(*network.MeshPort)
			if !ok {
				continue
			}
			for {
				n := port.Drain(buf)
				for _, in := range buf[:n] {
					if in.Kind != network.InboundMessage || in.Msg == nil {
						continue
					}
					route(r, in)
				}
				if n < len(buf) {
					break
				}
			}
		}
		for _, r := range runs {
			r.c.Apply()
		}
	}
}

// route is the seam NetworkResource binds: every frame the protocol owns, taken as
// bytes and decided on between two ticks.
func route(r *run, in network.Inbound) {
	switch in.Msg.Type {
	case network.MsgStateCorrection:
		if done, err := r.chunks.Add(in.Msg.Payload); err == nil && done {
			_, body := r.chunks.Result()
			r.chunks = network.SnapshotAssembly{}
			r.c.Receive(body)
		}
	case network.MsgStateManifest, network.MsgStateRequest,
		network.MsgStateShard, network.MsgStateUnserved:
		r.c.ReceiveSelective(uint8(in.Msg.Type), uint32(in.Peer), in.Msg.Payload)
	case network.MsgAuthorityReport, network.MsgAuthorityHandoff, network.MsgPeerList:
		r.c.ReceiveAuthorityFrame(uint8(in.Msg.Type), uint32(in.Peer), in.Msg.Payload)
	}
}
