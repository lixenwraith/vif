package system

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// NetworkSystem is the only crossing between this instance and its peers, in both
// directions. Outbound it is the event queue's wire sink (D-3 artifacts, D-10
// classes) plus a periodic owner-authored state sync (D-13). Inbound it replays a
// peer's crossings in the domain their producer stamped, and it is the sole writer
// of a remote cursor's owner-authored components — which is what makes a
// transported value the single authority for that cell.
type NetworkSystem struct {
	world *engine.World

	// Barrier state is written from the lock-free push path and drained under the tick.
	mu              sync.Mutex
	crossings       []event.ScheduledWireFrame
	scheduled       []barrierArtifact
	epochs          [parameter.MaxPlayers + 1]epochWindow
	productionEpoch uint64
	crossSeq        uint64
	localSource     uint32
	delayTicks      uint64
	encodeErr       int64
	barrierActive   atomic.Bool

	// snapshotFloor is the tick of the last world this instance installed. An
	// installed world has already applied everything due at or before its own tick,
	// so an artifact arriving for one of those ticks is not a late crossing to
	// apply — it is a crossing the capture already contains, and applying it again
	// would double it. Zero on an instance that never installed one.
	snapshotFloor uint64

	buf [parameter.NetworkDrainWindow]network.Inbound // per-tick drain window

	syncSeq   uint64
	lastSync  [parameter.MaxPlayers]uint64   // last applied sync per slot, for reordering
	departed  [parameter.MaxPlayers + 1]bool // participants already announced or noticed
	ticks     uint64
	statSent  *atomic.Int64
	statRecv  *atomic.Int64
	statState *atomic.Int64
	statDrop  *atomic.Int64

	statDeferred       *atomic.Int64
	statAppliedLocal   *atomic.Int64
	statAppliedPeer    *atomic.Int64
	statLate           *atomic.Int64
	statRanWithout     *atomic.Int64
	statPeerLag        *atomic.Int64
	statPeerArtifacts  *atomic.Int64
	statPeerApplied    *atomic.Bool
	statPeers          *atomic.Int64
	statConnected      *atomic.Bool
	statConnection     *status.AtomicString
	statMapLatched     *atomic.Bool
	statLostIn         *atomic.Int64
	statLostOut        *atomic.Int64
	statRelayed        *atomic.Int64
	statDuplicates     *atomic.Int64
	statDigestMismatch *atomic.Int64

	digestHistory   [parameter.NetworkEpochWindow]stateDigest
	pendingDigest   [parameter.MaxPlayers + 1]stateDigest
	statDriftPart   *status.AtomicString
	statDriftTick   *atomic.Int64
	statPreInstall  *atomic.Int64
	statJoinLag     *atomic.Int64
	statLag         *atomic.Int64
	statStale       *atomic.Bool
	statLocalNow    *atomic.Int64
	statCorrections *atomic.Int64
	statForged      *atomic.Int64

	// The link measurement, published for the worst peer this instance has. It is
	// the transport's own estimate rather than anything this system derives: what
	// happens here is a read and a store, so that a network round trip never
	// becomes a value a tick could branch on.
	statRTT       *atomic.Int64
	statRTTMicros *atomic.Int64
	statJitter    *atomic.Int64
	statLinkBps   *atomic.Int64
	statLinkLoss  *atomic.Int64
	statSaturated *atomic.Bool

	// statMagnitude is App's correction magnitude, read rather than owned: it is
	// what this instance reports to whoever is publishing to it, and it is the
	// only reason the report says anything about the world at all.
	statMagnitude *atomic.Int64

	// snapshots reassembles one authoritative correction per peer. A correction is
	// the only message whose size is a function of the world, so it is the only one
	// that arrives in pieces, and the pieces of two peers' corrections must not be
	// able to interleave into a body that hashes as neither.
	snapshots [parameter.MaxPlayers + 1]network.SnapshotAssembly

	// Last reported transport loss, so a new one is logged once rather than per tick.
	lastLostIn  uint64
	lastLostOut uint64

	// Every counter this system publishes, collected as it is registered. Init
	// clears the session by ranging these rather than by restating each name, so a
	// counter added to the constructor cannot be left carrying the previous run.
	resetInts    []*atomic.Int64
	resetBools   []*atomic.Bool
	resetStrings []*status.AtomicString

	enabled bool
}

// intStat, boolStat and textStat register one counter and enrol it in the set Init
// clears, so registration and reset cannot drift apart.
func (s *NetworkSystem) intStat(reg *status.Registry, key string) *atomic.Int64 {
	c := reg.Ints.Get(key)
	s.resetInts = append(s.resetInts, c)
	return c
}

func (s *NetworkSystem) boolStat(reg *status.Registry, key string) *atomic.Bool {
	c := reg.Bools.Get(key)
	s.resetBools = append(s.resetBools, c)
	return c
}

func (s *NetworkSystem) textStat(reg *status.Registry, key string) *status.AtomicString {
	c := reg.Strings.Get(key)
	s.resetStrings = append(s.resetStrings, c)
	return c
}

// stateDigest names one completed shared-world state. Valid distinguishes a real
// run-zero/tick-zero hash from an empty ring slot.
type stateDigest struct {
	Run       uint64 `json:"run"`
	Tick      uint64 `json:"tick"`
	Hash      uint64 `json:"hash"`
	Positions uint64 `json:"positions"`
	Kinetics  uint64 `json:"kinetics"`
	Combat    uint64 `json:"combat"`
	Context   uint64 `json:"context"`
	Status    uint64 `json:"status"`
	Surface   uint64 `json:"surface"`

	Valid bool `json:"-"`
}

// epochWindow is one source's replay filter: the newest production epoch admitted
// from it, plus a bitmap of the epochs immediately before that one.
//
// A single stream delivers a source's epochs in order, and a high-water mark is
// enough for it. A mesh does not: the same epoch reaches a node by several paths,
// and epochs from one source can arrive out of order because the paths differ in
// length. Against a high-water mark an out-of-order epoch is indistinguishable from
// a duplicate, so it would be discarded without ever being applied — a silent
// divergence exactly where the relay is supposed to prevent one.
//
// The window admits each epoch once and no more, in any arrival order, as long as
// it is within NetworkEpochWindow of the newest. Beyond that it is refused: an
// artifact that far behind has already missed its apply tick.
type epochWindow struct {
	high uint64 // newest ProducedTick admitted; zero means nothing yet
	seen uint64 // bit i set: epoch high-1-i was admitted
}

// admit reports whether tick is new to this source and records it. Zero is not a
// valid production epoch — the first tick a session can close is one — so it is
// refused rather than treated as "nothing seen yet".
func (w *epochWindow) admit(tick uint64) bool {
	switch {
	case tick == 0:
		return false

	case w.high == 0:
		w.high = tick
		return true

	case tick > w.high:
		// The old high joins the backlog, shifted by the gap it now sits behind.
		if shift := tick - w.high; shift < parameter.NetworkEpochWindow {
			w.seen = (w.seen << shift) | (1 << (shift - 1))
		} else {
			w.seen = 0
		}
		w.high = tick
		return true

	case tick == w.high:
		return false

	default:
		behind := w.high - tick
		if behind > parameter.NetworkEpochWindow {
			return false
		}
		bit := uint64(1) << (behind - 1)
		if w.seen&bit != 0 {
			return false
		}
		w.seen |= bit
		return true
	}
}

// newest reports the highest epoch admitted from this source, for the playout lag
// telemetry that asks how far behind a peer's production is.
func (w *epochWindow) newest() uint64 { return w.high }

