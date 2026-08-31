package system

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	statSyncState      *status.AtomicString

	digestHistory   [parameter.NetworkEpochWindow]stateDigest
	pendingDigest   [parameter.MaxPlayers + 1]stateDigest
	peerDesync      [parameter.MaxPlayers + 1]bool
	peerMismatches  [parameter.MaxPlayers + 1]int
	syncNoticeUntil uint64
	statSyncPart    *status.AtomicString
	statSyncRecords *status.AtomicString
	statSyncTick    *atomic.Int64
	statDiverged    *atomic.Bool

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

	// Groups carries one hash per snapshot record, and only while a mismatch is
	// already outstanding. A category names the surface that disagrees; this names
	// the record, which is what a two-host post-mortem actually needs and what a
	// single host's log cannot otherwise recover.
	Groups map[string]uint64 `json:"groups,omitempty"`

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
	s.statSyncState = s.textStat(reg, "network.sync_state")
	s.statSyncPart = s.textStat(reg, "network.sync_part")
	s.statSyncRecords = s.textStat(reg, "network.sync_records")
	s.statSyncTick = s.intStat(reg, "network.sync_tick")
	s.statDiverged = s.boolStat(reg, "network.diverged")

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
	s.peerMismatches = [parameter.MaxPlayers + 1]int{}
	s.lastLostIn, s.lastLostOut = 0, 0
	s.barrierActive.Store(false)
	s.digestHistory = [parameter.NetworkEpochWindow]stateDigest{}
	s.pendingDigest = [parameter.MaxPlayers + 1]stateDigest{}
	s.peerDesync = [parameter.MaxPlayers + 1]bool{}
	s.syncNoticeUntil = 0

	s.mu.Lock()
	s.crossings = s.crossings[:0]
	s.scheduled = s.scheduled[:0]
	s.epochs = [parameter.MaxPlayers + 1]epochWindow{}
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

