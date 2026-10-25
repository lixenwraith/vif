// The world these criteria drive the protocol against, and the captures it holds.

package converge

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/status"
	link "github.com/lixenwraith/vif/pkg/network"
)

// stub is the Instance a criterion drives: a world that answers with the capture
// it was handed and records what the protocol wrote to it. It exists so the
// protocol can be asserted without a composition root — a real one would bring a
// scheduler, an FSM and a corpus to a question about page hashes.
type stub struct {
	mu     sync.Mutex
	stamp  event.Stamp
	frozen bool
	held   bool
	local  uint32
	roster []network.RosterEntry
	port   engine.NetworkPort
	world  snapshot.SharedCapture
	enc    snapshot.Encoder

	installed []snapshot.SharedCapture
	adopted   []snapshot.CaptureHeader
	handoffs  []adopted
	departed  [][]network.RosterEntry
	returned  [][]network.RosterEntry
	forked    int
	forkedAs  []network.RosterEntry
	thawed    int
	said      []string

	// What a walk asks of the run: whether it redials the authority it lost and
	// which, whether it relays for others and whom it marked behind it, and whether
	// it has lost every path to its authority.
	redial   bool
	redials  []link.PeerID
	relaying bool
	carried  []uint32
	cut      bool

	// What the eviction policy reads, and what it decided.
	late    map[uint32]uint64
	ingress map[uint32]uint64
	dropped []uint32

	// How the world was read as ticks closed: captured whole, or indexed in place.
	captured, indexed int
}

// adopted is one call the succession made on the run: the record it moved to,
// whether this instance is the one that now authors, and the link it follows that
// one through.
type adopted struct {
	rec  network.HandoffRecord
	mine bool
	via  link.PeerID
}

func (s *stub) Position() event.Stamp {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stamp
}

// Driven is always true: a criterion paces its own publications, and a pump beside
// it would be a second thing deciding when a correction leaves.
func (s *stub) Driven() bool { return true }

func (s *stub) Held() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held
}

func (s *stub) setHeld(held bool) {
	s.mu.Lock()
	s.held = held
	s.mu.Unlock()
}

func (s *stub) LocalParticipant() uint32 { return s.local }

func (s *stub) WorldRoster() []network.RosterEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]network.RosterEntry(nil), s.roster...)
}

func (s *stub) Transport() engine.NetworkPort { return s.port }

// Downstream is the links this stub serves, which in a tree built from the host
// outward is everyone behind it.
func (s *stub) Downstream() []uint32 {
	if port, ok := s.port.(*link.MeshPort); ok {
		return port.Served()
	}
	return nil
}

func (s *stub) DrainOffTick() {}

// Reaches is true unless a criterion cuts this instance off.
func (s *stub) Reaches() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.cut
}

// Carries is a link served here, or any once relaying is on.
func (s *stub) Carries(id uint32) bool {
	s.mu.Lock()
	relaying := s.relaying
	s.mu.Unlock()
	return relaying || slices.Contains(s.Downstream(), id)
}

func (s *stub) Carry(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.carried = append(s.carried, id)
}

func (s *stub) Redial(lost link.PeerID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redials = append(s.redials, lost)
	return s.redial
}

func (s *stub) CaptureShared() (snapshot.SharedCapture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.world, nil
}

func (s *stub) CaptureSharedLocked() (snapshot.SharedCapture, error) {
	s.captured++
	return s.world, nil
}

// IndexSharedLocked indexes the stub's capture as a live world would be, counting
// the answers that needed no capture.
func (s *stub) IndexSharedLocked(authority uint32, term network.AuthorityTerm) (*snapshot.Manifest, snapshot.SharedCapture, error) {
	s.indexed++
	light := s.world
	light.World = engine.SharedWorldState{NextEntity: light.World.NextEntity, Created: light.World.Created, Destroyed: light.World.Destroyed}
	light.Header.Term = term
	whole := s.world
	whole.Header.Term = term
	index, err := s.enc.Build(whole, authority)
	return index, light, err
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

func (s *stub) Encoder() *snapshot.Encoder { return &s.enc }

// AdoptAuthority records a header this instance's world already equals.
func (s *stub) AdoptAuthority(h snapshot.CaptureHeader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adopted = append(s.adopted, h)
}

func (s *stub) CommitLate(id uint32) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.late[id]
}

func (s *stub) Ingress(id uint32) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ingress[id]
}

func (s *stub) DropParticipant(id uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := !slices.Contains(s.dropped, id)
	s.dropped = append(s.dropped, id)
	return first
}

func (s *stub) AuthorityChanged(rec network.HandoffRecord, mine bool, via link.PeerID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handoffs = append(s.handoffs, adopted{rec: rec, mine: mine, via: via})
}

func (s *stub) CrossDepartures(away []network.RosterEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.departed = append(s.departed, away)
	s.markAwayLocked(away, true)
}

