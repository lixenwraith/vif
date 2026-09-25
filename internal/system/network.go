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
	mu             sync.Mutex
	crossings      []event.ScheduledWireFrame
	scheduled      []barrierArtifact
	scheduledBytes int
	epochs         [participantSlots]epochWindow
	// rawEpochs dedupes the raw epochs this instance only passes on toward the
	// authority; the committed copy shares their key and must still be admitted.
	rawEpochs       [participantSlots]epochWindow
	productionEpoch uint64
	crossSeq        uint64
	// appliedCrossSeq is the contiguous source-local sequence through which every
	// ordinary local crossing has completed dispatch. Barrier-bound sequences are
	// closed when scheduled because snapshots continue to classify those by their
	// agreed ApplyTick. appliedAhead holds completions that raced an earlier
	// publisher; a capture may expose only the contiguous prefix.
	appliedCrossSeq uint64
	appliedAhead    map[uint64]struct{}
	localSource     uint32
	delayTicks      uint64

	// lastApplyTick is the newest apply tick this source has assigned. A shrinking
	// lead would otherwise number the next artifact below one already scheduled,
	// and a receiver orders by (ApplyTick, Source, Seq) — so two of this source's
	// crossings would arrive in the other order. Holding the number until the
	// production epoch catches it decays the lead a tick per tick instead.
	lastApplyTick uint64
	encodeErr     int64
	barrierActive atomic.Bool

	// appliedPeerSeq is the highest ordinary sequence this instance has applied from
	// each remote source, and the receive-side half of the fence a capture carries.
	// The local source's half is appliedCrossSeq above, which is a contiguous prefix
	// because local dispatch can complete out of order; this one is a maximum,
	// because a link delivers one source's frames in order and a frame this instance
	// refused is one it will never see again — so the highest it applied is the
	// honest statement of what it has dealt with.
	appliedPeerSeq [participantSlots]uint64

	// snapshotFloor is the tick of the last world this instance installed, and
	// snapshotFences what that world contained from each participant. For a
	// barrier-bound frame ApplyTick decides membership — the tick is exact. For an
	// ordinary crossing the authority may have applied late, so a world can miss one
	// whose ApplyTick is already past; the sequence answers.
	// Zero on an instance that never installed a capture.
	snapshotFloor     uint64
	snapshotAuthority uint32
	snapshotFences    network.CrossingFences

	buf [parameter.NetworkDrainWindow]network.Inbound // per-tick drain window

	syncSeq  uint64
	lastSync [parameter.MaxPlayers]uint64 // last applied sync per slot, for reordering
	// states are owner-authored syncs waiting for the tick the authority committed
	// them to, and stateSeen the newest sequence scheduled per slot, which is what
	// stops a flood from scheduling one twice.
	states       []pendingState
	stateSeen    [parameter.MaxPlayers]uint64
	rawStateSeen [parameter.MaxPlayers]uint64
	departed     [participantSlots]bool // participants already announced or noticed
	ticks        uint64
	statSent     *atomic.Int64
	statRecv     *atomic.Int64
	statState    *atomic.Int64
	statDrop     *atomic.Int64

	statDeferred       *atomic.Int64
	statAppliedLocal   *atomic.Int64
	statAppliedPeer    *atomic.Int64
	statLate           *atomic.Int64
	statDelayTicks     *atomic.Int64
	statRanWithout     *atomic.Int64
	statPeerLag        *atomic.Int64
	statPeerArtifacts  *atomic.Int64
	statPeerApplied    *atomic.Bool
	statPeers          *atomic.Int64
	statConnected      *atomic.Bool
	statConnection     *status.AtomicString
	statMapLatched     *atomic.Bool
	statHostLost       *atomic.Bool
	statLostIn         *atomic.Int64
	statLostOut        *atomic.Int64
	statRelayed        *atomic.Int64
	statDuplicates     *atomic.Int64
	statDigestMismatch *atomic.Int64

	digestHistory  [parameter.NetworkEpochWindow]stateDigest
	pendingDigest  [participantSlots]stateDigest
	statDriftPart  *status.AtomicString
	statDriftTick  *atomic.Int64
	statPreInstall *atomic.Int64
	statSuperseded *atomic.Int64

	// statRefusedTick and statScheduleFull count what the forward window and the
	// schedule's ceilings turned away. They are separate from statDrop because a
	// drop is a frame this instance could not read and these are frames it read and
	// refused: a non-zero pair names a peer whose ticks or whose volume are not
	// those of a participant, which is a different diagnosis from a decode failure.
	statRefusedTick    *atomic.Int64
	statScheduleFull   *atomic.Int64
	statAgreedRestored *atomic.Int64
	statJoinLag        *atomic.Int64
	statLag            *atomic.Int64
	statStale          *atomic.Bool
	statCorrections    *atomic.Int64
	statForged         *atomic.Int64

	// The authority's commit: crossings that reached it late (and the part of those
	// too late to apply), and on a guest, raw epochs refused for lacking a commit.
	statCommitLate  *atomic.Int64
	statCommitVoid  *atomic.Int64
	statUncommitted *atomic.Int64

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

	// snapshot reassembles the authority's correction, which is the only message
	// whose size is a function of the world and so the only one that arrives in
	// pieces. One per node rather than one per peer: the flood terminates because a
	// node relays only chunks it admitted, and an assembly per peer admits the same
	// chunk once per path instead, which is a cycle the mesh closes.
	snapshot network.SnapshotAssembly

	// suffix is the bounded ring of this instance's own ordinary crossings, kept so
	// a projection over a capture that predates them can re-apply them at their
	// ticks. Barrier-bound artifacts are absent: they decide what the world *is*,
	// and are retained separately in applied once they apply.
	//
	// lostSeq is the newest source-local sequence retention has dropped. A replay is
	// offered only when it lies at or before the correction's fence, because a
	// suffix missing one of its records is not a shorter suffix — it is a different
	// history, and this never guesses at one.
	suffix        []localCrossing
	suffixBytes   int
	lostSeq       uint64
	suffixDropped int64

	// applied is the bounded set of peer and barrier-bound artifacts this instance
	// has applied that a correction may still predate. The suffix above answers
	// "what did this participant do that the authority has not described yet"; this
	// answers it for everything else this instance applied, which nothing replays.
	applied []barrierArtifact

	// paceSamples holds the newest arrival offsets of the authority's epochs, and
	// paceCount how many are live; see observeAuthorityEpoch.
	paceSamples [parameter.NetworkPaceWindow]int64
	paceCount   int
	paceLatest  int64
	paceKnown   bool

	// leadLowSince and leadPushed are driveOwnLead's hysteresis: when a lower
	// target first held, and the change already pushed and not yet dispatched.
	leadLowSince  uint64
	leadPushed    uint64
	statPaceLate  *atomic.Int64
	statPaceTrim  *atomic.Int64
	statPaceSteps *atomic.Int64

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