// coordinatorParticipant is the identity the handshake always assigns to the host.
// It is the one participant every topology the session can build has a path to, which
// is what makes it the single producer of a departure crossing.
const coordinatorParticipant uint32 = 1

// barrierArtifact is one encoded local or peer crossing waiting for its apply tick.
type barrierArtifact struct {
	frame     event.WireFrame
	applyTick uint64
	source    uint32
	origin    event.Origin
}

func NewNetworkSystem(world *engine.World) engine.System {
	s := &NetworkSystem{world: world}

	reg := world.Resources.Status
	s.statSent = s.intStat(reg, "network.crossings_sent")
	s.statRecv = s.intStat(reg, "network.crossings_received")
	s.statState = s.intStat(reg, "network.state_applied")
	s.statDrop = s.intStat(reg, "network.frames_dropped")
	s.statDeferred = s.intStat(reg, "network.barrier_deferred")
	s.statAppliedLocal = s.intStat(reg, "network.barrier_applied_local")
	s.statAppliedPeer = s.intStat(reg, "network.barrier_applied_peer")
	s.statLate = s.intStat(reg, "network.barrier_late")
	s.statRanWithout = s.intStat(reg, "network.barrier_ran_without_peer")
	s.statPeerLag = s.intStat(reg, "network.barrier_peer_lag_ticks")
	s.statPeerArtifacts = s.intStat(reg, "network.barrier_peer_artifacts")
	s.statPeerApplied = s.boolStat(reg, "network.barrier_peer_applied")
	s.statPeers = s.intStat(reg, "network.peers")
	s.statConnected = s.boolStat(reg, "network.connected")
	s.statConnection = s.textStat(reg, "network.state")
	s.statMapLatched = s.boolStat(reg, "network.map_latched")
	s.statLostIn = s.intStat(reg, "network.transport_lost_in")
	s.statLostOut = s.intStat(reg, "network.transport_lost_out")
	s.statRelayed = s.intStat(reg, "network.relay_forwarded")
	s.statDuplicates = s.intStat(reg, "network.relay_duplicates")
	s.statDigestMismatch = s.intStat(reg, "network.digest_mismatches")
	s.statDriftPart = s.textStat(reg, "network.drift_part")
	s.statDriftTick = s.intStat(reg, "network.drift_tick")
	s.statPreInstall = s.intStat(reg, "network.artifacts_pre_install")
	s.statJoinLag = s.intStat(reg, "network.join_lag_ticks")
	s.statLag = s.intStat(reg, "network.lag_ticks")
	s.statStale = s.boolStat(reg, "network.stale")
	s.statLocalNow = s.intStat(reg, "network.crossings_local")
	s.statCorrections = s.intStat(reg, "network.corrections_received")
	s.statForged = s.intStat(reg, "network.artifacts_refused")
	s.statRTT = s.intStat(reg, "network.link_rtt_ms")
	// Milliseconds are the unit a person reads and the unit `tc netem delay`
	// speaks, and they round a loopback round trip to zero — which reads as "not
	// measured" rather than as "fast". The microsecond form is the same number at
	// the resolution the estimator actually works in, so a local session can show
	// that the round trip exists at all.
	s.statRTTMicros = s.intStat(reg, "network.link_rtt_us")
	s.statJitter = s.intStat(reg, "network.link_jitter_ms")
	s.statLinkBps = s.intStat(reg, "network.link_bps")
	s.statLinkLoss = s.intStat(reg, "network.link_loss_pct")
	s.statSaturated = s.boolStat(reg, "network.link_saturated")
	// Not registered through intStat: this cell is App's and a session reset must
	// not clear the magnitude of a correction App has already published into it.
	s.statMagnitude = reg.Ints.Get("snapshot.correction_entities")

	s.Init()
	return s
}

// Init resets the barrier and leaves its sink installed as a no-op without peers.
func (s *NetworkSystem) Init() {
	s.enabled = true
	s.ticks = 0
	s.syncSeq = 0
	s.lastSync = [parameter.MaxPlayers]uint64{}
	s.departed = [parameter.MaxPlayers + 1]bool{}

	for _, c := range s.resetInts {
		c.Store(0)
	}
	for _, c := range s.resetBools {
		c.Store(false)
	}
	for _, c := range s.resetStrings {
		c.Store("")
	}
	s.statConnection.Store("off") // the link has a resting value; the counters do not
	s.lastLostIn, s.lastLostOut = 0, 0
	s.barrierActive.Store(false)
	s.digestHistory = [parameter.NetworkEpochWindow]stateDigest{}
	s.pendingDigest = [parameter.MaxPlayers + 1]stateDigest{}
	s.snapshots = [parameter.MaxPlayers + 1]network.SnapshotAssembly{}

	s.mu.Lock()
	s.crossings = s.crossings[:0]
	s.scheduled = s.scheduled[:0]
	s.epochs = [parameter.MaxPlayers + 1]epochWindow{}
	s.snapshotFloor = 0
	s.productionEpoch = s.world.Resources.Game.State.GetGameTicks() + 1
	s.crossSeq = 0
	s.localSource = 0
	s.delayTicks = parameter.NetworkBarrierDelayTicks
	if r := s.world.Resources.Network; r != nil {
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
	}
	s.encodeErr = 0
	s.mu.Unlock()

	s.world.Resources.Event.Queue.SetWireSink(s)
}

// port returns the live endpoint, nil when no transport is attached. Read per tick
// rather than cached at construction: an embedder or a harness may attach one after
// the system set is sealed.
func (s *NetworkSystem) port() engine.NetworkPort {
	if s.world.Resources.Network == nil {
		return nil
	}
	return s.world.Resources.Network.Port
}

// Name returns system's name
func (s *NetworkSystem) Name() string { return "network" }

// Priority: inbound translation runs before every consumer, so a peer's crossing
// is queued in time for the settle of the tick that drained it
func (s *NetworkSystem) Priority() int { return parameter.PriorityNetwork }

func (s *NetworkSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
		event.EventParticipantJoined,
		event.EventParticipantDeparted,
	}
}

func (s *NetworkSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameResetRequest:
		s.Init()
	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
	case event.EventParticipantJoined:
		if p, ok := ev.Payload.(*event.ParticipantJoinedPayload); ok {
			s.addParticipant(p)
		}
	case event.EventParticipantDeparted:
		if p, ok := ev.Payload.(*event.ParticipantDepartedPayload); ok {
			s.removeParticipant(p)
		}
	}
}

// addParticipant applies the arrival crossing. Every instance runs it at the same
// apply tick, so the cursor it creates takes the same shared entity everywhere (D-11)
// — which is the reason a mid-run arrival cannot be a local reaction to a connect.
// The instance the payload names is the one that goes on to simulate it.
func (s *NetworkSystem) addParticipant(p *event.ParticipantJoinedPayload) {
	if int(p.Slot) >= parameter.MaxPlayers || s.world.Resources.Player.Slot(p.Slot) != 0 {
		return
	}
	control := component.ControlRemote
	if p.Participant == s.participantID() {
		control = component.ControlHuman
	}
	heat, energy := s.world.Resources.Player.InitialResources()
	s.world.PushEvent(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{
		Slot: p.Slot, Center: true, Control: uint8(control), PeerID: p.Participant,
		Heat: heat, Energy: energy,
	})
	if control == component.ControlHuman {
		s.world.PushEvent(event.EventCursorSetLocalRequest, &event.CursorSetLocalPayload{Slot: p.Slot})
	}
	s.lastSync[p.Slot] = 0
	if int(p.Participant) < len(s.departed) {
		s.departed[p.Participant] = false
	}
}