// DiscardArtifactsThrough drops scheduled artifacts whose apply tick a replayed
// session log has already covered. A participant catching up receives live epochs
// while it replays, and everything they carry up to the log's end is in the records
// it just applied; applying both would double every crossing in that window.
func (s *NetworkSystem) DiscardArtifactsThrough(tick uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.scheduled[:0]
	for _, a := range s.scheduled {
		if a.applyTick > tick {
			keep = append(keep, a)
		}
	}
	s.scheduled = keep
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
	s.scheduled = append(s.scheduled, barrierArtifact{
		frame: frame, applyTick: applyTick, source: s.localSource, origin: ev.Origin,
	})
	s.mu.Unlock()
	event.ReleaseDeferredPayload(ev.Payload)
	s.statDeferred.Add(1)
	return true
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
	if s.statSyncState.Load() == "synced" && s.ticks >= s.syncNoticeUntil {
		s.statSyncState.Store("")
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
	if peer == 0 || int(peer) >= len(s.peerDesync) {
		return
	}
	s.peerDesync[peer] = false
	s.peerMismatches[peer] = 0
	s.pendingDigest[peer] = stateDigest{}
	if !s.anyPeerDesynced() {
		s.statSyncState.Store("")
		s.statSyncPart.Store("")
		s.statSyncRecords.Store("")
		s.statDiverged.Store(false)
		s.syncNoticeUntil = 0
	}
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
	default:
		s.statDrop.Add(1)
	}
	return 0
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
	// Detail costs a map on the wire, so it is sent only once this instance has
	// something to explain: one disagreeing sample is enough to start, which puts
	// the breakdown on both sides before the second sample reports.
	digest := r.SharedDigest(s.anyPeerMismatching())
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
		Groups:    digest.Groups,
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

// compareStateDigest folds one peer's sample against this instance's own at the same
// tick. A single disagreement is not yet a report: an artifact that missed its apply
// tick lands one side late and the next sample finds the two equal again, so the
// indicator waits for NetworkDesyncSamples consecutive disagreements. Past
// NetworkDivergedSamples the divergence is no longer transient — nothing re-derives
// a missing artifact — and the session is marked diverged, which is what a recovery
// acts on.
func (s *NetworkSystem) compareStateDigest(peer uint32, local, remote stateDigest) {
	wasDesynced := s.anyPeerDesynced()

	if local.Hash == remote.Hash {
		s.peerMismatches[peer] = 0
		s.peerDesync[peer] = false
		if wasDesynced && !s.anyPeerDesynced() {
			s.statSyncState.Store("synced")
			s.statSyncPart.Store("")
			s.statSyncRecords.Store("")
			s.statDiverged.Store(false)
			s.syncNoticeUntil = s.ticks + parameter.NetworkResyncNoticeTicks
			vlog.Info("app", "msg", "shared state resynchronised",
				"run", local.Run, "tick", local.Tick)
		}
		return
	}

	s.statDigestMismatch.Add(1)
	s.peerMismatches[peer]++
	n := s.peerMismatches[peer]
	if n < parameter.NetworkDesyncSamples {
		return
	}

	part := digestDifference(local, remote)
	records := digestRecordDifference(local, remote)
	if !s.peerDesync[peer] {
		s.peerDesync[peer] = true
		s.statSyncState.Store("desync")
		s.statSyncPart.Store(part)
		s.statSyncTick.Store(int64(local.Tick))
		s.statSyncRecords.StoreIfChanged(records)
		s.syncNoticeUntil = 0
		vlog.Warn("app", "msg", "shared state divergence", "peer", peer,
			"run", local.Run, "tick", local.Tick, "part", part, "records", records,
			"samples", n, "local", local.Hash, "remote", remote.Hash)
	} else if records != "" {
		s.statSyncRecords.StoreIfChanged(records)
	}
	if n >= parameter.NetworkDivergedSamples && !s.statDiverged.Swap(true) {
		vlog.Error("app", "msg", "shared state diverged", "peer", peer,
			"run", local.Run, "tick", local.Tick, "part", part, "records", records,
			"samples", n)
	}
}

// digestRecordDifference names the snapshot records whose hashes disagree, which is
// what turns "the status surface differs" into something to read. Empty until both
// sides carry the breakdown, which starts one sample after the first disagreement;
// a name present on one side only is reported with the side that has it.
func digestRecordDifference(local, remote stateDigest) string {
	if len(local.Groups) == 0 || len(remote.Groups) == 0 {
		return ""
	}
	names := make([]string, 0, 4)
	for name, hash := range local.Groups {
		if other, ok := remote.Groups[name]; !ok {
			names = append(names, name+"(local only)")
		} else if other != hash {
			names = append(names, name)
		}
	}
	for name := range remote.Groups {
		if _, ok := local.Groups[name]; !ok {
			names = append(names, name+"(peer only)")
		}
	}
	slices.Sort(names)
	if len(names) > parameter.NetworkDivergedRecordsLogged {
		names = append(names[:parameter.NetworkDivergedRecordsLogged],
			"+"+strconv.Itoa(len(names)-parameter.NetworkDivergedRecordsLogged)+" more")
	}
	return strings.Join(names, ",")
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

// anyPeerMismatching reports whether any peer's last sample disagreed, which is
// what turns on the per-record breakdown.
func (s *NetworkSystem) anyPeerMismatching() bool {
	for peer := 1; peer < len(s.peerMismatches); peer++ {
		if s.peerMismatches[peer] > 0 {
			return true
		}
	}
	return false
}

func (s *NetworkSystem) anyPeerDesynced() bool {
	for peer := 1; peer < len(s.peerDesync); peer++ {
		if s.peerDesync[peer] {
			return true
		}
	}
	return false
}

// scheduleCrossings buffers one peer epoch and passes it on. Application waits for
// the artifact's own target tick, which travels with it, so a relayed epoch applies
// at the same tick however many links it crossed to arrive.
//
// The session is a mesh of links, not a star with an authority: a participant sends
// only to the peers it dialled or accepted, so an artifact reaches everyone else by
// being forwarded. Every node therefore floods each epoch it has not seen to every
// link except the one it arrived on. What terminates the flood is the per-source
// epoch window: a second copy arriving by another path is recognised and neither
// applied nor forwarded again, so each node handles each epoch exactly once whatever
// the topology. The hop limit is a backstop, not the termination argument.
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
	if admitted {
		for _, f := range batch.Frames {
			s.scheduled = append(s.scheduled, barrierArtifact{
				frame: f.Frame, applyTick: f.ApplyTick, source: batch.Source, origin: event.OriginNetwork,
			})
		}
	}
	s.mu.Unlock()

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
		if a.applyTick < nextTick {
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
	// Sources are participants, not links: in a mesh most of them arrive relayed, so
	// the count this instance should hear from is the remote half of the roster
	// rather than the number of peers it happens to be connected to.
	if required != 0 && (seen < s.remoteParticipants() || lag != 0) {
		s.statRanWithout.Add(1)
	}
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