// localCrossing is one of this instance's own crossings, retained in the wire
// representation it was already encoded into.
//
// Reusing event.ScheduledWireFrame is the point rather than an economy: the frame
// is what the host will apply, the journal writes the same payload text, and a
// replay that decoded a second representation could differ from both. The apply
// tick beside it is the session's agreed instant for the artifact, which is what
// decides whether a correction already contains it.
type localCrossing struct {
	frame event.ScheduledWireFrame

	// produced is the tick this instance published the artifact on — the epoch it
	// belongs to, not the tick the session applies it at. It bounds retention age;
	// the frame's source-local sequence, compared against the capture's fence for
	// this source, decides whether a particular correction already contains it.
	produced uint64
	origin   event.Origin
	bytes    int
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

// has reports whether tick was admitted, without admitting it.
func (w *epochWindow) has(tick uint64) bool {
	switch {
	case tick == 0 || w.high == 0 || tick > w.high:
		return false
	case tick == w.high:
		return true
	}
	behind := w.high - tick
	return behind <= parameter.NetworkEpochWindow && w.seen&(uint64(1)<<(behind-1)) != 0
}

// newest reports the highest epoch admitted from this source, for the playout lag
// telemetry that asks how far behind a peer's production is.
func (w *epochWindow) newest() uint64 { return w.high }

// participantSlots sizes every array this system indexes by participant identity.
// The coordinator hands out 1..MaxPlayers+1 and never zero, so one name rather than
// a size at each declaration: they used to disagree by one, and the identity at the
// top of the range had its epochs, corrections, digests and departure silently
// dropped by whichever array was short.
const participantSlots = parameter.MaxPlayers + 2

// barrierArtifact is one encoded local or peer crossing waiting for its apply tick.
type barrierArtifact struct {
	frame     event.WireFrame
	applyTick uint64
	source    uint32
	origin    event.Origin
	// void marks a crossing the authority refused as too late: it applies nothing
	// and only closes its source's fence, so its producer stops replaying it.
	void bool
}

// frameBytes is what one artifact costs a bounded buffer. The schedule and the
// replay suffix measure the same thing the same way, so their two ceilings stay
// comparable rather than drifting into two ideas of a frame's size.
func frameBytes(f event.WireFrame) int {
	return len(f.Payload) + len(f.Event) + len(f.Domain)
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
	s.statDelayTicks = s.intStat(reg, "network.barrier_delay_ticks")
	s.statRanWithout = s.intStat(reg, "network.barrier_ran_without_peer")
	s.statPeerLag = s.intStat(reg, "network.barrier_peer_lag_ticks")
	s.statPeerArtifacts = s.intStat(reg, "network.barrier_peer_artifacts")
	s.statPeerApplied = s.boolStat(reg, "network.barrier_peer_applied")
	s.statPeers = s.intStat(reg, "network.peers")
	s.statConnected = s.boolStat(reg, "network.connected")
	s.statConnection = s.textStat(reg, "network.state")
	s.statMapLatched = s.boolStat(reg, "network.map_latched")
	// Host loss is a run-level fact, not a transient session counter. The guest
	// continues simulating its local fork and a game reset must not make that loss
	// disappear from the player-facing status surface.
	s.statHostLost = reg.Bools.Get("network.host_lost")
	s.statLostIn = s.intStat(reg, "network.transport_lost_in")
	s.statLostOut = s.intStat(reg, "network.transport_lost_out")
	s.statRelayed = s.intStat(reg, "network.relay_forwarded")
	s.statDuplicates = s.intStat(reg, "network.relay_duplicates")
	s.statDigestMismatch = s.intStat(reg, "network.digest_mismatches")
	s.statDriftPart = s.textStat(reg, "network.drift_part")
	s.statDriftTick = s.intStat(reg, "network.drift_tick")
	s.statRefusedTick = s.intStat(reg, "network.artifacts_refused_tick")
	s.statScheduleFull = s.intStat(reg, "network.artifacts_schedule_full")
	s.statPreInstall = s.intStat(reg, "network.artifacts_pre_install")
	s.statSuperseded = s.intStat(reg, "network.artifacts_authority_superseded")
	s.statAgreedRestored = s.intStat(reg, "network.artifacts_restored")
	s.statJoinLag = s.intStat(reg, "network.join_lag_ticks")
	s.statLag = s.intStat(reg, "network.lag_ticks")
	s.statStale = s.boolStat(reg, "network.stale")
	s.statCorrections = s.intStat(reg, "network.corrections_received")
	s.statForged = s.intStat(reg, "network.artifacts_refused")
	s.statCommitLate = s.intStat(reg, "network.commit_late")
	s.statCommitVoid = s.intStat(reg, "network.commit_void")
	s.statUncommitted = s.intStat(reg, "network.commit_refused_raw")
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
	s.statPaceLate = s.intStat(reg, "network.pace_late_ticks")
	s.statPaceTrim = s.intStat(reg, "network.pace_trim_permille")
	s.statPaceSteps = s.intStat(reg, "network.pace_steps")

	s.Init()
	return s
}

// Init resets the barrier and leaves its sink installed as a no-op without peers.
func (s *NetworkSystem) Init() {
	s.enabled = true
	s.ticks = 0
	s.syncSeq = 0
	s.lastSync = [parameter.MaxPlayers]uint64{}
	s.states, s.stateSeen, s.rawStateSeen = s.states[:0], [parameter.MaxPlayers]uint64{}, [parameter.MaxPlayers]uint64{}
	s.departed = [participantSlots]bool{}

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
	s.paceCount, s.paceLatest, s.paceKnown = 0, 0, false
	s.leadLowSince, s.leadPushed = 0, 0
	s.digestHistory = [parameter.NetworkEpochWindow]stateDigest{}
	s.pendingDigest = [participantSlots]stateDigest{}
	s.snapshot = network.SnapshotAssembly{}

	// Held derivations go for the reason the replay suffix below does: a reward the
	// authority never proved belongs to the run that predicted it, and that run has
	// been replaced. Outside the barrier lock, which the ledger does not share.
	s.world.ResetPredictedDeaths()

	s.mu.Lock()
	s.crossings = s.crossings[:0]
	s.scheduled = s.scheduled[:0]
	s.scheduledBytes = 0
	s.applied = s.applied[:0]
	// The replay suffix goes with them, and it has to: a reset restarts crossSeq at
	// zero, so a record retained from before it carries a sequence a post-reset
	// fence would compare against and get wrong in both directions. Under the old
	// tick boundary the first correction pruned these by age; a sequence boundary
	// has no such accident to rely on.
	s.suffix = s.suffix[:0]
	s.suffixBytes = 0
	s.suffixDropped = 0
	s.lostSeq = 0
	s.epochs = [participantSlots]epochWindow{}
	s.rawEpochs = [participantSlots]epochWindow{}
	s.snapshotFloor = 0
	s.snapshotAuthority = 0
	s.snapshotFences = nil
	s.appliedPeerSeq = [participantSlots]uint64{}
	s.productionEpoch = s.world.Resources.Game.State.GetGameTicks() + 1
	s.lastApplyTick = 0
	s.crossSeq = 0
	s.appliedCrossSeq = 0
	s.appliedAhead = make(map[uint64]struct{})
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
		event.EventPlayoutLead,
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
	case event.EventPlayoutLead:
		if p, ok := ev.Payload.(*event.PlayoutLeadPayload); ok {
			s.adoptDelay(p.Ticks)
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
	s.lastSync[p.Slot], s.stateSeen[p.Slot], s.rawStateSeen[p.Slot] = 0, 0, 0
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

// localTick is this instance's own simulation position, which is what an arriving
// apply tick is judged against. It is read rather than passed in because the two
// drain paths differ on whether a tick is in progress and the answer must not.
func (s *NetworkSystem) localTick() uint64 {
	return s.world.Resources.Event.Queue.Stamp().Tick
}

// adoptDelay installs this instance's own lead at the tick its journaled event
// dispatches, so a reproduction switches on the same tick. The resource is the
// single copy refreshLink re-reads. Caller holds the world lock.
func (s *NetworkSystem) adoptDelay(ticks uint64) {
	ticks = min(max(ticks, parameter.NetworkBarrierMinDelayTicks), parameter.NetworkBarrierMaxDelayTicks)
	r := s.world.Resources.Network
	if r == nil || r.BarrierDelayTicks == ticks {
		return
	}
	r.BarrierDelayTicks = ticks
	s.mu.Lock()
	s.delayTicks = ticks
	s.mu.Unlock()
	s.statDelayTicks.Store(int64(ticks))
	vlog.Info("net", "msg", "playout lead adopted", "ticks", ticks, "tick", s.localTick())
}

// barrierDelayTicks returns this instance's own lead.
func (s *NetworkSystem) barrierDelayTicks() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delayTicks
}

// removeParticipant applies the departure crossing. It arrives at the same apply
// tick on every instance, so the despawn it derives does too (D-5) — which is the
// whole reason a disconnect is not acted on where it is observed.
func (s *NetworkSystem) removeParticipant(p *event.ParticipantDepartedPayload) {
	s.forgetParticipant(p.Participant)
	if int(p.Participant) < len(s.departed) {
		s.departed[p.Participant] = true
	}
	if int(p.Slot) >= parameter.MaxPlayers {
		return
	}
	cursor := s.world.Resources.Player.Slot(p.Slot)
	if cursor == 0 || s.world.SimulatesLocally(cursor) {
		return
	}
	s.world.PushEvent(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: p.Slot})
	s.lastSync[p.Slot], s.stateSeen[p.Slot], s.rawStateSeen[p.Slot] = 0, 0, 0
}

// forgetParticipant clears a departed identity for whoever takes it next, whose
// sequence starts at one: its fence, and the ordinary crossings of its still
// scheduled or retained. Applied after the departure, one of those re-raised the
// fence and the next holder's first crossings read as applied everywhere (§3.2).
// Committed copies precede the departure on every link, so all instances drop alike.
func (s *NetworkSystem) forgetParticipant(participant uint32) {
	if participant == 0 || int(participant) >= len(s.appliedPeerSeq) {
		return
	}
	ordinary := func(a barrierArtifact) bool {
		et, ok := event.GetEventType(a.frame.Event)
		return a.source == participant && (!ok || !barrierBound(et))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appliedPeerSeq[participant] = 0
	kept := make(network.CrossingFences, 0, len(s.snapshotFences)) // shared with the capture header
	for _, f := range s.snapshotFences {
		if f.Source != network.PeerID(participant) {
			kept = append(kept, f)
		}
	}
	s.snapshotFences = kept
	s.scheduled = slices.DeleteFunc(s.scheduled, ordinary)
	s.scheduledBytes = 0
	for _, a := range s.scheduled {
		s.scheduledBytes += frameBytes(a.frame)
	}
	s.applied = slices.DeleteFunc(s.applied, ordinary)
}

// Cross encodes and schedules a crossing when a peer is live, taking the event: the
// local copy is applied by the barrier at the artifact's agreed tick, in the order
// every other instance applies it, and the pooled original is released after
// encoding. There is no producer copy published sooner (multi-player.md §3.1).
func (s *NetworkSystem) Cross(ev event.GameEvent) (taken bool) {
	if !s.barrierActive.Load() {
		return false
	}
	s.mu.Lock()
	// Named before it is encoded, so this instance's own copy and every peer's
	// carry one identity: a payload whose shared outcome needs a value no receiver
	// can re-derive takes it from the artifact rather than from a stream position
	// the two consume at different ticks. The sequence is given back if the encode
	// fails: a number that is never dispatched would stall the applied prefix.
	s.crossSeq++
	event.StampCrossing(ev.Payload, s.localSource, s.crossSeq)
	frame, encErr := event.NewWireFrame(ev)
	if encErr != "" {
		s.crossSeq--
		s.encodeErr++
		s.mu.Unlock()
		event.ReleaseDeferredPayload(ev.Payload)
		return true
	}
	frame.Seq = s.crossSeq
	applyTick := max(s.productionEpoch+s.delayTicks, s.lastApplyTick)
	s.lastApplyTick = applyTick
	s.crossings = append(s.crossings, event.ScheduledWireFrame{Frame: frame, ApplyTick: applyTick})
	// Counted, never capped. The cap is on what a peer can make this instance hold;
	// this artifact is this instance's own and has already been broadcast, so
	// refusing it here would be a divergence rather than a defence.
	s.scheduled = append(s.scheduled, barrierArtifact{
		frame: frame, applyTick: applyTick, source: s.localSource, origin: ev.Origin,
	})
	s.scheduledBytes += frameBytes(frame)
	if barrierBound(ev.Type) {
		// A barrier-bound sequence does not participate in the authority's
		// local-first capture fence. Close its position now so it cannot leave a
		// permanent hole in the ordinary sequence prefix; its effect remains
		// classified by ApplyTick on every instance.
		s.closeAppliedCrossingLocked(frame.Seq)
	} else {
		// Retained for the projection a correction runs, at the agreed tick.
		s.retainLocked(frame, s.productionEpoch, applyTick, ev.Origin)
	}
	s.mu.Unlock()
	event.ReleaseDeferredPayload(ev.Payload)
	s.statDeferred.Add(1)
	return true
}

// DropUncommitted forgets this instance's own crossings when a handoff moves
// authorship: the authority they were sent to is gone and its successor never had
// them. What already applied here the next correction takes back; the sequences
// dropped unapplied are closed so the local fence does not stall on them.
// Caller MUST hold updateMutex.
func (s *NetworkSystem) DropUncommitted() {
	s.mu.Lock()
	kept, bytes, dropped := s.scheduled[:0], 0, len(s.suffix)
	for _, a := range s.scheduled {
		if a.source == s.localSource {
			s.closeAppliedCrossingLocked(a.frame.Seq)
			dropped++
			continue
		}
		kept = append(kept, a)
		bytes += frameBytes(a.frame)
	}
	s.scheduled, s.scheduledBytes = kept, bytes
	s.suffix, s.suffixBytes = s.suffix[:0], 0
	s.mu.Unlock()
	s.world.Resources.Player.DropPrediction()
	if dropped > 0 {
		s.statDrop.Add(int64(dropped))
		vlog.Info("app", "msg", "uncommitted crossings dropped at handoff", "crossings", dropped)
	}
}

// CrossingApplied closes the source-local sequence of an ordinary crossing once
// its local copy has run through every handler. A correction capture reads only
// the contiguous prefix: a producer that assigned an earlier sequence but has
// not published it yet cannot make the capture claim that event's effect.
func (s *NetworkSystem) CrossingApplied(sequence uint64) {
	if sequence == 0 {
		return
	}
	s.mu.Lock()
	s.closeAppliedCrossingLocked(sequence)
	s.mu.Unlock()
}

// closeAppliedCrossingLocked advances the contiguous applied prefix, retaining
// completions that arrived ahead of a lower sequence until the gap closes.
// Caller MUST hold mu.
func (s *NetworkSystem) closeAppliedCrossingLocked(sequence uint64) {
	if sequence <= s.appliedCrossSeq {
		return
	}
	if sequence != s.appliedCrossSeq+1 {
		if s.appliedAhead == nil {
			s.appliedAhead = make(map[uint64]struct{})
		}
		s.appliedAhead[sequence] = struct{}{}
		return
	}
	s.appliedCrossSeq = sequence
	for {
		next := s.appliedCrossSeq + 1
		if _, ok := s.appliedAhead[next]; !ok {
			return
		}
		delete(s.appliedAhead, next)
		s.appliedCrossSeq = next
	}
}

// noteAppliedFrom advances one remote source's applied boundary.
//
// A maximum rather than a contiguous prefix. Within one link a source's frames
// arrive in order, so the highest applied is the whole of what arrived; a frame this
// instance refused for a full queue is one it will never see again, and stalling the
// fence on it would make that producer replay a growing suffix at every correction
// for the rest of the session. The cost of the maximum is the opposite and much
// smaller: a frame that overtakes a lower one across a relay leaves the lower one
// looking contained for one cadence, which the next correction repairs.
func (s *NetworkSystem) noteAppliedFrom(source uint32, seq uint64) {
	if seq == 0 || source == 0 || int(source) >= len(s.appliedPeerSeq) {
		return
	}
	s.mu.Lock()
	if seq > s.appliedPeerSeq[source] {
		s.appliedPeerSeq[source] = seq
	}
	s.mu.Unlock()
}

// AppliedCrossingFences returns, for every participant, the sequence through which
// a capture of the current world contains that participant's ordinary crossings.
//
// The local entry is the contiguous prefix its own dispatchers have completed; the
// rest are the highest this instance has applied. It is read under the world lock by
// CaptureShared, so no completed dispatcher and no applied peer frame can move the
// world without moving its fence with it.
func (s *NetworkSystem) AppliedCrossingFences() network.CrossingFences {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(network.CrossingFences, 0, len(s.appliedPeerSeq))
	for source, seq := range s.appliedPeerSeq {
		if seq != 0 {
			out = append(out, network.CrossingFence{Source: network.PeerID(source), Seq: seq})
		}
	}
	if s.localSource != 0 && s.appliedCrossSeq != 0 {
		out = append(out, network.CrossingFence{
			Source: network.PeerID(s.localSource), Seq: s.appliedCrossSeq,
		})
	}
	return out.Normalize()
}

// barrierBound names the artifacts that apply at one agreed tick on every instance,
// the producer included: the ones deciding what the world *is* — roster, run, shared
// identity, the progression a region gates spawns on, and the defeat that rebuilds
// the level — not what happens in a world both already hold, whose gap a correction
// repairs. Nobody's input waits on them. See multi-player.md §3.1.
func barrierBound(et event.EventType) bool {
	switch et {
	case event.EventParticipantJoined, event.EventParticipantDeparted, event.EventGameResetRequest,
		event.EventSwarmSpawnRequest, event.EventQuasarSpawnRequest, event.EventDrainDefeated,
		event.EventCursorDefeatState:
		return true
	default:
		return false
	}
}

// retainLocked adds one of this instance's own crossings to the replay suffix,
// dropping the oldest whenever any of the three bounds is exceeded.
//
// All three bounds are needed and each answers a different failure. The tick span
// keeps the suffix inside the window a correction can rebase onto — past the
// convergence floor a whole world has arrived and there is nothing older to
// preserve. The record count bounds a participant producing faster than the
// cadence. The byte bound is what stops a pathological payload from turning a
// bounded ring into an unbounded buffer.
//
// A dropped record raises lostSeq rather than merely shortening the ring, because a
// suffix with a hole is not a shorter suffix — it is a different history.
// LocalReplaySuffix refuses to offer one.
//
// Caller MUST hold mu.
func (s *NetworkSystem) retainLocked(frame event.WireFrame, produced, applyTick uint64, origin event.Origin) {
	rec := localCrossing{
		frame:    event.ScheduledWireFrame{Frame: frame, ApplyTick: applyTick},
		produced: produced,
		origin:   origin,
		bytes:    len(frame.Payload) + len(frame.Event) + len(frame.Domain),
	}
	s.suffix = append(s.suffix, rec)
	s.suffixBytes += rec.bytes

	drop := func() {
		if len(s.suffix) == 0 {
			return
		}
		old := s.suffix[0]
		if old.frame.Frame.Seq > s.lostSeq {
			s.lostSeq = old.frame.Frame.Seq
		}
		s.suffixBytes -= old.bytes
		s.suffix = append(s.suffix[:0], s.suffix[1:]...)
		s.suffixDropped++
	}
	horizon := uint64(0)
	if produced > parameter.SnapshotReplayTicks {
		horizon = produced - parameter.SnapshotReplayTicks
	}
	for len(s.suffix) > 0 && s.suffix[0].produced < horizon {
		drop()
	}
	for len(s.suffix) > parameter.SnapshotReplayRecords {
		drop()
	}
	for s.suffixBytes > parameter.SnapshotReplayBytes && len(s.suffix) > 1 {
		drop()
	}
}

// retainApplied keeps one applied peer or barrier-bound artifact, so a projection
// over a capture that lacks it can re-apply it. Bounded by age and by count, like
// the local suffix; a peer frame the next capture holds is pruned by its fence.
func (s *NetworkSystem) retainApplied(a barrierArtifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, a)
	kept := s.applied[:0]
	for _, r := range s.applied {
		if r.applyTick+parameter.SnapshotReplayTicks >= a.applyTick {
			kept = append(kept, r)
		}
	}
	s.applied = kept
	if n := len(s.applied); n > parameter.SnapshotReplayRecords {
		s.applied = append(s.applied[:0], s.applied[n-parameter.SnapshotReplayRecords:]...)
	}
}

// RetainedAfter returns the applied artifacts a capture at tick with these fences
// lacks, in session order: barrier-bound ones due after its tick, and peer frames
// past their source's fence. A projection re-applies each at its own tick.
func (s *NetworkSystem) RetainedAfter(tick uint64, fences network.CrossingFences) (frames []event.ScheduledWireFrame, sources []uint32, origins []event.Origin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.applied {
		if s.retainedContainedLocked(a, tick, fences) {
			continue
		}
		frames = append(frames, event.ScheduledWireFrame{Frame: a.frame, ApplyTick: a.applyTick})
		sources = append(sources, a.source)
		origins = append(origins, a.origin)
	}
	return frames, sources, origins
}

// retainedContainedLocked is snapshotContainsLocked against a capture being
// projected rather than the one last installed. Caller MUST hold mu.
func (s *NetworkSystem) retainedContainedLocked(a barrierArtifact, tick uint64, fences network.CrossingFences) bool {
	if et, ok := event.GetEventType(a.frame.Event); ok && !barrierBound(et) {
		fence := fences.Seq(network.PeerID(a.source))
		return fence != 0 && a.frame.Seq <= fence
	}
	return a.applyTick <= tick
}

// ScheduleReplay puts artifacts another instance retained on this barrier's
// schedule, for a projection world to apply at their ticks. One due at or before
// the next tick applies then: a record the capture missed because it reached the
// authority late is re-applied late, exactly as the authority did.
func (s *NetworkSystem) ScheduleReplay(frames []event.ScheduledWireFrame, sources []uint32, origins []event.Origin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range frames {
		a := barrierArtifact{frame: f.Frame, applyTick: f.ApplyTick, source: sources[i], origin: origins[i]}
		s.scheduled = append(s.scheduled, a)
		s.scheduledBytes += frameBytes(f.Frame)
	}
}

// LocalReplaySuffix returns this instance's own crossings that the installed world
// does not contain, in session application order.
//
// The boundary is the capture's fence for this source, not its tick. A copy whose
// apply tick is already past can still be missing from a capture read before it
// reached the authority; judging by tick discarded exactly those, which is how a
// guest's own action came to vanish for one cadence whenever its link missed the
// lead. Production ticks bound the retained window; they do not decide membership.
//
// ok is false when retention has dropped a record the suffix would need. The
// caller then installs the authority alone and reports that replay was skipped;
// there is no partial answer, because a partial replay is a guess about what the
// participant did.
func (s *NetworkSystem) LocalReplaySuffix(fence uint64) ([]event.ScheduledWireFrame, []event.Origin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lostSeq > fence {
		return nil, nil, false
	}
	// A record whose scheduled copy has not applied yet is not missing from the
	// world, it is pending in it; the schedule applies it at its tick.
	pending := make(map[uint64]struct{}, len(s.scheduled))
	for _, a := range s.scheduled {
		if a.source == s.localSource {
			pending[a.frame.Seq] = struct{}{}
		}
	}
	selected := make([]localCrossing, 0, len(s.suffix))
	for _, rec := range s.suffix {
		if rec.frame.Frame.Seq != 0 && rec.frame.Frame.Seq <= fence {
			continue
		}
		if _, ok := pending[rec.frame.Frame.Seq]; ok {
			continue
		}
		selected = append(selected, rec)
	}
	slices.SortStableFunc(selected, func(a, b localCrossing) int {
		if n := cmp.Compare(a.frame.ApplyTick, b.frame.ApplyTick); n != 0 {
			return n
		}
		return cmp.Compare(a.frame.Frame.Seq, b.frame.Frame.Seq)
	})
	frames := make([]event.ScheduledWireFrame, len(selected))
	origins := make([]event.Origin, len(selected))
	for i, rec := range selected {
		frames[i], origins[i] = rec.frame, rec.origin
	}
	return frames, origins, true
}

// ReplaySuffixSize is how many records this instance is currently retaining, and
// how many retention has dropped, for the telemetry the plan asks for.
func (s *NetworkSystem) ReplaySuffixSize() (retained int, dropped int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.suffix), s.suffixDropped
}