// participantID is this instance's canonical identity in the session.
func (s *NetworkSystem) participantID() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localSource
}

// barrierDelayTicks returns the session's negotiated playout lead.
func (s *NetworkSystem) barrierDelayTicks() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delayTicks
}

// removeParticipant applies the departure crossing. It arrives at the same apply
// tick on every instance, so the despawn it derives does too (D-5) — which is the
// whole reason a disconnect is not acted on where it is observed.
func (s *NetworkSystem) removeParticipant(p *event.ParticipantDepartedPayload) {
	if int(p.Slot) >= parameter.MaxPlayers {
		return
	}
	cursor := s.world.Resources.Player.Slot(p.Slot)
	if cursor == 0 || s.world.SimulatesLocally(cursor) {
		return
	}
	s.world.PushEvent(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: p.Slot})
	s.lastSync[p.Slot] = 0
	if int(p.Participant) < len(s.departed) {
		s.departed[p.Participant] = true
	}
}

// Cross encodes and schedules a crossing when a peer is live. Returning true
// transfers queue ownership; pooled originals are released after encoding.
func (s *NetworkSystem) Cross(ev event.GameEvent) bool {
	if !s.barrierActive.Load() {
		return false
	}
	frame, encErr := event.NewWireFrame(ev)
	s.mu.Lock()
	if encErr != "" {
		s.encodeErr++
		s.mu.Unlock()
		event.ReleaseDeferredPayload(ev.Payload)
		return true
	}
	s.crossSeq++
	frame.Seq = s.crossSeq
	applyTick := s.productionEpoch + s.delayTicks
	s.crossings = append(s.crossings, event.ScheduledWireFrame{Frame: frame, ApplyTick: applyTick})
	agreed := barrierBound(ev.Type)
	if agreed {
		s.scheduled = append(s.scheduled, barrierArtifact{
			frame: frame, applyTick: applyTick, source: s.localSource, origin: ev.Origin,
		})
	}
	s.mu.Unlock()
	if agreed {
		event.ReleaseDeferredPayload(ev.Payload)
		s.statDeferred.Add(1)
		return true
	}
	s.statLocalNow.Add(1)

	// Ownership is *not* taken: the queue publishes this artifact now, in the tick
	// that produced it, and what was scheduled above is only the copy the peers get.
	//
	// This is Phase 4's requirement 5 and it is the point where D-3 changes its
	// destination. A crossing used to be a fact every instance applied at one agreed
	// tick, which meant the producer waited for its own action as long as everyone
	// else did — a playout lead's worth of latency charged to the one participant
	// who did not need it. It is a *request to the authority* now: the producer
	// applies it immediately, the host applies it in its own order, and the
	// difference between the two is what the next correction repairs. On the host
	// that difference is nothing, because the host is the authority.
	//
	// The receive side keeps the lead, and keeps it for a different reason: it is
	// an interpolation buffer that lets a remote participant's artifacts arrive out
	// of order and still be applied in one. Nothing about that is a barrier on this
	// instance's own input.
	return false
}

// barrierBound names the artifacts that must still apply at one agreed tick on
// every instance, the producer included.
//
// Requirement 5 takes the playout lead off the local path, and the reason it can is
// that an artifact's effect is provisional on a guest: the producer applies it now,
// the host applies it in its own order, and the next correction repairs the gap.
// That argument holds for every D-3 crossing, which describes an *effect* on a
// world both instances already have.
//
// It does not hold for the three artifacts that decide what the world *is*. An
// arrival creates a shared cursor and a departure destroys one, and a shared
// entity's identity and creation order are what a capture references by; a reset
// replaces the run. Applied a lead early on the producer, each of those would
// allocate an entity — or a run — the rest of the session numbers differently, and
// a correction would then be repairing identity rather than state. So the
// coordinator, which is their only producer, waits with everyone else. Nobody's
// input is waiting on them: they are the session's own bookkeeping rather than a
// participant's action.
func barrierBound(et event.EventType) bool {
	switch et {
	case event.EventParticipantJoined, event.EventParticipantDeparted, event.EventGameResetRequest:
		return true
	default:
		return false
	}
}

// refreshLink re-reads the negotiated session identity and reports whether the
// barrier owns crossings this tick. Every entry point the system has — the startup
// gate, the update pass, tick open and tick close — needs the same answer, and the
// endpoint may be attached or lost between any two of them.
func (s *NetworkSystem) refreshLink(p engine.NetworkPort) bool {
	// The barrier belongs to the run, not to the link. A session's crossings are
	// deferred by a fixed playout lead and apply at an absolute tick; a stretch that
	// happens to have no peer attached — a lobby still waiting, every participant
	// gone, or a replay reproducing the whole thing — must defer them by the same
	// lead, because the tick an artifact applies at is what the reproduction has to
	// reach. Sending is a separate question, answered by the port.
	active := s.enabled && (s.world.SessionBarrier() ||
		(p != nil && p.IsRunning() && p.PeerCount() > 0))
	if r := s.world.Resources.Network; r != nil {
		s.mu.Lock()
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
		s.mu.Unlock()
	}
	s.barrierActive.Store(active)
	return active
}

// AdoptSnapshot rebases the barrier onto a world this instance just installed.
//
// Three things move at once and all three are the same fact. The production epoch
// belongs to the installed tick rather than to the one this instance had reached,
// or the next crossing it produces would name an apply tick the session left
// behind. Artifacts already scheduled for a tick at or before the installed one are
// dropped, because the capture contains their effect. And the floor is remembered,
// so the artifacts still in flight for those ticks — the ones the host produced
// between admitting this participant and reading the world for it — are recognised
// as already-applied rather than applied twice.
//
// Caller MUST hold updateMutex: it runs inside the install.
func (s *NetworkSystem) AdoptSnapshot(tick uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.crossings); n > 0 {
		s.statDrop.Add(int64(n))
		s.crossings = s.crossings[:0]
	}
	keep := s.scheduled[:0]
	dropped := 0
	for _, a := range s.scheduled {
		if a.applyTick <= tick {
			dropped++
			continue
		}
		keep = append(keep, a)
	}
	s.scheduled = keep
	s.snapshotFloor = tick
	s.productionEpoch = tick + 1
	if r := s.world.Resources.Network; r != nil {
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
	}
	if dropped > 0 {
		s.statPreInstall.Add(int64(dropped))
	}
}

// DrainPeers translates whatever the transport is holding without advancing a tick.
//
// It exists for the join, which has to know the tick the session has reached before
// it decides how many ticks to run. Receive answers that question only as part of a
// tick, and a tick is the thing being decided; this is the same drain with the
// applying half left out, so an artifact is scheduled at the tick it names and
// nothing is applied early.
//
// Caller MUST hold updateMutex.
func (s *NetworkSystem) DrainPeers() {
	p := s.port()
	if p == nil || !s.enabled {
		return
	}
	s.refreshLink(p)
	s.drain(p)
}

// NewestPeerEpoch is the highest production epoch any peer has been seen closing.
// Every tick closes one, empty or not, so it is the session's tick as far as this
// instance can observe it — which is what a freshly installed participant measures
// its own lag against.
func (s *NetworkSystem) NewestPeerEpoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest uint64
	for source, w := range s.epochs {
		if uint32(source) == s.localSource {
			continue
		}
		if n := w.newest(); n > newest {
			newest = n
		}
	}
	return newest
}

// ReportJoinLag publishes how far behind the session a joining participant landed,
// in ticks. It is telemetry rather than a gate: the gate is in App, which refuses a
// join whose lag exceeds the playout lead, because past that its own crossings
// would arrive after the tick they name.
func (s *NetworkSystem) ReportJoinLag(ticks uint64) { s.statJoinLag.Store(int64(ticks)) }