func (s *stub) CrossReturns(back []network.RosterEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.returned = append(s.returned, back)
	s.markAwayLocked(back, false)
}

// markAwayLocked marks each place as a departure, or a return, leaves it on the run.
func (s *stub) markAwayLocked(who []network.RosterEntry, away bool) {
	for i := range s.roster {
		if slices.ContainsFunc(who, func(p network.RosterEntry) bool { return p.ID == s.roster[i].ID }) {
			s.roster[i].Away = away
		}
	}
}

func (s *stub) Forked(roster []network.RosterEntry, _ link.PeerID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forked++
	s.forkedAs = roster
	s.frozen = false
}

func (s *stub) Freeze() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frozen = true
}

func (s *stub) Thaw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thawed++
	s.frozen = false
}

func (s *stub) SetStatusMessage(msg string, _ parameter.StatusKind) {
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

// installedTick is the tick of the last world installed, zero before any.
func (s *stub) installedTick() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.installed) == 0 {
		return 0
	}
	return s.installed[len(s.installed)-1].Header.Tick
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
	chunks link.SnapshotAssembly
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
func (r *run) open(host link.PeerID, chain network.SuccessionChain, fixed bool) {
	r.u.Open(network.SessionOffer{
		Host: host, Assigned: link.PeerID(r.world.local), Term: network.FirstTerm,
		Roster: r.world.WorldRoster(), Chain: chain, FixedAuthority: fixed,
	}, link.PeerID(r.world.local))
}

func (r *run) stat(key string) int64 { return r.reg.Ints.Get(key).Load() }

// roster is the closed membership every participant in these criteria holds.
func roster(n int) []network.RosterEntry {
	out := make([]network.RosterEntry, 0, n)
	for i := range n {
		out = append(out, network.RosterEntry{ID: link.PeerID(i + 1), Slot: uint8(i)})
	}
	return out
}

// captureRows is how many placements the fixture world holds. It is many manifest
// pages' worth on purpose: a one-page repair has to be cheaper than the whole
// world, or every repair reaches the keyframe fallback and no criterion below is
// about what it says it is; a page's rows deflate to twenty bytes each as JSON, a
// world's to under two in columns.
const captureRows = parameter.SnapshotManifestPageRows * 64

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
	// Placements scattered rather than cycled: a cycle's columns deflate to less
	// than one page of its rows, and a world cheaper than a page is sent whole.
	for i := range captureRows {
		h := uint32(i+1) * 2654435761
		x := int(h>>24) % 100
		if i == 0 {
			x += spread
		}
		cap.World.Positions = append(cap.World.Positions,
			engine.StoreEntry[component.PositionComponent]{
				Entity: core.Entity(i + 1),
				Value:  component.PositionComponent{X: x, Y: int(h>>16) % 40},
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
// already in the first term under coordinator 1, every guest on the chain in
// identity order, as guests that each declared a port are.
func session(t *testing.T, n int, links [][2]int) []*run {
	t.Helper()
	mesh := network.NewMesh()
	for _, l := range links {
		mesh.Link(link.PeerID(l[0]), link.PeerID(l[1]))
	}
	var chain network.SuccessionChain
	for id := 2; id <= n; id++ {
		chain = append(chain, network.ChainEntry{ID: link.PeerID(id), Addr: "mesh"})
	}
	runs := make([]*run, n)
	for i := range runs {
		runs[i] = newRun(t, uint32(i+1), mesh.Node(link.PeerID(i+1)), roster(n))
		runs[i].open(1, chain, false)
	}
	return runs
}

// deliver drains every node once and hands what arrived to the protocol, which is
// what NetworkSystem does inside a tick on a run that has one. Repeating it is how
// a criterion lets an exchange complete: a manifest out, a request back, a repair
// out again is three rounds.
func deliver(runs []*run, rounds int) {
	buf := make([]link.Inbound, 64)
	for range rounds {
		for _, r := range runs {
			port, ok := r.world.port.(interface{ Drain([]link.Inbound) int })
			if !ok {
				continue
			}
			for {
				n := port.Drain(buf)
				for _, in := range buf[:n] {
					if in.Kind != link.InboundMessage || in.Msg == nil {
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
func route(r *run, in link.Inbound) {
	switch in.Msg.Type {
	case network.MsgStateCorrection:
		if done, err := r.chunks.Add(in.Msg.Payload); err == nil && done {
			_, body := r.chunks.Result()
			r.chunks = link.SnapshotAssembly{}
			r.c.Receive(body)
		}
	case network.MsgStateManifest, network.MsgStateRequest,
		network.MsgStateShard, network.MsgStateUnserved:
		r.c.ReceiveSelective(uint8(in.Msg.Type), uint32(in.Peer), in.Msg.Payload)
	case network.MsgAuthorityReport, network.MsgAuthorityHandoff, network.MsgPeerList:
		r.c.ReceiveAuthorityFrame(uint8(in.Msg.Type), uint32(in.Peer), in.Msg.Payload)
	}
}