// refreshLink re-reads the negotiated session identity and reports whether the
// barrier owns crossings this tick. Every entry point the system has — the startup
// gate, the update pass, tick open and tick close — needs the same answer, and the
// endpoint may be attached or lost between any two of them.
func (s *NetworkSystem) refreshLink(p engine.NetworkPort) bool {
	// The barrier belongs to the run, not to the link: a stretch with no peer — a
	// lobby, an emptied session, a replay — still stamps by the lead it last adopted,
	// because the tick an artifact applies at is what a reproduction has to reach.
	active := s.enabled && (s.world.SessionBarrier() ||
		(p != nil && p.IsRunning() && p.PeerCount() > 0))
	if r := s.world.Resources.Network; r != nil {
		s.mu.Lock()
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
		s.mu.Unlock()
	}
	s.barrierActive.Store(active)
	s.statDelayTicks.Store(int64(s.barrierDelayTicks()))
	return active
}

// AdoptSnapshot rebases the barrier onto a world this instance just installed.
//
// Three clocks move here, but only two may move backwards. The world and its
// scheduled artifacts rebase to the installed tick. This source's production
// epoch does not: peers use it as a monotonic replay key and would reject a new
// batch sent under an epoch they admitted before the correction. A rewound source
// therefore keeps its next unsent epoch and buffers new crossings until the world
// catches it. Barrier-bound artifacts already due at or before the installed tick
// are dropped, because every instance applies one at the same tick. Ordinary
// crossings are dropped by their source's sequence fence instead: an apply tick
// says when a copy was scheduled to run, and the fence says what the captured world
// actually contains. Both boundaries are remembered so an artifact still in flight
// is classified the same way when it arrives.
//
// Caller MUST hold updateMutex: it runs inside the install.
func (s *NetworkSystem) AdoptSnapshot(tick uint64, authority uint32, fences network.CrossingFences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotFloor = tick
	s.snapshotAuthority = authority
	s.snapshotFences = fences
	localFence := fences.Seq(network.PeerID(s.localSource))
	// A local crossing may have been accepted between the last tick close and this
	// install. One the captured world does not contain is still outstanding, and
	// dropping it would leave only a local replay for a later correction to erase
	// permanently — which is the whole failure this fence exists to prevent.
	pending := s.crossings[:0]
	pendingDropped := 0
	for _, f := range s.crossings {
		if f.Frame.Seq != 0 && f.Frame.Seq <= localFence {
			pendingDropped++
			continue
		}
		pending = append(pending, f)
	}
	s.crossings = pending
	if pendingDropped > 0 {
		s.statDrop.Add(int64(pendingDropped))
	}
	keep := s.scheduled[:0]
	dropped, superseded := 0, 0
	held := 0
	for _, a := range s.scheduled {
		contained, bySequence := s.snapshotContainsLocked(a)
		if contained {
			dropped++
			if bySequence {
				superseded++
			}
			continue
		}
		keep = append(keep, a)
		held += frameBytes(a.frame)
	}
	s.scheduled = keep
	s.scheduledBytes = held
	// A placement the capture proves applied is discarded here unannounced, so the
	// D-18 queue keeps only what this source still has to apply.
	moves := 0
	for _, a := range s.scheduled {
		if et, ok := event.GetEventType(a.frame.Event); ok && a.source == s.localSource && et == event.EventCursorMoveRequest {
			moves++
		}
	}
	s.world.Resources.Player.KeepPredictions(moves)
	// The retained local suffix is pruned by this instance's own fence, for the same
	// reason the schedule is: a record the captured world does not contain is still
	// this participant's action, whatever its apply tick says. Pruning by tick here
	// was the discard that made a guest's own action vanish for one cadence whenever
	// its link missed the playout lead.
	pruned := s.suffix[:0]
	s.suffixBytes = 0
	for _, rec := range s.suffix {
		if rec.frame.Frame.Seq != 0 && rec.frame.Frame.Seq <= localFence {
			continue
		}
		pruned = append(pruned, rec)
		s.suffixBytes += rec.bytes
	}
	s.suffix = pruned
	// Retention keeps what the installed world lacks. A barrier-bound artifact it
	// predates goes back on the schedule, since its consumed copy is the only one;
	// a peer's ordinary frame is kept for the next projection to re-apply.
	restored, retained := 0, s.applied[:0]
	for _, a := range s.applied {
		if s.retainedContainedLocked(a, tick, fences) {
			continue
		}
		retained = append(retained, a)
		if et, ok := event.GetEventType(a.frame.Event); ok && barrierBound(et) {
			restored++
			s.scheduled = append(s.scheduled, a)
			s.scheduledBytes += frameBytes(a.frame)
		}
	}
	s.applied = retained
	if restored > 0 {
		s.statAgreedRestored.Add(int64(restored))
	}
	// Production epochs are source-local replay keys, not captured world state.
	// Moving this high-water mark backwards makes the receiver discard a genuinely
	// new batch as a duplicate. A forward install can skip empty unsent epochs, but
	// an epoch holding a pending crossing must retain its original key and deadline;
	// flushCrossings closes it once, then catches the high-water mark up to the world.
	if len(s.crossings) == 0 {
		s.productionEpoch = max(s.productionEpoch, tick+1)
	}
	if r := s.world.Resources.Network; r != nil {
		s.localSource = r.ParticipantID
		s.delayTicks = r.BarrierDelayTicks
	}
	if dropped > 0 {
		s.statPreInstall.Add(int64(dropped))
	}
	if superseded > 0 {
		s.statSuperseded.Add(int64(superseded))
	}
	if dropped > 0 || pendingDropped > 0 {
		vlog.Debug("app", "msg", "snapshot pruned crossings",
			"tick", tick, "authority", authority, "local_fence", localFence,
			"fences", len(fences), "scheduled", dropped, "sequence_superseded", superseded,
			"pending_local", pendingDropped)
	}
}