// ActivateSession closes the pre-first-tick input window after startup gates.
// The regular tick path keeps the same state current after disconnects.
func (s *NetworkSystem) ActivateSession() {
	p := s.port()
	active := s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if active {
		peers := 0
		if p != nil {
			peers = p.PeerCount()
		}
		vlog.Info("app", "msg", "network session active",
			"participant", s.participantID(), "coordinator", s.isCoordinator(),
			"barrier_delay_ticks", s.barrierDelayTicks(), "peers", peers)
	}
}

func (s *NetworkSystem) Update() {
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if s.enabled && p != nil && p.IsRunning() {
		s.ticks++
	}
}

// Receive drains transport state and publishes every artifact due before nextTick.
// Caller holds the world lock.
func (s *NetworkSystem) Receive(nextTick uint64) int {
	queued := 0
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if s.enabled && p != nil {
		queued += s.drain(p)
	}
	queued += s.applyDue(nextTick)
	s.publishBarrierTelemetry(nextTick, p)
	return queued
}

// Flush closes completedTick's production epoch, including an empty marker.
// Caller holds the world lock.
func (s *NetworkSystem) Flush(completedTick uint64) {
	p := s.port()
	active := s.refreshLink(p)
	s.flushCrossings(p, completedTick, active)
	if s.enabled && p != nil && p.IsRunning() && s.ticks%parameter.NetworkSyncTicks == 0 {
		s.sendCursorState(p)
	}
	s.publishTransportLoss(p)
	s.publishLinkMeasurement(p, completedTick)
}

// publishLinkMeasurement is both halves of this system's part in the round trip,
// and neither of them is a decision.
//
// Outbound it hands the transport the report a probing peer will be echoed: the
// tick this instance stands on, how far behind it believes it is, how much the
// last correction had to move it, and where its cursor is. Every one of those is
// a scheduling hint — a host may publish to this participant sooner because of
// them — and a wrong or stale one can cost a correction sent early and nothing
// else. Inbound it copies the transport's own estimate into telemetry.
//
// What it deliberately does not do is read a measurement back into the world.
// Network timing may pace a transport and may not enter shared simulation state,
// an RNG stream, a replay or a game decision, and the way that is kept true is
// that this is the only seam between the two and it only ever copies.
func (s *NetworkSystem) publishLinkMeasurement(p engine.NetworkPort, completedTick uint64) {
	link, ok := p.(engine.LinkMeasuringPort)
	if !ok || !s.enabled || !p.IsRunning() {
		return
	}
	link.SetLinkReport(s.linkReport(completedTick))

	// The worst link is the one that decides what a player is told: a session is
	// as constrained as its most constrained edge, and reporting an average would
	// hide exactly the peer that needs saying.
	var worst linkpace.Metrics
	var haveWorst bool
	for _, peer := range link.Peers() {
		m := link.LinkMetric(peer)
		if m.Samples == 0 {
			continue
		}
		if !haveWorst || m.RTT > worst.RTT {
			worst, haveWorst = m, true
		}
	}
	if !haveWorst {
		return
	}
	s.statRTT.Store(worst.RTTMillis())
	s.statRTTMicros.Store(worst.RTT.Microseconds())
	s.statJitter.Store(worst.JitterMillis())
	s.statLinkBps.Store(int64(worst.Throughput))
	s.statLinkLoss.Store(int64(worst.Loss * 100))
	s.statSaturated.Store(worst.Saturated)
}

// linkReport describes this instance's picture for the peers measuring it.
// Caller MUST hold updateMutex: it reads the cursor store.
func (s *NetworkSystem) linkReport(completedTick uint64) network.LinkReport {
	r := network.LinkReport{
		Tick:      completedTick,
		LagTicks:  uint32(min(s.statLag.Load(), math.MaxUint32)),
		Magnitude: uint32(min(max(s.statMagnitude.Load(), 0), math.MaxUint32)),
	}
	// The interest centre is the shared position of a cursor this instance
	// simulates — the shared store's, not the D-18 prediction, which no
	// shared-profile read may reach and which would say nothing more here.
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		if pos, ok := s.world.Positions.GetPosition(cursor); ok {
			r.CursorX, r.CursorY, r.HasCursor = int32(pos.X), int32(pos.Y), true
		}
		break
	}
	return r
}

// drain translates one tick's transport notifications into events
func (s *NetworkSystem) drain(p engine.NetworkPort) int {
	n := p.Drain(s.buf[:])
	queued := 0
	for i := range n {
		in := &s.buf[i]
		switch in.Kind {
		case network.InboundConnect:
			s.world.PushLocal(event.EventNetworkConnect, &event.NetworkConnectPayload{PeerID: uint32(in.Peer)})
			queued++
		case network.InboundDisconnect:
			s.world.PushLocal(event.EventNetworkDisconnect, &event.NetworkDisconnectPayload{PeerID: uint32(in.Peer)})
			s.reportDisconnect(uint32(in.Peer), p.PeerCount())
			s.forgetDigestPeer(uint32(in.Peer))
			s.noticeDeparture(uint32(in.Peer))
			queued++
		case network.InboundMessage:
			queued += s.dispatchMessage(uint32(in.Peer), in.Msg)
		}
	}
	return queued
}

// reportDisconnect makes link loss visible independently of digest comparison.
// Once a link disappears there is no peer left on that edge to send a mismatching
// digest, so waiting for DESYNC would make the most serious transport failure look
// like silence. NET:DOWN remains the persistent indicator; this local message and
// warning name the event when it happens. Losing participant one is called out on a
// guest because the current membership protocol cannot replace its coordinator.
func (s *NetworkSystem) reportDisconnect(peerID uint32, remaining int) {
	coordinatorLost := peerID == coordinatorParticipant && !s.isCoordinator()
	message := fmt.Sprintf("Participant %d disconnected", peerID)
	if coordinatorLost {
		message = "Host connection lost; this session cannot recover automatically"
	}
	s.world.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message: message, Duration: 4 * parameter.StatusMessageDefaultTimeout, DurationOverride: true,
	})
	vlog.Warn("app", "msg", "network peer disconnected", "participant", s.participantID(), "peer", peerID,
		"coordinator_lost", coordinatorLost, "remaining_peers", remaining)
}

func (s *NetworkSystem) forgetDigestPeer(peer uint32) {
	if peer == 0 || int(peer) >= len(s.pendingDigest) {
		return
	}
	s.pendingDigest[peer] = stateDigest{}
	s.snapshots[peer] = network.SnapshotAssembly{}
}

// noticeDeparture reacts to a lost link. It deliberately does not despawn anything:
// only a direct neighbour sees a disconnect, and it sees it at a moment of its own
// transport's choosing, so acting here would remove a shared cursor at a different
// tick on every instance — and not at all on instances that never linked to the
// departing participant.
//
// Instead exactly one instance turns the observation into a crossing. The
// coordinator is that instance: it is the only participant every topology this
// session can build guarantees a path to, and one producer is what gives the
// departure a single apply tick. A neighbour that is not the coordinator floods a
// notice instead, deduped by departing participant like any other artifact.
//
// The identity release is separate and stays local: which identities the lobby may
// hand out again is this instance's own bookkeeping, not shared state.
func (s *NetworkSystem) noticeDeparture(peerID uint32) {
	if r := s.world.Resources.Network; r != nil && r.OnDeparture != nil {
		r.OnDeparture(peerID)
	}
	s.announceDeparture(peerID, 0)
}

// announceDeparture crosses or forwards one departure, once. from is the link a
// notice arrived on, or zero when this instance observed the disconnect itself.
func (s *NetworkSystem) announceDeparture(peerID uint32, from uint32) bool {
	if peerID == 0 || int(peerID) >= len(s.departed) || s.departed[peerID] {
		return false
	}
	s.departed[peerID] = true

	slot, ok := s.participantSlot(peerID)
	if !ok {
		return true
	}
	if s.isCoordinator() {
		s.crossSession(event.EventParticipantDeparted,
			&event.ParticipantDepartedPayload{Participant: peerID, Slot: slot})
		return true
	}
	p := s.port()
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return true
	}
	body, err := json.Marshal(event.ParticipantDepartedPayload{Participant: peerID, Slot: slot})
	if err != nil {
		s.statDrop.Add(1)
		return true
	}
	p.BroadcastExcept(from, uint8(network.MsgDisconnect), body)
	s.statRelayed.Add(1)
	return true
}

// receiveDeparture handles a neighbour's notice: the coordinator turns it into the
// crossing, anyone else passes it on.
func (s *NetworkSystem) receiveDeparture(from uint32, body []byte) {
	var p event.ParticipantDepartedPayload
	if err := json.Unmarshal(body, &p); err != nil {
		s.statDrop.Add(1)
		return
	}
	if !s.announceDeparture(p.Participant, from) {
		s.statDuplicates.Add(1)
	}
}

// crossSession pushes a roster change as a D-3 crossing carrying OriginSession, so
// the record enters the replay journal. Nothing else in the stream implies a roster
// change — it originates in a transport observation — and a participant catching up
// by replaying that stream has to see it.
func (s *NetworkSystem) crossSession(t event.EventType, payload any) {
	s.world.PushEventFull(t, payload, event.OriginSession, core.DomainPlayer)
}

// isCoordinator reports whether this instance owns the session's first identity,
// which is the one the handshake always assigns to the host.
func (s *NetworkSystem) isCoordinator() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localSource == coordinatorParticipant
}

// participantSlot finds the roster slot a peer-owned cursor occupies.
// Caller MUST hold updateMutex.
func (s *NetworkSystem) participantSlot(peerID uint32) (uint8, bool) {
	var slot uint8
	found := false
	s.world.Components.Cursor.Each(func(_ core.Entity, c *component.CursorComponent) bool {
		if c.Control == component.ControlRemote && c.PeerID == peerID {
			slot, found = c.Slot, true
			return false
		}
		return true
	})
	return slot, found
}

// publishConnectionTelemetry exposes the poll endpoint and D-14 latch state.
func (s *NetworkSystem) publishConnectionTelemetry(p engine.NetworkPort) {
	peers := 0
	state := "off"
	if p != nil {
		peers = p.PeerCount()
		if statePort, ok := p.(interface{ ConnectionState() network.ConnState }); ok {
			switch statePort.ConnectionState() {
			case network.StateConnected:
				state = "connected"
			case network.StateConnecting:
				state = "waiting"
			default:
				state = "down"
			}
		} else {
			switch {
			case !p.IsRunning():
				state = "down"
			case peers == 0:
				state = "waiting"
			default:
				state = "connected"
			}
		}
	}
	latched := s.world.SessionShared()
	s.statPeers.Store(int64(peers))
	s.statConnected.Store(peers > 0)
	s.statConnection.StoreIfChanged(state)
	s.statMapLatched.Store(latched)
	s.world.Resources.Status.Bools.Get("context.map_locked").Store(
		s.world.Resources.Config.CropOnResize && latched)
}

// dispatchMessage admits the three steady-state message kinds the domain model
// defines: the D-3 artifact epoch, D-13 owner-authored sync and D-11 parity digest.
// Raw participant input is not one of them — a peer sends the resolved artifact,
// never the keystroke that produced it — so an unrecognised type is counted and
// discarded rather than translated.
func (s *NetworkSystem) dispatchMessage(from uint32, msg *network.Message) int {
	if msg == nil {
		return 0
	}
	switch msg.Type {
	case network.MsgEvent:
		s.scheduleCrossings(from, msg.Payload)
	case network.MsgStateSync:
		s.applyCursorState(from, msg.Payload)
	case network.MsgStateDigest:
		s.receiveStateDigest(from, msg.Payload)
	case network.MsgDisconnect:
		s.receiveDeparture(from, msg.Payload)
	case network.MsgStateCorrection:
		s.receiveCorrection(from, msg.Payload)
	default:
		s.statDrop.Add(1)
	}
	return 0
}

// receiveCorrection reassembles one authoritative correction and hands the body to
// the session layer.
//
// Nothing here decodes or installs it. This runs inside the tick that drained the
// chunk, under the world lock a correction's install needs for itself, and a
// correction is hundreds of kilobytes of JSON — so what happens here is a copy into
// a queue and nothing else.
//
// A malformed transfer resets that peer's assembly rather than poisoning it: the
// host sends a whole correction every SnapshotKeyframeCorrections and each one is
// self-sufficient, so the recovery from a broken transfer is to wait for the next.
func (s *NetworkSystem) receiveCorrection(from uint32, body []byte) {
	if from == 0 || int(from) >= len(s.snapshots) {
		s.statDrop.Add(1)
		return
	}
	asm := &s.snapshots[from]
	admitted, done, err := asm.AddChunk(body)
	if err != nil {
		*asm = network.SnapshotAssembly{}
		s.statDrop.Add(1)
		vlog.Warn("app", "msg", "correction chunk refused", "peer", from, "error", err.Error())
		return
	}
	if !admitted {
		s.statDuplicates.Add(1)
		return
	}
	// Forwarded on the same argument the artifact flood uses: a node relays only
	// what it admitted, so a copy arriving by a second path is recognised and the
	// flood terminates. Without it the authority would reach only the participants
	// the host happens to be linked to directly, and a relayed session would have
	// crossings but no corrections.
	if p := s.port(); p != nil && p.IsRunning() && p.PeerCount() > 0 {
		p.BroadcastExcept(from, uint8(network.MsgStateCorrection), body)
		s.statRelayed.Add(1)
	}
	if !done {
		return
	}
	tick, whole := asm.Result()
	*asm = network.SnapshotAssembly{}
	s.statCorrections.Add(1)
	if r := s.world.Resources.Network; r != nil && r.OnCorrection != nil {
		r.OnCorrection(tick, whole)
	}
}

// publishTransportLoss exposes frames the link lost outside the barrier: inbound
// notifications a full poll buffer discarded, and outbound frames a peer's send
// queue refused. Either one silently desynchronises two instances, so a new loss
// is logged as well as counted.
func (s *NetworkSystem) publishTransportLoss(p engine.NetworkPort) {
	if p == nil {
		return
	}
	loss, ok := p.(interface {
		Dropped() uint64
		Refused() uint64
	})
	if !ok {
		return
	}
	in, out := loss.Dropped(), loss.Refused()
	s.statLostIn.Store(int64(in))
	s.statLostOut.Store(int64(out))
	if in == s.lastLostIn && out == s.lastLostOut {
		return
	}
	vlog.Warn("app", "msg", "network transport loss",
		"inbound_dropped", in, "outbound_refused", out)
	s.lastLostIn, s.lastLostOut = in, out
}