// snapshotContainsLocked reports whether the last installed capture already
// contains an artifact, and whether a sequence fence (rather than the tick floor)
// proved it.
//
// Ordinary crossings are decided by their source's fence, whoever produced them. A
// frame the capturing instance had not applied is absent even when scheduler delay
// or a slow link made its nominal ApplyTick old, and a frame it had applied is
// present even when the receive-side ApplyTick is still in the future. Barrier-bound
// frames keep the agreed ApplyTick: every instance applies one at the same tick, so
// the tick is the exact answer for them.
//
// A capture that named no fence for a source claims nothing about it, so nothing is
// contained and the artifact is kept. That is the conservative direction — a
// duplicate is repaired by the next correction, a discarded action is not.
// Caller MUST hold mu.
func (s *NetworkSystem) snapshotContainsLocked(a barrierArtifact) (contained, bySequence bool) {
	if et, ok := event.GetEventType(a.frame.Event); ok && !barrierBound(et) {
		if fence := s.snapshotFences.Seq(network.PeerID(a.source)); fence != 0 {
			contained = a.frame.Seq <= fence
			return contained, contained
		}
		// No fence for this source. Falling through to the tick would reintroduce
		// exactly the misjudgement the fence exists to prevent, so an unnamed source
		// is treated as unrepresented.
		return false, false
	}
	return a.applyTick <= s.snapshotFloor, false
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
		// The slot is named beside the identity because they answer different
		// questions: a dedicated host holds peer identity 1 and no slot, so its
		// first guest is peer 2 and the session's only participant.
		vlog.Info("app", "msg", "network session active",
			"local", s.participantID(), "slot", s.world.Resources.Player.LocalSlot(),
			"coordinator", s.isCoordinator(),
			"barrier_delay_ticks", s.barrierDelayTicks(), "peers", peers)
	}
}