// flushCrossings advances the epoch under the producer lock, then sends its marker.
func (s *NetworkSystem) flushCrossings(p engine.NetworkPort, completedTick uint64, active bool) {
	s.mu.Lock()
	pending := s.crossings
	s.crossings = nil
	dropped := s.encodeErr
	s.encodeErr = 0
	s.productionEpoch = completedTick + 1
	source := s.localSource
	s.mu.Unlock()

	if dropped != 0 {
		s.statDrop.Add(dropped)
	}
	if !active {
		if len(pending) != 0 {
			s.statDrop.Add(int64(len(pending)))
		}
		return
	}
	// The barrier can own a tick that has nobody to send it to: a lobby still
	// waiting, a session whose peers have all left, or a run being reproduced. The
	// artifacts are still scheduled and still apply locally at their own tick.
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return
	}
	body, err := event.EncodeWireBatch(event.WireBatch{
		Source: source, ProducedTick: completedTick, Frames: pending,
	})
	if err != nil {
		s.statDrop.Add(int64(len(pending)))
		vlog.Warn("app", "msg", "network encode", "frames", len(pending), "error", err.Error())
		return
	}
	p.Broadcast(uint8(network.MsgEvent), body)
	s.statSent.Add(int64(len(pending)))
	if completedTick%parameter.NetworkDigestTicks == 0 {
		s.sendStateDigest(p, completedTick)
	}
}

// sendStateDigest publishes one completed-tick hash to direct neighbours. Digest
// messages need no mesh flood: if each edge agrees, the connected graph agrees.
func (s *NetworkSystem) sendStateDigest(p engine.NetworkPort, completedTick uint64) {
	r := s.world.Resources.Network
	if r == nil || r.SharedDigest == nil {
		return
	}
	// No breakdown on the wire. Detail used to be requested as soon as a sample
	// disagreed, because a disagreement was a fault worth naming record by record.
	// Under an authority a guest disagrees with the host between every pair of
	// corrections by design, so asking for detail would mean sending a map of
	// per-record hashes with every digest for the whole session — bandwidth spent
	// describing the condition this phase exists to make ordinary. The App-side
	// breakdown stays, for the tests and the tools that compare two runs offline.
	digest := r.SharedDigest(false)
	sample := stateDigest{
		Run:       s.world.Resources.Event.Queue.Stamp().Run,
		Tick:      completedTick,
		Hash:      digest.Hash,
		Positions: digest.Positions,
		Kinetics:  digest.Kinetics,
		Combat:    digest.Combat,
		Context:   digest.Context,
		Status:    digest.Status,
		Surface:   digest.Surface,
		Valid:     true,
	}
	s.recordStateDigest(sample)
	body, err := json.Marshal(sample)
	if err != nil {
		s.statDrop.Add(1)
		return
	}
	p.Broadcast(uint8(network.MsgStateDigest), body)
}

// receiveStateDigest compares immediately when this tick is already in local
// history, or holds the peer sample until the local Flush reaches it.
func (s *NetworkSystem) receiveStateDigest(from uint32, body []byte) {
	if from == 0 || int(from) >= len(s.pendingDigest) {
		s.statDrop.Add(1)
		return
	}
	var remote stateDigest
	if err := json.Unmarshal(body, &remote); err != nil {
		s.statDrop.Add(1)
		return
	}
	remote.Valid = true
	local := s.digestHistory[remote.Tick%uint64(len(s.digestHistory))]
	if local.Valid && local.Run == remote.Run && local.Tick == remote.Tick {
		s.compareStateDigest(from, local, remote)
		return
	}
	s.pendingDigest[from] = remote
}

func (s *NetworkSystem) recordStateDigest(local stateDigest) {
	s.digestHistory[local.Tick%uint64(len(s.digestHistory))] = local
	for peer := 1; peer < len(s.pendingDigest); peer++ {
		remote := s.pendingDigest[peer]
		if !remote.Valid || remote.Run != local.Run || remote.Tick != local.Tick {
			continue
		}
		s.pendingDigest[peer] = stateDigest{}
		s.compareStateDigest(uint32(peer), local, remote)
	}
}

// compareStateDigest folds one peer's sample against this instance's own at the
// same tick, and records the difference as a *gauge* rather than as a verdict.
//
// This is where D-11's weakening lands in the runtime. Before Phase 4 a
// disagreement was a fault: both instances re-derived the shared world from one
// artifact stream, so if they disagreed one of them had lost an artifact and
// nothing would ever re-derive it — DESYNC after two samples, DIVERGED after five,
// and the session was over. Under an authority none of that holds. A guest applies
// its own input immediately and extrapolates between corrections, so it is
// *expected* to differ from the host, and the difference is repaired on a cadence
// rather than mourned. The counter and the part name survive because they are still
// the cheapest description of where two instances stand apart; the escalation does
// not, because there is nothing left for it to be right about.
//
// What replaced it is two numbers with better claims: the correction magnitude,
// which says how far apart the two actually were at the moment the authority
// arrived, and the staleness, which says whether this instance is far enough behind
// that its own artifacts are reaching the host late.
func (s *NetworkSystem) compareStateDigest(peer uint32, local, remote stateDigest) {
	if local.Hash == remote.Hash {
		return
	}
	s.statDigestMismatch.Add(1)
	s.statDriftPart.StoreIfChanged(digestDifference(local, remote))
	s.statDriftTick.Store(int64(local.Tick))
}

func digestDifference(local, remote stateDigest) string {
	switch {
	case local.Positions != remote.Positions:
		return "positions"
	case local.Kinetics != remote.Kinetics:
		return "kinetics"
	case local.Combat != remote.Combat:
		return "combat"
	case local.Context != remote.Context:
		return "context"
	case local.Status != remote.Status:
		return "status"
	case local.Surface != remote.Surface:
		return "snapshot"
	default:
		return "combined"
	}
}

// scheduleCrossings buffers one peer epoch and passes it on. Application waits for
// the artifact's own target tick, which travels with it, so a relayed epoch applies
// at the same tick however many links it crossed to arrive.
//
// Authority and topology are separate: the host owns the canonical shared world,
// while a participant sends only to peers it dialled or accepted. An artifact
// therefore reaches everyone else by being forwarded. Every node floods each epoch
// it has not seen to every link except the one it arrived on. What terminates the
// flood is the per-source epoch window: a second copy arriving by another path is
// recognised and neither applied nor forwarded again, so each node handles each
// epoch exactly once whatever the topology. The hop limit is a backstop, not the
// termination argument.
func (s *NetworkSystem) scheduleCrossings(from uint32, body []byte) {
	batch, err := event.DecodeWireBatch(body)
	if err != nil {
		s.statDrop.Add(1)
		vlog.Warn("app", "msg", "network decode", "error", err.Error())
		return
	}
	if batch.Source == 0 || int(batch.Source) >= len(s.epochs) {
		s.statDrop.Add(int64(max(1, len(batch.Frames))))
		return
	}

	s.mu.Lock()
	admitted := s.epochs[batch.Source].admit(batch.ProducedTick)
	installed := 0
	if admitted {
		for _, f := range batch.Frames {
			// Everything due at or before the installed world's tick is already in
			// it. The epoch is still admitted, so the relay and the duplicate
			// window stay coherent for the peers that did not install one.
			if f.ApplyTick <= s.snapshotFloor {
				installed++
				continue
			}
			s.scheduled = append(s.scheduled, barrierArtifact{
				frame: f.Frame, applyTick: f.ApplyTick, source: batch.Source, origin: event.OriginNetwork,
			})
		}
	}
	s.mu.Unlock()
	if installed > 0 {
		s.statPreInstall.Add(int64(installed))
	}

	if !admitted {
		s.statDuplicates.Add(1)
		return
	}
	s.relayBatch(from, batch)
}

// relayBatch forwards one admitted epoch onward, unchanged apart from the hop count.
// Source, ProducedTick and every frame's ApplyTick and sequence are what make the
// artifact identical on every instance, so a relay must not restamp any of them.
func (s *NetworkSystem) relayBatch(from uint32, batch event.WireBatch) {
	p := s.port()
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return
	}
	if batch.Hops >= parameter.NetworkRelayHopLimit {
		s.statDrop.Add(1)
		vlog.Warn("app", "msg", "network relay hop limit",
			"source", batch.Source, "produced_tick", batch.ProducedTick)
		return
	}
	batch.Hops++
	body, err := event.EncodeWireBatch(batch)
	if err != nil {
		s.statDrop.Add(int64(max(1, len(batch.Frames))))
		vlog.Warn("app", "msg", "network relay encode", "error", err.Error())
		return
	}
	// Excluding the arriving link is traffic economy, not correctness: that peer
	// admitted the epoch before sending it and would recognise its own copy.
	p.BroadcastExcept(from, uint8(network.MsgEvent), body)
	s.statRelayed.Add(1)
}

// applyDue publishes due artifacts in the same source/sequence order everywhere.
func (s *NetworkSystem) applyDue(nextTick uint64) int {
	s.mu.Lock()
	due := make([]barrierArtifact, 0, len(s.scheduled))
	keep := s.scheduled[:0]
	for _, a := range s.scheduled {
		if a.applyTick <= nextTick {
			due = append(due, a)
		} else {
			keep = append(keep, a)
		}
	}
	s.scheduled = keep
	localSource := s.localSource
	s.mu.Unlock()

	slices.SortFunc(due, func(a, b barrierArtifact) int {
		if n := cmp.Compare(a.applyTick, b.applyTick); n != 0 {
			return n
		}
		if n := cmp.Compare(a.source, b.source); n != 0 {
			return n
		}
		return cmp.Compare(a.frame.Seq, b.frame.Seq)
	})

	local, peer := 0, 0
	for _, a := range due {
		et, payload, domain, err := a.frame.Decode()
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		if !admissibleFromSource(et, a.source) {
			s.statForged.Add(1)
			vlog.Warn("app", "msg", "artifact refused",
				"peer", a.source, "event", event.GetEventName(et), "apply_tick", a.applyTick)
			continue
		}
		if a.applyTick < nextTick {
			// Late, and no longer a divergence. Under an authority the host applies
			// what reaches it in the order it reaches it, and a guest whose artifact
			// arrived after the tick it named gets the host's ordering back in the
			// next correction. The counter stays because it is what says a
			// participant's link is not keeping the playout lead.
			s.statLate.Add(1)
		}
		s.world.Resources.Event.Queue.PushReady(event.GameEvent{
			Type: et, Payload: payload, Origin: a.origin, Domain: domain,
		})
		if a.source == localSource {
			local++
		} else {
			peer++
		}
	}
	s.statAppliedLocal.Add(int64(local))
	s.statAppliedPeer.Add(int64(peer))
	s.statPeerArtifacts.Store(int64(peer))
	s.statPeerApplied.Store(peer != 0)
	s.statRecv.Add(int64(peer))
	return len(due)
}

// admissibleFromSource is the host's validation, and it is deliberately narrow.
//
// Requirement 2 makes the host the authority over what happens, and most of that is
// structural: it applies the artifacts it receives in its own order and its
// resulting world is what ships, so a guest cannot make the session believe
// something by producing an artifact — it can only make the session believe it for
// as long as the next correction takes to arrive. What that argument does *not*
// cover is the roster, because a roster crossing does not describe a shared outcome,
// it creates or destroys a shared entity: an arrival spawns a cursor at one agreed
// tick on every instance and a departure despawns one, and both are produced by the
// coordinator alone precisely so that there is one apply tick for them. An artifact
// of either kind arriving from anyone else is refused.
//
// Everything past this — a guest attributing a crossing to another participant's
// cursor, a peer replaying an artifact it never produced — is an *authentication*
// question rather than an authority one, and this plan puts authentication in
// Phase 6, before anything beyond trusted peers.
func admissibleFromSource(et event.EventType, source uint32) bool {
	switch et {
	case event.EventParticipantJoined, event.EventParticipantDeparted:
		return source == coordinatorParticipant
	default:
		return true
	}
}

// publishBarrierTelemetry reports playout lead and epochs that ran before a marker.
func (s *NetworkSystem) publishBarrierTelemetry(nextTick uint64, p engine.NetworkPort) {
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		s.statPeerLag.Store(0)
		return
	}
	s.mu.Lock()
	delay := s.delayTicks
	local := s.localSource
	minPeer := uint64(0)
	seen := 0
	for source := 1; source < len(s.epochs); source++ {
		newest := s.epochs[source].newest()
		if uint32(source) == local || newest == 0 {
			continue
		}
		if seen == 0 || newest < minPeer {
			minPeer = newest
		}
		seen++
	}
	s.mu.Unlock()

	required := uint64(0)
	if nextTick > delay {
		required = nextTick - delay
	}
	lag := uint64(0)
	if required > minPeer {
		lag = required - minPeer
	}
	s.statPeerLag.Store(int64(lag))
	s.publishStaleness(nextTick)
	// Sources are participants, not links: in a mesh most of them arrive relayed, so
	// the count this instance should hear from is the remote half of the roster
	// rather than the number of peers it happens to be connected to.
	if required != 0 && (seen < s.remoteParticipants() || lag != 0) {
		s.statRanWithout.Add(1)
	}
}

// publishStaleness reports how far behind the session this instance stands, every
// tick, which is the measurement Phase 3 took once at admission and never again.
//
// resumeJoinedSession closes the gap a transfer opens and refuses a join it cannot
// close — and then nothing looked at it for the rest of the run, so a participant
// whose machine fell behind mid-session produced artifacts that reached the host
// after the ticks they named and had no way to know. The number is the same one:
// the newest tick any peer has been seen closing, minus this instance's own. Past
// the playout lead this participant's own crossings are landing late on the host,
// which is exactly when a player should be told the link is the problem.
//
// It is a gauge and not a gate. A guest that falls behind is corrected like any
// other guest; what changes is that it can say so.
func (s *NetworkSystem) publishStaleness(nextTick uint64) {
	newest := s.NewestPeerEpoch()
	lag := uint64(0)
	if local := nextTick; newest > local {
		lag = newest - local
	}
	s.statLag.Store(int64(lag))
	s.statStale.Store(lag > parameter.SnapshotStaleTicks)
}

// remoteParticipants counts the rostered cursors another instance simulates.
// Caller MUST hold updateMutex.
func (s *NetworkSystem) remoteParticipants() int {
	n := 0
	s.world.Components.Cursor.Each(func(_ core.Entity, c *component.CursorComponent) bool {
		if c.Control == component.ControlRemote {
			n++
		}
		return true
	})
	return n
}

// sendCursorState broadcasts the owner-authored state of every cursor this
// instance simulates (D-13). One message per cursor keeps a late arrival from
// holding up the others.
func (s *NetworkSystem) sendCursorState(p engine.NetworkPort) {
	s.syncSeq++
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		body, err := event.EncodeFrames([]event.WireFrame{stateFrame(s.readCursorState(cursor, uint8(i)))})
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		p.Broadcast(uint8(network.MsgStateSync), body)
	}
}