func (s *NetworkSystem) Update() {
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	// An instance nothing will correct any more owns what it predicted, so the
	// rewards it is holding are due. Cheap and silent while the ledger is empty,
	// which is every tick of a host and of a run with no session.
	s.world.SettlePredictedDeaths()
	if r := s.world.Resources.Network; r != nil && s.isCoordinator() {
		r.Pace.Store(0) // the authority is the clock the others pace against
	}
	if s.enabled {
		s.driveOwnLead(p)
	}
	if s.enabled && p != nil && p.IsRunning() {
		s.ticks++
	}
}

// Receive drains transport state, publishes every artifact due before nextTick
// and reports whether a session is live. Caller holds the world lock.
func (s *NetworkSystem) Receive(nextTick uint64) (live bool) {
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if s.enabled && p != nil {
		s.drain(p)
	}
	s.applyDue(nextTick)
	s.publishBarrierTelemetry(nextTick, p)
	return s.barrierActive.Load()
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
	if r := s.world.Resources.Network; r != nil && r.OnTickClosed != nil && s.enabled {
		r.OnTickClosed(completedTick)
	}
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
func (s *NetworkSystem) drain(p engine.NetworkPort) {
	s.drainWith(p, p.Drain)
}

// DrainOffTick translates what the endpoint already holds without the poll
// counting as a tick, so a manifest, request or repair is acted on at the tick it
// describes rather than at the next one.
//
// Caller MUST hold updateMutex.
func (s *NetworkSystem) DrainOffTick() {
	p := s.port()
	if p == nil || !s.enabled {
		return
	}
	s.refreshLink(p)
	poll := p.Drain
	if off, ok := p.(engine.OffTickDrainPort); ok {
		poll = off.DrainOffTick
	}
	s.drainWith(p, poll)
}

// drainWith is one poll of the endpoint, whichever door it came through.
func (s *NetworkSystem) drainWith(p engine.NetworkPort, poll func([]network.Inbound) int) {
	n := poll(s.buf[:])
	for i := range n {
		in := &s.buf[i]
		switch in.Kind {
		case network.InboundConnect:
			s.world.PushLocal(event.EventNetworkConnect, &event.NetworkConnectPayload{PeerID: uint32(in.Peer)})
		case network.InboundDisconnect:
			s.world.PushLocal(event.EventNetworkDisconnect, &event.NetworkDisconnectPayload{PeerID: uint32(in.Peer)})
			s.reportDisconnect(uint32(in.Peer), p.PeerCount())
			s.forgetDigestPeer(uint32(in.Peer))
			s.noticeDeparture(uint32(in.Peer))
		case network.InboundMessage:
			s.dispatchMessage(uint32(in.Peer), in.Msg)
		}
	}
}

// reportDisconnect names link loss when it happens. A digest cannot: once the edge
// is gone nothing on it sends a mismatching one, so the worst transport failure
// would look like silence. Losing the authority is called out separately on a
// guest, which continues from the last authoritative state — a fork rather than a
// session until a succession adopts a term.
func (s *NetworkSystem) reportDisconnect(peerID uint32, remaining int) {
	authorityLost := peerID == s.authorityParticipant() && !s.isCoordinator()
	message := fmt.Sprintf("Participant %d disconnected", peerID)
	if authorityLost {
		// The badge and the message are the fallback's, and the session layer may
		// clear both: a succession that finds an eligible successor ends with an
		// authority rather than a fork. What it may not do is leave the loss
		// unsaid while it tries, so the honest state is published first and
		// withdrawn only once a handoff has actually been adopted.
		s.statHostLost.Store(true)
		message = "Host connection lost; continuing locally from the last authoritative state"
	}
	// Every departure reaches the session layer, not only the authority's: a
	// survivor's own reach is what the succession rule counts, and it changes
	// whenever any link does.
	if r := s.world.Resources.Network; r != nil && r.OnPeerLost != nil {
		r.OnPeerLost(peerID)
	}
	s.world.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message: message, Duration: 4 * parameter.StatusMessageDefaultTimeout, DurationOverride: true,
	})
	vlog.Warn("app", "msg", "peer link lost", "peer", peerID,
		"authority_lost", authorityLost, "remaining_peers", remaining)
}