// readCursorState reads one cursor's D-13 set into a payload.
// Caller MUST hold updateMutex — Update runs inside the tick.
func (s *NetworkSystem) readCursorState(cursor core.Entity, slot uint8) *event.CursorStatePayload {
	p := &event.CursorStatePayload{Entity: cursor, Slot: slot, Seq: s.syncSeq}

	if c, ok := s.world.Components.Energy.GetComponent(cursor); ok {
		p.Energy = c.Current
	}
	if c, ok := s.world.Components.Heat.GetComponent(cursor); ok {
		p.Heat, p.Overheat, p.EmberActive = c.Current, c.Overheat, c.EmberActive
	}
	if c, ok := s.world.Components.Shield.GetComponent(cursor); ok {
		p.ShieldActive = c.Active
		p.ShieldRadiusX, p.ShieldRadiusY = c.RadiusX, c.RadiusY
		p.ShieldInvRxSq, p.ShieldInvRySq = c.InvRxSq, c.InvRySq
	}
	if c, ok := s.world.Components.Boost.GetComponent(cursor); ok {
		p.BoostActive = c.Active
		p.BoostRemaining, p.BoostTotal = int64(c.Remaining), int64(c.TotalDuration)
	}
	if c, ok := s.world.Components.Weapon.GetComponent(cursor); ok {
		p.WeaponCharges = make([]int, component.WeaponCount)
		p.WeaponCooldown = make([]int64, component.WeaponCount)
		for wt := range component.WeaponCount {
			p.WeaponCharges[wt] = c.Charges[wt]
			p.WeaponCooldown[wt] = int64(c.Cooldown[wt])
		}
		p.MainFireCooldown = int64(c.MainFireCooldown)
	}
	if c, ok := s.world.Components.Combat.GetComponent(cursor); ok {
		p.HitPoints, p.DamageImmunity = c.HitPoints, int64(c.RemainingDamageImmunity)
	}
	if c, ok := s.world.Components.CursorView.GetComponent(cursor); ok {
		p.ErrorFlash, p.BurstFlash = int64(c.ErrorFlashRemaining), int64(c.BurstFlashRemaining)
		p.BlinkActive, p.BlinkType, p.BlinkLevel = c.BlinkActive, c.BlinkType, c.BlinkLevel
		p.BlinkRemaining = int64(c.BlinkRemaining)
	}
	if c, ok := s.world.Components.Pulse.GetComponent(cursor); ok {
		p.PulseActive = true
		p.PulseOriginX, p.PulseOriginY = c.OriginX, c.OriginY
		p.PulseDuration, p.PulseRemaining = int64(c.Duration), int64(c.Remaining)
	}
	return p
}

// applyCursorState writes a peer's cursor state. The write is admitted only for a
// cursor this instance does not simulate, so the transported value and a local
// writer can never both author one cell (D-2, D-13).
// A sync is relayed only when this instance had not already applied it. That is the
// same termination argument as the epoch flood, using the per-slot sequence in place
// of the per-source epoch window: state is a whole snapshot rather than a delta, so
// the newest wins and an older one needs neither applying nor forwarding.
func (s *NetworkSystem) applyCursorState(from uint32, body []byte) {
	frames, err := event.DecodeFrames(body)
	if err != nil {
		s.statDrop.Add(1)
		return
	}
	applied := false
	for _, f := range frames {
		et, payload, _, err := f.Decode()
		if err != nil || et != event.EventCursorStateSync {
			s.statDrop.Add(1)
			continue
		}
		p, ok := payload.(*event.CursorStatePayload)
		if !ok {
			s.statDrop.Add(1)
			continue
		}
		applied = s.writeCursorState(p) || applied
	}
	if !applied {
		s.statDuplicates.Add(1)
		return
	}
	if p := s.port(); p != nil && p.IsRunning() && p.PeerCount() > 0 {
		p.BroadcastExcept(from, uint8(network.MsgStateSync), body)
		s.statRelayed.Add(1)
	}
}

// writeCursorState applies one sync, dropping a stale or misaddressed one. The
// payload names both an entity and a slot; they must agree, because the entity
// selects the cells written and the slot keys the sequence that decides whether to
// write them at all. A disagreement would age one participant's state under
// another's sequence, so it is dropped rather than reconciled.
func (s *NetworkSystem) writeCursorState(p *event.CursorStatePayload) bool {
	cursor := s.world.ResolveCursor(p.Entity)
	if cursor == 0 || s.world.SimulatesLocally(cursor) || int(p.Slot) >= parameter.MaxPlayers {
		s.statDrop.Add(1)
		return false
	}
	if slot, ok := s.world.CursorSlot(cursor); !ok || slot != p.Slot {
		s.statDrop.Add(1)
		return false
	}
	if p.Seq <= s.lastSync[p.Slot] {
		s.statDrop.Add(1) // reordered or replayed; the newer value already landed
		return false
	}
	s.lastSync[p.Slot] = p.Seq

	if c, ok := s.world.Components.Energy.GetPtr(cursor); ok {
		c.Current = p.Energy
	}
	if c, ok := s.world.Components.Heat.GetPtr(cursor); ok {
		c.Current, c.Overheat, c.EmberActive = p.Heat, p.Overheat, p.EmberActive
	}
	if c, ok := s.world.Components.Shield.GetPtr(cursor); ok {
		c.Active = p.ShieldActive
		c.RadiusX, c.RadiusY = p.ShieldRadiusX, p.ShieldRadiusY
		c.InvRxSq, c.InvRySq = p.ShieldInvRxSq, p.ShieldInvRySq
	}
	if c, ok := s.world.Components.Boost.GetPtr(cursor); ok {
		c.Active = p.BoostActive
		c.Remaining, c.TotalDuration = time.Duration(p.BoostRemaining), time.Duration(p.BoostTotal)
	}
	if c, ok := s.world.Components.Weapon.GetPtr(cursor); ok {
		for wt := range min(component.WeaponCount, component.WeaponType(len(p.WeaponCharges))) {
			c.Charges[wt] = p.WeaponCharges[wt]
		}
		for wt := range min(component.WeaponCount, component.WeaponType(len(p.WeaponCooldown))) {
			c.Cooldown[wt] = time.Duration(p.WeaponCooldown[wt])
		}
		c.MainFireCooldown = time.Duration(p.MainFireCooldown)
	}
	if c, ok := s.world.Components.Combat.GetPtr(cursor); ok {
		c.HitPoints = p.HitPoints
		c.RemainingDamageImmunity = time.Duration(p.DamageImmunity)
	}
	if c, ok := s.world.Components.CursorView.GetPtr(cursor); ok {
		c.ErrorFlashRemaining = time.Duration(p.ErrorFlash)
		c.BurstFlashRemaining = time.Duration(p.BurstFlash)
		c.BlinkActive, c.BlinkType, c.BlinkLevel = p.BlinkActive, p.BlinkType, p.BlinkLevel
		c.BlinkRemaining = time.Duration(p.BlinkRemaining)
	}
	switch {
	case p.PulseActive:
		s.world.Components.Pulse.SetComponent(cursor, component.PulseComponent{
			OriginX: p.PulseOriginX, OriginY: p.PulseOriginY,
			Duration:  time.Duration(p.PulseDuration),
			Remaining: time.Duration(p.PulseRemaining),
		})
	case s.world.Components.Pulse.HasEntity(cursor):
		s.world.Components.Pulse.RemoveEntity(cursor, false)
	}
	s.statState.Add(1)
	return true
}

// stateFrame wraps one cursor sync in the frame the codec already carries, so
// state and crossings share one encoder
func stateFrame(p *event.CursorStatePayload) event.WireFrame {
	f, _ := event.NewWireFrame(event.GameEvent{
		Type: event.EventCursorStateSync, Payload: p, Domain: core.DomainShared,
	})
	return f
}