func (s *NetworkSystem) forgetDigestPeer(peer uint32) {
	if peer == 0 || int(peer) >= len(s.pendingDigest) {
		return
	}
	s.pendingDigest[peer] = stateDigest{}
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

// isCoordinator reports whether this instance is the one currently authoring the
// Shared world.
//
// It used to be "does this instance own the session's first identity", because
// that is the identity the handshake assigns to the host and the host authored
// for the life of the session. A handoff moves authorship, so the question moves
// with it: the roster artifacts only one producer may raise follow the authority,
// not the participant that opened the session.
func (s *NetworkSystem) isCoordinator() bool {
	s.mu.Lock()
	local := s.localSource
	s.mu.Unlock()
	return local != 0 && local == s.authorityParticipant()
}

// authorityParticipant is the participant currently authoring, defaulting to the
// session's first identity for a transport that carries no authority yet.
func (s *NetworkSystem) authorityParticipant() uint32 {
	if r := s.world.Resources.Network; r != nil {
		if id := r.Authority.Load(); id != 0 {
			return id
		}
	}
	return engine.CoordinatorParticipant
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
func (s *NetworkSystem) dispatchMessage(from uint32, msg *network.Message) {
	if msg == nil {
		return
	}
	switch msg.Type {
	case network.MsgEvent:
		s.scheduleCrossings(from, msg.Payload)
	case network.MsgStateSync:
		s.scheduleCursorState(from, msg.Payload)
	case network.MsgStateDigest:
		s.receiveStateDigest(from, msg.Payload)
	case network.MsgDisconnect:
		s.receiveDeparture(from, msg.Payload)
	case network.MsgStateCorrection:
		s.receiveCorrection(from, msg.Payload)
	case network.MsgStateManifest, network.MsgStateRequest, network.MsgStateShard,
		network.MsgStateUnserved:
		s.receiveSelective(msg.Type, from, msg.Payload)
	case network.MsgSessionRestart:
		s.receiveSessionRestart(from, msg.Payload)
	case network.MsgAuthorityReport, network.MsgAuthorityHandoff, network.MsgPeerList:
		// The address map travels with the succession it exists for: same term
		// gate, same flood, same session layer deduplicating by term and
		// participant. It says nothing about the world, so nothing here reads it.
		s.receiveAuthority(msg.Type, from, msg.Payload)
	default:
		s.statDrop.Add(1)
	}
}

// receiveCorrection reassembles one authoritative correction and hands the body to
// the session layer.
//
// Nothing here decodes or installs it. This runs inside the tick that drained the
// chunk, under the world lock a correction's install needs for itself, and a
// correction is hundreds of kilobytes of JSON — so what happens here is a copy into
// a queue and nothing else.
//
// A malformed transfer resets the assembly rather than poisoning it: the host sends
// a whole correction every SnapshotKeyframeCorrections and each one is
// self-sufficient, so the recovery from a broken transfer is to wait for the next.
func (s *NetworkSystem) receiveCorrection(from uint32, body []byte) {
	if from == 0 || int(from) >= participantSlots {
		s.statDrop.Add(1)
		return
	}
	asm := &s.snapshot
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
	// The completed transfer is kept rather than cleared: it is what recognises the
	// copies still arriving by the mesh's other paths, and recognising them is the
	// whole of the flood's termination.
	tick, whole := asm.Result()
	s.statCorrections.Add(1)
	if r := s.world.Resources.Network; r != nil && r.OnCorrection != nil {
		r.OnCorrection(tick, whole)
	}
}

// receiveSelective hands one selective-correction frame to the session layer: a
// manifest, a receiver's answer to one, the repair that answers that, or the
// refusal a retention holder gives when it cannot produce one.
//
// None of the four is relayed *here*, and that is deliberate rather than an
// omission. The transport's flood forwards what it admitted, which is the right
// rule for an artifact everyone needs and the wrong one for an exchange: a request
// or a repair forwarded to a peer that did not ask would deliver a page vector it
// holds no retention for. Forwarding a manifest is a decision about retention —
// only a participant that can answer for the tick it names may pass the question
// on — so it is made in the session layer, which is what holds the ring. See
// corrections.forwardManifest and corrections.canAnswerEveryParticipant.
func (s *NetworkSystem) receiveSelective(kind network.MessageType, from uint32, body []byte) {
	if from == 0 {
		s.statDrop.Add(1)
		return
	}
	if r := s.world.Resources.Network; r != nil && r.OnSelective != nil {
		r.OnSelective(uint8(kind), from, body)
	}
}

// receiveAuthority hands one succession frame to the session layer.
//
// Unlike the selective exchange these are flooded rather than unicast, and the
// flood is the point: a survivor two links from the lost authority learns of the
// loss from the reports that cross it, because the departure crossing that used
// to carry that news is produced by the authority — the thing that is gone. The
// session layer decides what to relay onward, because it is what deduplicates by
// term and participant.
func (s *NetworkSystem) receiveAuthority(kind network.MessageType, from uint32, body []byte) {
	if from == 0 {
		s.statDrop.Add(1)
		return
	}
	if r := s.world.Resources.Network; r != nil && r.OnAuthority != nil {
		r.OnAuthority(uint8(kind), from, body)
	}
}

// receiveSessionRestart hands the authority's rebuild notice to the session layer.
// Only the authority's own is taken: a participant that could make its peers tear
// down and redial on demand is a participant that could empty a session, and the
// term already names which one of them speaks for it.
func (s *NetworkSystem) receiveSessionRestart(from uint32, addr []byte) {
	r := s.world.Resources.Network
	if r == nil || r.OnSessionRestart == nil {
		return
	}
	if from == 0 || from != r.Authority.Load() {
		s.statDrop.Add(1)
		return
	}
	if len(addr) > network.MaxSessionName {
		s.statDrop.Add(1) // an address, not a document
		return
	}
	r.OnSessionRestart(from, string(addr))
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
//
// completedTick can stand behind productionEpoch after an install moved the epoch
// forward. Epochs up to that high-water mark were already sent, so sending them
// again would make the receiver's duplicate filter discard any new frames they
// carry. The frames stay pending until the world reaches the next unsent epoch;
// they were stamped for it by Cross and leave together under its one marker.
func (s *NetworkSystem) flushCrossings(p engine.NetworkPort, completedTick uint64, active bool) {
	s.mu.Lock()
	dropped := s.encodeErr
	s.encodeErr = 0
	if active && completedTick < s.productionEpoch {
		s.mu.Unlock()
		if dropped != 0 {
			s.statDrop.Add(dropped)
		}
		return
	}
	epoch := s.productionEpoch
	pending := s.crossings
	s.crossings = nil
	s.productionEpoch = max(epoch, completedTick+1)
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
		Source: source, ProducedTick: epoch, Frames: pending,
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
// This is where D-11's live contract lands in the runtime. A guest predicts between
// corrections, so it may differ from the host. The counter and part name cheaply
// describe where two instances stand apart; authority corrections, not a terminal
// verdict, close it.
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

// scheduleCrossings admits one production epoch. The authority commits a guest's
// raw epoch — on time as stamped, late at its own next tick, too late as void — and
// relays the committed copy; every other instance applies another participant's
// crossings only as the authority committed them, and only passes a raw epoch on
// toward the authority, so nobody lands an action at a tick the authority did not choose.
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
	local, authority := s.participantID(), s.authorityParticipant()
	raw := !batch.Committed && batch.Source != authority
	committing := raw && local == authority
	switch {
	case batch.Source == local:
		return // this instance's own epoch, back round a cycle
	case committing && batch.Source != from && s.linked(batch.Source):
		// A raw epoch from a producer on a link of its own arrives on that link. A
		// relayed copy of one already admitted is a duplicate; any other is claimed.
		s.mu.Lock()
		dup := s.epochs[batch.Source].has(batch.ProducedTick)
		s.mu.Unlock()
		if !dup {
			s.statForged.Add(int64(max(1, len(batch.Frames))))
		}
		return
	case raw && !committing:
		s.mu.Lock()
		fresh := s.rawEpochs[batch.Source].admit(batch.ProducedTick)
		s.mu.Unlock()
		s.statUncommitted.Add(1)
		if fresh {
			s.relayRaw(from, authority, batch)
		}
		return
	}

	// The forward window, applied before the epoch window rather than after it. An
	// epoch from beyond the horizon is refused without being admitted, so it
	// neither reserves a schedule entry nor advances this source's high-water mark.
	horizon := s.localTick() + parameter.NetworkApplyWindowTicks
	if batch.ProducedTick > horizon {
		s.statRefusedTick.Add(int64(max(1, len(batch.Frames))))
		return
	}

	s.mu.Lock()
	admitted := s.epochs[batch.Source].admit(batch.ProducedTick)
	installed, superseded, refused, overflowed, late, void := 0, 0, 0, 0, 0, 0
	sourceFence := s.snapshotFences.Seq(network.PeerID(batch.Source))
	held := 0
	for _, a := range s.scheduled {
		if a.source == batch.Source {
			held++
		}
	}
	if admitted {
		for i := range batch.Frames {
			f := &batch.Frames[i]
			// A batch inside the window may still carry a frame outside it: the
			// apply tick is per frame and is not derived from the epoch's.
			if f.ApplyTick > horizon {
				refused++
				continue
			}
			a := barrierArtifact{
				frame: f.Frame, applyTick: f.ApplyTick, source: batch.Source, origin: event.OriginNetwork,
			}
			if committing {
				if next := s.localTick() + 1; a.applyTick < next {
					late++
					if next-a.applyTick > parameter.NetworkCommitLateTicks {
						a.void, f.Frame.Event = true, ""
						void++
					}
					a.applyTick, f.ApplyTick = next, next
				}
			}
			// The epoch is still admitted when its artifact is already in the
			// installed world, so relay and duplicate suppression stay coherent for
			// peers that did not install that capture.
			contained, bySequence := s.snapshotContainsLocked(a)
			if contained {
				installed++
				if bySequence {
					superseded++
				}
				continue
			}
			n := frameBytes(a.frame)
			if held >= parameter.NetworkScheduledPerSource || len(s.scheduled) >= parameter.NetworkScheduledMax ||
				s.scheduledBytes+n > parameter.NetworkScheduledBytes {
				overflowed++
				continue
			}
			s.scheduled = append(s.scheduled, a)
			s.scheduledBytes += n
			held++
		}
	}
	s.mu.Unlock()
	if admitted && batch.Source == authority {
		s.observeAuthorityEpoch(batch.ProducedTick)
	}
	if late > 0 {
		s.statCommitLate.Add(int64(late))
		s.statCommitVoid.Add(int64(void))
		// One per epoch: a link is slow by how often its ticks land late, not by
		// how much its player did in each of them
		if r := s.world.Resources.Network; r != nil {
			r.CommitLate[batch.Source].Add(1)
		}
	}
	if installed > 0 {
		s.statPreInstall.Add(int64(installed))
	}
	if refused > 0 {
		s.statRefusedTick.Add(int64(refused))
	}
	if overflowed > 0 {
		s.statScheduleFull.Add(int64(overflowed))
		vlog.Warn("app", "msg", "barrier schedule full",
			"source", batch.Source, "produced_tick", batch.ProducedTick,
			"dropped", overflowed, "held", held, "bytes", s.scheduledBytes)
	}
	if superseded > 0 {
		s.statSuperseded.Add(int64(superseded))
		vlog.Debug("app", "msg", "snapshot refused crossings the installed world holds",
			"source", batch.Source, "produced_tick", batch.ProducedTick,
			"source_fence", sourceFence, "superseded", superseded)
	}

	if !admitted {
		s.statDuplicates.Add(1)
		return
	}
	// A void frame applies nowhere else, so it is not relayed; its fence closes here.
	batch.Frames = slices.DeleteFunc(batch.Frames, func(f event.ScheduledWireFrame) bool { return f.Frame.Event == "" })
	batch.Committed = true
	s.relayBatch(from, batch)
}

// relayRaw passes a raw epoch on toward the authority: straight to it when this
// instance has a link to it, else onward through the mesh.
func (s *NetworkSystem) relayRaw(from, authority uint32, batch event.WireBatch) {
	p := s.port()
	if p == nil || !s.linked(authority) {
		s.relayBatch(from, batch)
		return
	}
	batch.Hops++
	if body, err := event.EncodeWireBatch(batch); err == nil && batch.Hops < parameter.NetworkRelayHopLimit {
		p.Send(authority, uint8(network.MsgEvent), body)
	}
}

// linked reports whether a participant is on a direct link of this instance.
func (s *NetworkSystem) linked(id uint32) bool {
	p, ok := s.port().(interface{ Peers() []uint32 })
	return ok && slices.Contains(p.Peers(), id)
}

// observeAuthorityEpoch paces a guest against the authority. The sample is how
// late the epoch landed: this instance's completed tick minus the epoch's. The
// latest of a window decides, and a step clears the window it has answered.
func (s *NetworkSystem) observeAuthorityEpoch(produced uint64) {
	r := s.world.Resources.Network
	if r == nil || s.participantID() == s.authorityParticipant() {
		return
	}
	s.paceSamples[s.paceCount%len(s.paceSamples)] = int64(s.localTick()) - int64(produced)
	s.paceCount++
	if s.paceCount < len(s.paceSamples) {
		return
	}
	latest := slices.Max(s.paceSamples[:])
	trim, step := paceDecision(latest)
	s.paceLatest, s.paceKnown = latest-step, true
	s.statPaceLate.Store(latest)
	s.statPaceTrim.Store(int64(trim))
	r.Pace.Store(trim)
	if step != 0 {
		r.PaceStep.Add(step)
		s.statPaceSteps.Add(1)
		s.paceCount = 0
	}
}

// paceDecision turns the latest arrival offset into a trim of the tick interval, in
// permille, and a whole-tick step; positive slows this instance down. Inside the
// band it rests; far outside it steps, and a step back is bounded by the debt the
// scheduler runs back to back.
func paceDecision(latest int64) (trimPermille int32, stepTicks int64) {
	late := latest - parameter.NetworkAheadTicks
	early := parameter.NetworkAheadTicks - parameter.NetworkPaceBandTicks - latest
	trim := func(ticks int64) int32 {
		return int32(min(ticks*parameter.NetworkPaceGainPermille, parameter.NetworkPacePermilleMax))
	}
	switch {
	case late >= parameter.NetworkPaceStepTicks:
		return 0, late
	case late > 0:
		return trim(late), 0
	case early >= parameter.NetworkPaceStepTicks:
		return 0, -min(early, parameter.NetworkPaceStepBackTicks)
	case early > 0:
		return -trim(early), 0
	}
	return 0, 0
}

// driveOwnLead re-derives the lead this instance stamps its crossings with and
// pushes a change as a journaled local event, so a reproduction switches on the
// same tick. Raising is immediate; lowering waits a window of the lower reading.
func (s *NetworkSystem) driveOwnLead(p engine.NetworkPort) {
	r := s.world.Resources.Network
	if r == nil || p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return
	}
	target, ok := s.ownLead(p)
	if !ok {
		return
	}
	next, lowSince := chooseLead(r.BarrierDelayTicks, target, s.localTick(), s.leadLowSince)
	s.leadLowSince = lowSince
	if next == r.BarrierDelayTicks || next == s.leadPushed {
		return
	}
	s.leadPushed = next
	s.world.PushEventFull(event.EventPlayoutLead,
		&event.PlayoutLeadPayload{Ticks: next}, event.OriginSession, core.DomainPlayer)
}

// ownLead is the round trip to the authority plus its jitter margin, a tick for
// the authority to relay it, less how late the authority's epochs already land
// here. The authority's own crossings wait only for their tick to close.
func (s *NetworkSystem) ownLead(p engine.NetworkPort) (uint64, bool) {
	authority := s.authorityParticipant()
	if s.participantID() == authority {
		return parameter.NetworkBarrierMinDelayTicks, true
	}
	link, ok := p.(engine.LinkMeasuringPort)
	if !ok {
		return 0, false
	}
	m := link.LinkMetric(authority)
	if !m.Ready || m.RTT <= 0 || !s.paceKnown {
		return parameter.NetworkBarrierDelayTicks, true
	}
	trip := m.RTT + parameter.NetworkBarrierJitterMargin*m.Jitter
	ticks := int64((trip+parameter.GameUpdateInterval-1)/parameter.GameUpdateInterval) +
		parameter.NetworkRelaySlackTicks - s.paceLatest
	return uint64(min(max(ticks, parameter.NetworkBarrierMinDelayTicks), parameter.NetworkBarrierMaxDelayTicks)), true
}

// chooseLead raises at once and lowers only after the lower target has held for
// NetworkBarrierRenegotiateTicks: a crossing that misses its lead costs a
// correction, one that clears it costs nothing.
func chooseLead(current, target, tick, lowSince uint64) (next, nextLowSince uint64) {
	switch {
	case target >= current:
		return target, 0
	case lowSince == 0 || tick < lowSince:
		return current, tick
	case tick-lowSince >= parameter.NetworkBarrierRenegotiateTicks:
		return target, 0
	}
	return current, lowSince
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
func (s *NetworkSystem) applyDue(nextTick uint64) {
	s.writeDueStates(nextTick)
	s.mu.Lock()
	due := make([]barrierArtifact, 0, len(s.scheduled))
	keep := s.scheduled[:0]
	held := 0
	for _, a := range s.scheduled {
		if a.applyTick <= nextTick {
			due = append(due, a)
		} else {
			keep = append(keep, a)
			held += frameBytes(a.frame)
		}
	}
	s.scheduled = keep
	s.scheduledBytes = held
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
		if a.void {
			s.noteAppliedFrom(a.source, a.frame.Seq)
			continue
		}
		et, payload, domain, err := a.frame.Decode()
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		if !s.admissibleFromSource(et, a.source) {
			s.statForged.Add(1)
			vlog.Warn("app", "msg", "artifact refused",
				"peer", a.source, "event", event.GetEventName(et), "apply_tick", a.applyTick)
			continue
		}
		if a.applyTick < nextTick {
			// Committed frames reach a paced guest before their tick, so this counts
			// a guest running ahead of its band; its next correction repairs it.
			s.statLate.Add(1)
		}
		ev := event.GameEvent{Type: et, Payload: payload, Origin: a.origin, Domain: domain}
		if a.source == localSource && !barrierBound(et) {
			// The local fence closes at dispatch: a capture read between the
			// schedule and the handlers must not claim an effect the world does
			// not yet hold.
			ev.CrossingSeq = a.frame.Seq
		}
		s.world.Resources.Event.Queue.PushReady(ev)
		if barrierBound(et) || a.source != localSource {
			s.retainApplied(a)
		}
		if !barrierBound(et) && a.source != localSource {
			// The receive-side half of the capture fence. Recorded after the push
			// rather than before it, so a capture read between two of these cannot
			// claim an effect the world does not yet hold. The local source's half
			// is closed by CrossingApplied instead, once every handler has run.
			s.noteAppliedFrom(a.source, a.frame.Seq)
		}
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
// question rather than an authority one. Sessions remain limited to trusted peers
// until authentication is implemented.
func (s *NetworkSystem) admissibleFromSource(et event.EventType, source uint32) bool {
	switch et {
	case event.EventParticipantJoined, event.EventParticipantDeparted:
		return source == s.authorityParticipant()
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
		// A departed source's window keeps its last epoch, so it would read as a
		// peer falling further behind on every tick for the rest of the run
		if uint32(source) == local || newest == 0 || s.departed[source] {
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

// publishStaleness reports how far behind the session this instance stands on
// every tick.
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
		// The authority's own sync is committed as sent, to its next tick; a guest's
		// goes raw, for the authority to commit.
		f := event.ScheduledWireFrame{Frame: stateFrame(s.readCursorState(cursor, uint8(i)))}
		if s.isCoordinator() {
			f.ApplyTick = s.localTick() + 1
		}
		body, err := event.EncodeWireBatch(event.WireBatch{
			Source: s.participantID(), Frames: []event.ScheduledWireFrame{f},
		})
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
		p.DamageImmunitySpent = c.DamageImmunitySpent
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

// pendingState is one owner-authored sync and the tick it is written at.
type pendingState struct {
	applyTick uint64
	source    uint32
	payload   *event.CursorStatePayload
}

// scheduleCursorState takes an owner-authored sync the way scheduleCrossings takes
// an epoch: the authority commits a guest's raw one to its next tick and relays it,
// and every other instance writes one only as committed, at that tick, so a value
// a stalled owner sends late lands on one tick everywhere.
func (s *NetworkSystem) scheduleCursorState(from uint32, body []byte) {
	batch, err := event.DecodeWireBatch(body)
	if err != nil || batch.Source == 0 || int(batch.Source) >= participantSlots {
		s.statDrop.Add(1)
		return
	}
	local, authority := s.participantID(), s.authorityParticipant()
	raw := !batch.Committed && batch.Source != authority
	committing := raw && local == authority
	switch {
	case batch.Source == local:
		return
	case committing && batch.Source != from && s.linked(batch.Source):
		s.statForged.Add(1)
		return
	}
	fresh := batch.Frames[:0]
	for _, f := range batch.Frames {
		et, payload, _, err := f.Frame.Decode()
		p, ok := payload.(*event.CursorStatePayload)
		if err != nil || et != event.EventCursorStateSync || !ok || int(p.Slot) >= parameter.MaxPlayers {
			s.statDrop.Add(1)
			continue
		}
		// A raw sync only passed on has its own filter: its committed copy follows.
		seen := &s.stateSeen[p.Slot]
		if raw && !committing {
			seen = &s.rawStateSeen[p.Slot]
		}
		if p.Seq <= *seen {
			continue // reordered, replayed, or back round the mesh
		}
		*seen = p.Seq
		if committing {
			f.ApplyTick = s.localTick() + 1
		}
		if !raw || committing {
			s.states = append(s.states, pendingState{applyTick: f.ApplyTick, source: batch.Source, payload: p})
		}
		fresh = append(fresh, f)
	}
	if len(fresh) == 0 {
		s.statDuplicates.Add(1)
		return
	}
	batch.Frames = fresh
	if raw && !committing {
		s.relayRaw(from, authority, batch)
		return
	}
	batch.Committed = true
	if out, err := event.EncodeWireBatch(batch); err == nil {
		if p := s.port(); p != nil && p.IsRunning() && p.PeerCount() > 0 {
			p.BroadcastExcept(from, uint8(network.MsgStateSync), out)
			s.statRelayed.Add(1)
		}
	}
}

// writeDueStates writes the owner-authored syncs due by nextTick, in the order every
// instance writes them. Caller holds the world lock.
func (s *NetworkSystem) writeDueStates(nextTick uint64) {
	if len(s.states) == 0 {
		return
	}
	due := make([]pendingState, 0, len(s.states))
	keep := s.states[:0]
	for _, st := range s.states {
		if st.applyTick <= nextTick {
			due = append(due, st)
		} else {
			keep = append(keep, st)
		}
	}
	s.states = keep
	slices.SortFunc(due, func(a, b pendingState) int {
		if n := cmp.Compare(a.applyTick, b.applyTick); n != 0 {
			return n
		}
		if n := cmp.Compare(a.source, b.source); n != 0 {
			return n
		}
		return cmp.Compare(a.payload.Seq, b.payload.Seq)
	})
	for _, st := range due {
		s.writeCursorState(st.payload)
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
		c.DamageImmunitySpent = p.DamageImmunitySpent
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
