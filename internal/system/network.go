package system

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/linkpace"
	link "github.com/lixenwraith/vif/pkg/network"
)

// NetworkSystem is this instance's only crossing to its peers: the event queue's
// wire sink (D-3, D-10) and the owner-state sync outbound, and inbound it replays
// each crossing in the domain its producer stamped. It is the sole writer of a
// remote cursor's owner-authored cells, which makes the synced value their single
// authority (D-13).
type NetworkSystem struct {
	world *engine.World

	// Barrier state is written from the lock-free push path and drained under the tick.
	mu             sync.Mutex
	crossings      []event.ScheduledWireFrame
	scheduled      []barrierArtifact
	scheduledBytes int
	epochs         [participantSlots]window
	// rawEpochs dedupes the raw epochs this instance only passes on toward the
	// authority; the committed copy shares their key and must still be admitted.
	// seqs takes each source's crossing sequences once, whatever epoch carries
	// them; ownCommitted is the own epochs returned committed, for the handoff.
	rawEpochs       [participantSlots]window
	seqs            [participantSlots]window
	ownCommitted    window
	productionEpoch uint64
	crossSeq        uint64
	// appliedCrossSeq is the contiguous source-local sequence through which every
	// ordinary local crossing has completed dispatch, or an install discarded as
	// held by the world it wrote. Barrier-bound sequences are closed when scheduled
	// because snapshots classify those by their agreed ApplyTick. appliedAhead holds
	// completions that raced an earlier publisher; a capture exposes the prefix.
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

	// settling holds an own cursor's defeat until the crossings its owner made by the
	// close of its tick have applied here, and drops it if its undoing comes first: a
	// hit already on its way pays its energy back before anyone hears of the defeat it
	// undoes, as one tick settles both in solo play.
	settling []settlingDefeat

	// appliedPeerSeq is each remote source's highest applied sequence. Unlike the
	// local contiguous prefix, a remote fence advances past refused frames because
	// they will not arrive again; relay reordering is repaired by correction.
	appliedPeerSeq [participantSlots]uint64

	// snapshotFloor and snapshotFences describe the last installed world.
	// Barrier-bound membership uses ApplyTick; ordinary crossings use source
	// sequence because the authority may have applied them late.
	snapshotFloor     uint64
	snapshotAuthority uint32
	snapshotFences    link.CrossingFences

	buf [parameter.NetworkDrainWindow]link.Inbound // per-tick drain window

	syncSeq  uint64
	lastSync [parameter.MaxPlayers]uint64 // last applied sync per slot, for reordering
	// states are owner-authored syncs waiting for the tick the authority committed
	// them to, and stateSeen the newest sequence scheduled per slot, which is what
	// stops a flood from scheduling one twice.
	states       []pendingState
	stateSeen    [parameter.MaxPlayers]uint64
	rawStateSeen [parameter.MaxPlayers]uint64
	departed     [participantSlots]bool // participants already announced or noticed
	expiring     [participantSlots]bool // Wraiths whose dismissal this authority has crossed
	ticks        uint64
	statSent     *atomic.Int64
	statRecv     *atomic.Int64
	statState    *atomic.Int64
	statDrop     *atomic.Int64

	statDeferred      *atomic.Int64
	statAppliedLocal  *atomic.Int64
	statAppliedPeer   *atomic.Int64
	statLate          *atomic.Int64
	statDelayTicks    *atomic.Int64
	statRanWithout    *atomic.Int64
	statPeerLag       *atomic.Int64
	statPeerArtifacts *atomic.Int64
	statPeerApplied   *atomic.Bool
	statPeers         *atomic.Int64
	statConnected     *atomic.Bool
	statConnection    *status.AtomicString
	statMapLatched    *atomic.Bool
	statHostLost      *atomic.Bool
	statLostIn        *atomic.Int64
	statLostOut       *atomic.Int64
	statSentKind      [link.KindCount]*atomic.Int64
	statWireOut       *atomic.Int64
	statWireIn        *atomic.Int64
	statRelayed       *atomic.Int64
	statDuplicates    *atomic.Int64

	statPreInstall *atomic.Int64
	statSuperseded *atomic.Int64

	// statRefusedTick and statScheduleFull count what the forward window, the run
	// and the schedule's ceilings turned away. They are separate from statDrop
	// because a drop is a frame this instance could not read and these are frames it
	// read and refused: past a reset's last epochs, a non-zero pair names a peer
	// whose ticks or whose volume are not those of a participant.
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

	// statUncarried counts raw artifacts from a participant this instance does not
	// carry toward the authority, its player not relaying for others.
	statUncarried *atomic.Int64

	// The link measurement: the slowest link's round trip, variation, rate and
	// peer, and the highest loss any link reports. It is the transport's own
	// estimate, read and stored, so a network round trip never becomes a value a
	// tick could branch on; with no link measured every cell reads zero.
	statRTT       *atomic.Int64
	statJitter    *atomic.Int64
	statLinkBps   *atomic.Int64
	statLinkLoss  *atomic.Int64
	statLinkPeer  *atomic.Int64
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
	snapshot link.SnapshotAssembly

	// suffix retains ordinary crossings for projection; barrier-bound artifacts
	// are retained in applied instead. lostSeq marks the newest evicted sequence:
	// a suffix with a gap beyond the correction fence must never be replayed.
	suffix        []localCrossing
	suffixBytes   int
	lostSeq       uint64
	suffixDropped int64

	// applied is the bounded set of peer and barrier-bound artifacts this instance
	// has applied that a correction may still predate. The suffix above answers
	// "what did this participant do that the authority has not described yet"; this
	// answers it for everything else this instance applied, which nothing replays.
	applied []barrierArtifact

	// due, retain and dueStates are applyDue's per-tick scratch, reused: one tick
	// runs at a time, and what a tick applied is dead by the next.
	due, retain []barrierArtifact
	dueStates   []pendingState

	// paceSamples holds the newest arrival offsets of the authority's epochs, and
	// paceCount how many are live; paceSettled is the first local tick sampled after
	// the last step is served. See observeAuthorityEpoch.
	paceSamples [parameter.NetworkPaceWindow]int64
	paceCount   int
	paceLatest  int64
	paceKnown   bool
	// paceStale is pacing's verdict that this instance is out of step: its last
	// decision stepped, or the latest arrival was late beyond the band.
	paceStale   bool
	paceSettled uint64

	// pathAge is the age at which the authority committed the newest of this
	// instance's own epochs, pathEpoch which one, and pathWorst the worst age a copy
	// returned since the lead last read one; see noteOwnCommit.
	pathAge   int64
	pathWorst int64
	pathEpoch uint64

	// downAt is, per link, the tick a raw artifact last arrived on it or the
	// participant on it joined through this instance: the links behind this one.
	// relay is the participant this instance reaches the authority through when no
	// link to it exists: the one it joined through, or the one that returned its
	// own epochs committed once that was gone.
	downAt [participantSlots]uint64
	relay  uint32

	// served marks, per link, a participant that joined through this instance: one
	// it carries toward the authority whatever its player chose. Any other it
	// carries only once its player turned relaying on; see carries.
	served [participantSlots]bool

	// leadWindow holds the own lead's reading at each of the last
	// NetworkBarrierRenegotiateTicks ticks, leadCount how many have been read, and
	// leadPushed the change already pushed and not yet dispatched.
	leadWindow    [parameter.NetworkBarrierRenegotiateTicks]uint64
	leadCount     uint64
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

	toggle
}

// localCrossing retains the wire representation so projection, authority
// application and journaling use identical payloads and apply ticks.
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

// epochWindow admits each source epoch once within NetworkEpochWindow.
// A bitmap preserves out-of-order arrivals over unequal mesh paths; a single
// high-water mark would silently discard them as duplicates.
type window struct {
	high uint64                                       // newest admitted; zero means nothing yet
	seen [parameter.NetworkSequenceWindow / 64]uint64 // bit i set: high-1-i was admitted
}

// admit reports whether n is new within width of the newest and records it. Zero
// is neither a production epoch nor a sequence, so it is refused rather than
// treated as "nothing seen yet".
func (w *window) admit(n, width uint64) bool {
	switch {
	case n == 0:
		return false
	case w.high == 0:
		w.high = n
		return true
	case n > w.high:
		// The old high joins the backlog, shifted by the gap it now sits behind.
		shift := n - w.high
		w.shift(shift)
		w.high = n
		if i, bit, ok := w.bit(n-shift, uint64(len(w.seen))*64); ok {
			w.seen[i] |= bit
		}
		return true
	case n == w.high:
		return false
	}
	i, bit, ok := w.bit(n, width)
	if !ok || w.seen[i]&bit != 0 {
		return false
	}
	w.seen[i] |= bit
	return true
}

// shift moves the backlog by further behind the newest.
func (w *window) shift(by uint64) {
	if by >= uint64(len(w.seen))*64 {
		w.seen = [len(w.seen)]uint64{}
		return
	}
	words, bits := int(by/64), by%64
	for i := len(w.seen) - 1; i >= 0; i-- {
		var v uint64
		if j := i - words; j >= 0 {
			v = w.seen[j] << bits
			if bits != 0 && j > 0 {
				v |= w.seen[j-1] >> (64 - bits)
			}
		}
		w.seen[i] = v
	}
}

// bit locates n in the backlog: its word and bit, if it is within width.
func (w *window) bit(n, width uint64) (int, uint64, bool) {
	behind := w.high - n
	if behind == 0 || behind > width {
		return 0, 0, false
	}
	return int((behind - 1) / 64), 1 << ((behind - 1) % 64), true
}

// has reports whether n was admitted, without admitting it.
func (w *window) has(n, width uint64) bool {
	switch {
	case n == 0 || w.high == 0 || n > w.high:
		return false
	case n == w.high:
		return true
	}
	i, bit, ok := w.bit(n, width)
	return ok && w.seen[i]&bit != 0
}

// newest reports the highest number admitted, for the playout lag telemetry that
// asks how far behind a peer's production is.
func (w *window) newest() uint64 { return w.high }

// participantSlots sizes every array this system indexes by participant identity.
// The coordinator hands out 1..MaxPlayers+1 and never zero; one name for every
// declaration keeps a short array from silently dropping the epochs, corrections
// and departure of the identity at the top of the range.
const participantSlots = parameter.MaxPlayers + 2

// settlingDefeat is a defeat held until settleAt, the apply tick of the newest
// crossing its owner had made when the defeat's tick closed; zero until then.
type settlingDefeat struct {
	ev       event.GameEvent
	entity   core.Entity
	settleAt uint64
}

// barrierArtifact is one encoded local or peer crossing waiting for its apply tick,
// and the epoch its source produced it in.
type barrierArtifact struct {
	frame     event.WireFrame
	applyTick uint64
	produced  uint64
	source    uint32
	origin    event.Origin
	// void marks a crossing the authority refused as too late: it applies nothing
	// and only closes its source's fence, so its producer stops replaying it.
	void bool
	// authored marks a frame its source sent while it held the authority, which a
	// roster artifact keeps across a handoff that lands before its apply tick.
	authored bool
}

// frameBytes is what one artifact costs a bounded buffer: its payload, the only
// part of it that grows. The schedule and the replay suffix measure the same thing
// the same way, so their two ceilings stay comparable rather than drifting into
// two ideas of a frame's size.
func frameBytes(f event.WireFrame) int { return len(f.Payload) }

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
	for k := range s.statSentKind {
		s.statSentKind[k] = s.intStat(reg, "network.sent_"+link.ByteKind(k).String()+"_bytes")
	}
	s.statWireOut = s.intStat(reg, "network.wire_bytes_out")
	s.statWireIn = s.intStat(reg, "network.wire_bytes_in")
	s.statRelayed = s.intStat(reg, "network.relay_forwarded")
	s.statDuplicates = s.intStat(reg, "network.relay_duplicates")
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
	s.statUncarried = s.intStat(reg, "network.relay_refused")
	s.statRTT = s.intStat(reg, "network.link_rtt_ms")
	s.statJitter = s.intStat(reg, "network.link_jitter_ms")
	s.statLinkBps = s.intStat(reg, "network.link_bps")
	s.statLinkLoss = s.intStat(reg, "network.link_loss_pct")
	s.statLinkPeer = s.intStat(reg, "network.link_peer")
	s.statSaturated = s.boolStat(reg, "network.link_saturated")
	// Not registered through intStat: this cell is App's and a session reset must
	// not clear the magnitude of a correction App has already published into it.
	s.statMagnitude = reg.Ints.Get("snapshot.correction_entities")
	s.statPaceLate = s.intStat(reg, "network.pace_late_ticks")
	s.statPaceTrim = s.intStat(reg, "network.pace_trim_permille")
	s.statPaceSteps = s.intStat(reg, "network.pace_steps")

	return s
}

// Init resets the barrier and leaves its sink installed as a no-op without peers.
func (s *NetworkSystem) Init() {
	s.enabled = true
	s.ticks = 0
	s.syncSeq = 0
	s.lastSync = [parameter.MaxPlayers]uint64{}
	s.states, s.stateSeen, s.rawStateSeen = s.states[:0], [parameter.MaxPlayers]uint64{}, [parameter.MaxPlayers]uint64{}
	s.departed, s.expiring = [participantSlots]bool{}, [participantSlots]bool{}

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
	s.paceCount, s.paceLatest, s.paceKnown, s.paceSettled, s.paceStale = 0, 0, false, 0, false
	s.pathAge, s.pathEpoch = 0, 0
	s.downAt = [participantSlots]uint64{}
	s.leadCount, s.leadPushed = 0, 0
	s.snapshot = link.SnapshotAssembly{}

	// Held derivations go for the reason the replay suffix below does: a reward the
	// authority never proved belongs to the run that predicted it, and that run has
	// been replaced. Outside the barrier lock, which the ledger does not share.
	s.world.ResetPredictedDeaths()

	s.mu.Lock()
	s.crossings = s.crossings[:0]
	s.settling = s.settling[:0]
	s.scheduled = s.scheduled[:0]
	s.scheduledBytes = 0
	s.applied = s.applied[:0]
	// The replay suffix goes with them, and it has to: a reset restarts crossSeq at
	// zero, so a record retained from before it carries a sequence a post-reset
	// fence would compare against and get wrong in both directions.
	s.suffix = s.suffix[:0]
	s.suffixBytes = 0
	s.suffixDropped = 0
	s.lostSeq = 0
	s.epochs = [participantSlots]window{}
	s.rawEpochs = [participantSlots]window{}
	s.seqs = [participantSlots]window{}
	s.ownCommitted = window{}
	s.snapshotFloor = 0
	s.snapshotAuthority = 0
	s.snapshotFences = nil
	s.appliedPeerSeq = [participantSlots]uint64{}
	// A run's first tick is 1; a reset reaches this handler before MetaSystem
	// re-bases the tick counter, which still holds the replaced run's count.
	s.productionEpoch = 1
	s.lastApplyTick = 0
	s.crossSeq = 0
	s.appliedCrossSeq = 0
	s.appliedAhead = make(map[uint64]struct{})
	s.localSource = s.world.Session().Participant
	s.delayTicks = parameter.NetworkBarrierDelayTicks
	if r := s.world.Resources.Network; r != nil {
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

// EventTypes omits EventMetaSystemCommandRequest deliberately: a disabled bridge
// strands every peer, and only_systems must not do that by omission.
func (s *NetworkSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventParticipantJoined,
		event.EventParticipantDeparted,
		event.EventPlayoutLead,
		event.EventCursorStateSync,
	}
}

func (s *NetworkSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
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
	case event.EventCursorStateSync:
		if p, ok := ev.Payload.(*event.CursorStatePayload); ok {
			s.writeCursorState(p)
		}
	}
}

// addParticipant applies the arrival crossing. Every instance runs it at the same
// apply tick, so the cursor it creates takes the same shared entity everywhere (D-11)
// — which is the reason a mid-run arrival cannot be a local reaction to a connect.
// The instance the payload names is the one that goes on to simulate it. An arrival
// into a slot its participant's own Wraith holds is that participant back.
func (s *NetworkSystem) addParticipant(p *event.ParticipantJoinedPayload) {
	if int(p.Slot) >= parameter.MaxPlayers {
		return
	}
	if e := s.world.Resources.Player.Slot(p.Slot); e != 0 {
		c, _ := s.world.Components.Cursor.GetComponent(e)
		if c.PeerID != p.Participant || !s.world.Components.Wraith.HasEntity(e) {
			return
		}
	}
	control := component.ControlRemote
	if p.Participant == s.participantID() {
		control = component.ControlLocal
	}
	heat, energy := s.world.Resources.Player.InitialResources()
	s.world.PushEvent(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{
		Slot: p.Slot, Center: true, Control: uint8(control), PeerID: p.Participant, Holder: p.Holder,
		Heat: heat, Energy: energy, Claim: p.Claim, Record: p.Record,
	})
	if control == component.ControlLocal {
		s.world.PushEvent(event.EventCursorSetLocalRequest, &event.CursorSetLocalPayload{Slot: p.Slot})
	}
	s.lastSync[p.Slot], s.stateSeen[p.Slot], s.rawStateSeen[p.Slot] = 0, 0, 0
	if int(p.Participant) < len(s.departed) {
		s.departed[p.Participant], s.expiring[p.Participant] = false, false
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

// localRun is the run this instance's ticks count in, which every batch it
// sends names and every batch it takes must name: a reset applies at one agreed
// tick, and what a guest sent a round trip before it arrives after.
func (s *NetworkSystem) localRun() uint64 {
	return s.world.Resources.Event.Queue.Stamp().Run
}

// adoptDelay installs this instance's own lead at the tick its journaled event
// dispatches, so a reproduction switches on the same tick. The resource is the copy
// refreshLink re-reads; a replay holds none, and keeps the system's own.
// Caller holds the world lock.
func (s *NetworkSystem) adoptDelay(ticks uint64) {
	ticks = min(max(ticks, parameter.NetworkBarrierMinDelayTicks), parameter.NetworkBarrierMaxDelayTicks)
	s.mu.Lock()
	same := s.delayTicks == ticks
	s.delayTicks = ticks
	s.mu.Unlock()
	if r := s.world.Resources.Network; r != nil {
		same = r.BarrierDelayTicks == ticks
		r.BarrierDelayTicks = ticks
	}
	if same {
		return
	}
	s.statDelayTicks.Store(int64(ticks))
	s.world.Log().Info("net", "msg", "playout lead adopted", "ticks", ticks, "tick", s.localTick(),
		"pace_latest", s.paceLatest)
}

// barrierDelayTicks returns this instance's own lead.
func (s *NetworkSystem) barrierDelayTicks() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delayTicks
}

// removeParticipant applies the departure crossing. It arrives at the same apply
// tick on every instance, so the change it derives does too (D-5) — which is the
// whole reason a disconnect is not acted on where it is observed. A dismissal
// despawns the cursor; any other departure turns it Wraith, keeping its slot.
func (s *NetworkSystem) removeParticipant(p *event.ParticipantDepartedPayload) {
	if p.Participant == 0 {
		return
	}
	if r := s.world.Resources.Network; r != nil && p.Dismissed && p.Participant == r.ParticipantID && r.OnDismissed != nil {
		// A dismissed instance must not relay its closing links as fresh departures.
		s.enabled = false
		r.OnDismissed()
	}
	// One crossing retires the whole holder group at the same simulation boundary,
	// in the same way: a holder's bots wait with it, or go with it.
	var children []event.ParticipantDepartedPayload
	for _, e := range s.world.Components.Cursor.Entities() {
		c, ok := s.world.Components.Cursor.GetComponent(e)
		if ok && c.Holder == p.Participant {
			children = append(children, event.ParticipantDepartedPayload{Participant: c.PeerID, Slot: c.Slot, Dismissed: p.Dismissed})
		}
	}
	for i := range children {
		s.removeParticipant(&children[i])
	}
	s.forgetParticipant(p.Participant)
	if int(p.Participant) < len(s.departed) {
		s.departed[p.Participant], s.expiring[p.Participant] = true, false
	}
	var cursor core.Entity
	if int(p.Slot) < parameter.MaxPlayers {
		cursor = s.world.Resources.Player.Slot(p.Slot)
	}
	c, held := s.world.Components.Cursor.GetComponent(cursor)
	held = held && c.PeerID == p.Participant
	if r := s.world.Resources.Network; r != nil && r.OnDeparture != nil {
		// A participant with no cursor has nothing to wait in, so it leaves for good.
		r.OnDeparture(p.Participant, p.Dismissed || !held)
	}
	if port, ok := s.port().(engine.PeerDroppingPort); ok {
		port.Disconnect(p.Participant)
	}
	if !held || s.world.SimulatesLocally(cursor) {
		return
	}
	if p.Dismissed {
		s.world.PushEvent(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: p.Slot})
	} else {
		s.world.PushEvent(event.EventCursorWraithRequest, &event.CursorWraithRequestPayload{Slot: p.Slot})
	}
	s.lastSync[p.Slot], s.stateSeen[p.Slot], s.rawStateSeen[p.Slot] = 0, 0, 0
}

// forgetParticipant clears a departed identity for whoever takes it next, whose
// sequence starts at one: its fence, and its ordinary crossings still scheduled or
// retained, any of which would re-raise the fence and make the next holder's first
// crossings read as applied (multi-player.md §3.2). Committed copies precede the
// departure on every link, so all instances drop alike.
func (s *NetworkSystem) forgetParticipant(participant uint32) {
	if participant == 0 || int(participant) >= len(s.appliedPeerSeq) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgetLocked(func(source uint32) bool { return source == participant }, func(a barrierArtifact) bool {
		return a.source == participant && !barrierBound(a.frame.Event)
	})
}

// LeaveSession forgets the session this instance has left, as every other
// participant's departure would and further: none of what they sent applies here
// any more, barrier-bound or not, and no roster change still scheduled is this
// session's. Its own crossings still apply. Caller holds the world lock.
func (s *NetworkSystem) LeaveSession() {
	s.states = s.states[:0] // owner syncs are other participants'; an own one is only forwarded
	s.mu.Lock()
	defer s.mu.Unlock()
	local := s.localSource
	s.forgetLocked(func(source uint32) bool { return source != local }, func(a barrierArtifact) bool {
		et := a.frame.Event
		return a.source != local || et == event.EventParticipantJoined || et == event.EventParticipantDeparted
	})
	s.relay = 0
	s.served = [participantSlots]bool{}
}

// forgetLocked clears the sources gone names and drops the artifacts drop names,
// scheduled or retained. Caller holds s.mu.
func (s *NetworkSystem) forgetLocked(gone func(uint32) bool, drop func(barrierArtifact) bool) {
	for id := uint32(1); id < participantSlots; id++ {
		if gone(id) {
			s.appliedPeerSeq[id], s.downAt[id], s.seqs[id] = 0, 0, window{}
		}
	}
	kept := make(link.CrossingFences, 0, len(s.snapshotFences)) // shared with the capture header
	for _, f := range s.snapshotFences {
		if !gone(uint32(f.Source)) {
			kept = append(kept, f)
		}
	}
	s.snapshotFences = kept
	s.scheduled = slices.DeleteFunc(s.scheduled, drop)
	s.scheduledBytes = 0
	for _, a := range s.scheduled {
		s.scheduledBytes += frameBytes(a.frame)
	}
	s.applied = slices.DeleteFunc(s.applied, drop)
}

// Cross encodes and schedules a crossing when a peer is live, taking the event: the
// local copy is applied by the barrier at the artifact's agreed tick, in the order
// every other instance applies it, and the pooled original is released after
// encoding. There is no producer copy published sooner (multi-player.md §3.1), and
// a cursor's defeat settles first.
func (s *NetworkSystem) Cross(ev event.GameEvent) (taken bool) {
	if !s.barrierActive.Load() {
		return false
	}
	if s.world.FollowsJournal() {
		// The recorded run's barrier applied this one where the journal records it
		event.ReleaseDeferredPayload(ev.Payload)
		return true
	}
	if ev.Type == event.EventCursorDefeatState && s.settles(ev) {
		return true
	}
	s.schedule(ev)
	return true
}

// settles holds a defeat and drops a held one its undoing reaches first, reporting
// whether it took the event; an undoing with nothing held crosses as it is.
func (s *NetworkSystem) settles(ev event.GameEvent) bool {
	p, ok := ev.Payload.(*event.CursorDefeatStatePayload)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.settling, func(d settlingDefeat) bool { return d.entity == p.Entity })
	switch {
	case p.Defeated && i < 0:
		s.settling = append(s.settling, settlingDefeat{ev: ev, entity: p.Entity})
		return true
	case !p.Defeated && i >= 0:
		event.ReleaseDeferredPayload(s.settling[i].ev.Payload)
		s.settling = slices.Delete(s.settling, i, i+1)
	case !p.Defeated:
		return false
	}
	event.ReleaseDeferredPayload(ev.Payload)
	return true
}

// crossSettled crosses each held defeat once the crossings its owner made by the
// close of its tick have applied, by completedTick; with the barrier gone, at once
// and here, ahead of any undoing made from now on.
func (s *NetworkSystem) crossSettled(completedTick uint64, active bool) {
	s.mu.Lock()
	var due []event.GameEvent
	held := s.settling[:0]
	for _, d := range s.settling {
		if d.settleAt == 0 {
			d.settleAt = s.lastApplyTick
		}
		if active && d.settleAt > completedTick {
			held = append(held, d)
			continue
		}
		due = append(due, d.ev)
	}
	s.settling = held
	s.mu.Unlock()
	for _, ev := range due {
		if active {
			s.schedule(ev)
		} else {
			s.world.Resources.Event.Queue.Push(ev)
		}
	}
}

// schedule encodes one own crossing into the open epoch and onto this instance's
// barrier, at the apply tick its lead gives it.
func (s *NetworkSystem) schedule(ev event.GameEvent) {
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
		return
	}
	frame.Seq = s.crossSeq
	applyTick := max(s.productionEpoch+s.delayTicks, s.lastApplyTick)
	s.lastApplyTick = applyTick
	s.crossings = append(s.crossings, event.ScheduledWireFrame{Frame: frame, ApplyTick: applyTick})
	// Counted, never capped. The cap is on what a peer can make this instance hold;
	// this artifact is this instance's own and has already been broadcast, so
	// refusing it here would be a divergence rather than a defence.
	s.scheduled = append(s.scheduled, barrierArtifact{
		frame: frame, applyTick: applyTick, produced: s.productionEpoch, source: s.localSource, origin: ev.Origin,
	})
	s.scheduledBytes += frameBytes(frame)
	if barrierBound(ev.Type) {
		// A barrier-bound sequence is contained by tick, never by its source's
		// fence. Close its position now so it cannot leave a permanent hole in the
		// ordinary sequence prefix; its effect remains classified by ApplyTick on
		// every instance.
		s.closeAppliedCrossingLocked(frame.Seq)
	} else {
		// Retained for the projection a correction runs, at the agreed tick.
		s.retainLocked(frame, s.productionEpoch, applyTick, ev.Origin)
	}
	s.mu.Unlock()
	event.ReleaseDeferredPayload(ev.Payload)
	s.statDeferred.Add(1)
}

// ResendUncommitted puts this instance's own retained crossings that no authority
// returned committed back into the open epoch, at a handoff: the authority they
// went to is gone and its successor never saw them. The successor commits them
// late and refuses by sequence what it already holds. Caller MUST hold updateMutex.
func (s *NetworkSystem) ResendUncommitted() {
	s.mu.Lock()
	resent, dropped, bytes := 0, 0, 0
	for i := range s.suffix {
		rec := &s.suffix[i]
		if rec.produced >= s.productionEpoch || s.ownCommitted.has(rec.produced, parameter.NetworkSequenceWindow) {
			continue
		}
		if bytes += rec.bytes; bytes > link.MaxPayloadSize/2 {
			dropped++ // more than one frame carries; the next correction takes the rest back
			continue
		}
		rec.produced = s.productionEpoch
		s.crossings = append(s.crossings, rec.frame)
		resent++
	}
	s.mu.Unlock()
	if dropped > 0 {
		s.statDrop.Add(int64(dropped))
	}
	if resent > 0 {
		s.world.Log().Info("session", "msg", "uncommitted crossings resent at handoff",
			"crossings", resent, "dropped", dropped)
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

// Remote fences use a maximum so a refused frame cannot stall replay forever.
// A lower frame overtaken across a relay is recovered by the next correction.
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

// AppliedCrossingFences reads the local contiguous prefix and remote maxima
// under the world lock, so world changes and their containment evidence agree.
func (s *NetworkSystem) AppliedCrossingFences() link.CrossingFences {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(link.CrossingFences, 0, len(s.appliedPeerSeq))
	for source, seq := range s.appliedPeerSeq {
		if seq != 0 {
			out = append(out, link.CrossingFence{Source: link.PeerID(source), Seq: seq})
		}
	}
	if s.localSource != 0 && s.appliedCrossSeq != 0 {
		out = append(out, link.CrossingFence{
			Source: link.PeerID(s.localSource), Seq: s.appliedCrossSeq,
		})
	}
	return out.Normalize()
}

// barrierBound names the artifacts that apply at one agreed tick on every instance,
// the producer included: the ones deciding what the world *is* — roster, run, shared
// identity, a region's spawn progression, the defeat that rebuilds the level, the
// hold every clock stops at — not what happens in a world both already hold, whose
// gap a correction repairs. Nobody's input waits on them. See multi-player.md §3.1.
func barrierBound(et event.EventType) bool {
	switch et {
	case event.EventParticipantJoined, event.EventParticipantDeparted, event.EventGameResetRequest,
		event.EventSwarmSpawnRequest, event.EventQuasarSpawnRequest, event.EventBehaviorSpawnRequest,
		event.EventDrainDefeated, event.EventCursorDefeatState, event.EventGameHold:
		return true
	default:
		return false
	}
}

// retainLocked keeps an own crossing for projection. Ticks hold the suffix inside
// what a correction can rebase onto, records bound a fast producer and bytes a
// pathological payload. lostSeq refuses a suffix with a hole past the correction
// fence: that is a different history, not a shorter one. Caller holds mu.
func (s *NetworkSystem) retainLocked(frame event.WireFrame, produced, applyTick uint64, origin event.Origin) {
	rec := localCrossing{
		frame:    event.ScheduledWireFrame{Frame: frame, ApplyTick: applyTick},
		produced: produced,
		origin:   origin,
		bytes:    frameBytes(frame),
	}
	s.suffix = append(s.suffix, rec)
	s.suffixBytes += rec.bytes

	horizon := uint64(0)
	if produced > parameter.SnapshotReplayTicks {
		horizon = produced - parameter.SnapshotReplayTicks
	}
	cut, bytes := 0, s.suffixBytes
	drop := func() { bytes -= s.suffix[cut].bytes; cut++ }
	for cut < len(s.suffix) && s.suffix[cut].produced < horizon {
		drop()
	}
	for len(s.suffix)-cut > parameter.SnapshotReplayRecords {
		drop()
	}
	for bytes > parameter.SnapshotReplayBytes && len(s.suffix)-cut > 1 {
		drop()
	}
	if cut == 0 {
		return
	}
	for _, old := range s.suffix[:cut] {
		s.lostSeq = max(s.lostSeq, old.frame.Frame.Seq)
	}
	s.suffixDropped += int64(cut)
	s.suffix = append(s.suffix[:0], s.suffix[cut:]...)
	s.suffixBytes = bytes
}

// retainApplied keeps a tick's applied peer and barrier-bound artifacts, in apply
// order, so a projection over a capture that lacks one can re-apply it. Bounded by
// age and by count, like the local suffix; a peer frame the next capture holds is
// pruned by its fence.
func (s *NetworkSystem) retainApplied(batch []barrierArtifact) {
	if len(batch) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, batch...)
	newest := batch[len(batch)-1].applyTick
	kept := s.applied[:0]
	for _, r := range s.applied {
		if r.applyTick+parameter.SnapshotReplayTicks >= newest {
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
func (s *NetworkSystem) RetainedAfter(tick uint64, fences link.CrossingFences) (frames []event.ScheduledWireFrame, sources []uint32, origins []event.Origin) {
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

// PendingAfter is what this instance holds scheduled past tick, as the committed
// epochs it went out in: a joiner given the world at that tick heard none of them.
// An own crossing not yet sent goes out in its own epoch, and a void one applies
// nothing. Caller MUST hold updateMutex.
func (s *NetworkSystem) PendingAfter(tick uint64) []event.WireBatch {
	run := s.localRun()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.WireBatch
	for _, a := range s.scheduled {
		if a.applyTick <= tick || a.void || a.source == s.localSource && a.produced >= s.productionEpoch {
			continue
		}
		i := slices.IndexFunc(out, func(b event.WireBatch) bool {
			return b.Source == a.source && b.ProducedTick == a.produced
		})
		if i < 0 {
			i = len(out)
			out = append(out, event.WireBatch{Run: run, ProducedTick: a.produced, Source: a.source, Committed: true})
		}
		out[i].Frames = append(out[i].Frames, event.ScheduledWireFrame{Frame: a.frame, ApplyTick: a.applyTick})
	}
	return out
}

// retainedContainedLocked is snapshotContainsLocked against a capture being
// projected rather than the one last installed. Caller MUST hold mu.
func (s *NetworkSystem) retainedContainedLocked(a barrierArtifact, tick uint64, fences link.CrossingFences) bool {
	if !barrierBound(a.frame.Event) {
		fence := fences.Seq(link.PeerID(a.source))
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

// LocalReplaySuffix selects ordinary crossings by the capture's source fence,
// not its tick: a late crossing can be absent from a newer capture.
// If retention lost a needed record, refuse the whole suffix.
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
// how many retention has dropped, for the replay telemetry.
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
	s.mu.Lock()
	s.localSource = s.world.Session().Participant
	if r := s.world.Resources.Network; r != nil {
		s.delayTicks = r.BarrierDelayTicks
		if s.relay == 0 {
			s.relay = r.Relay
		}
	}
	s.mu.Unlock()
	s.barrierActive.Store(active)
	s.statDelayTicks.Store(int64(s.barrierDelayTicks()))
	return active
}

// AdoptSnapshot rebases scheduled state onto an installed world. The production
// epoch never rewinds, because peers key duplicates by it: a rewound source holds
// new crossings for its next unsent epoch. Barrier-bound artifacts use the tick
// floor and ordinary ones source fences, for late arrivals too. Caller holds
// updateMutex.
func (s *NetworkSystem) AdoptSnapshot(tick uint64, authority uint32, fences link.CrossingFences) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotFloor = tick
	s.snapshotAuthority = authority
	s.snapshotFences = fences
	localFence := fences.Seq(link.PeerID(s.localSource))
	// A local crossing may have been accepted between the last tick close and this
	// install. One the captured world does not contain is still outstanding, and
	// dropping it would leave only a local replay for a later correction to erase
	// permanently — which is the whole failure this fence exists to prevent.
	pending := s.crossings[:0]
	pendingDropped := 0
	for _, f := range s.crossings {
		if f.Frame.Seq != 0 && f.Frame.Seq <= localFence {
			pendingDropped++
			s.closeAppliedCrossingLocked(f.Frame.Seq)
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
			if a.source == s.localSource {
				// The pruned copy never dispatches, and the world holds its effect:
				// the local fence closes over it here, or every later sequence
				// would wait behind it. An own placement lands in the installed
				// world rather than through CursorSystem, so it settles its D-18
				// prediction here too.
				s.closeAppliedCrossingLocked(a.frame.Seq)
				if a.frame.Event == event.EventCursorMoveRequest {
					s.world.Resources.Player.Reconcile(component.PositionComponent{}, true)
				}
			}
			continue
		}
		keep = append(keep, a)
		held += frameBytes(a.frame)
	}
	s.scheduled = keep
	s.scheduledBytes = held
	// The retained local suffix is pruned by this instance's own fence, never by
	// tick, for the same reason the schedule is: a record the captured world does not
	// contain is still this participant's action, whatever its apply tick says.
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
		if barrierBound(a.frame.Event) {
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
	s.localSource = s.world.Session().Participant
	if r := s.world.Resources.Network; r != nil {
		s.delayTicks = r.BarrierDelayTicks
	}
	if dropped > 0 {
		s.statPreInstall.Add(int64(dropped))
	}
	if superseded > 0 {
		s.statSuperseded.Add(int64(superseded))
	}
	if dropped > 0 || pendingDropped > 0 {
		s.world.Log().Debug("converge", "msg", "snapshot pruned crossings",
			"tick", tick, "authority", authority, "local_fence", localFence,
			"fences", len(fences), "scheduled", dropped, "sequence_superseded", superseded,
			"pending_local", pendingDropped)
	}
}

// snapshotContainsLocked uses source fences for ordinary crossings and the tick
// floor for barrier-bound ones, whose apply tick is exact. A source the capture
// names no fence for is kept: a duplicate is repaired by the next correction, a
// discarded action is not. bySequence reports a fence decided. Caller holds mu.
func (s *NetworkSystem) snapshotContainsLocked(a barrierArtifact) (contained, bySequence bool) {
	if !barrierBound(a.frame.Event) {
		if fence := s.snapshotFences.Seq(link.PeerID(a.source)); fence != 0 {
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

// DrainPeers translates inbound frames without advancing a tick, for a join that
// must learn the session's tick before deciding how many to run. Each artifact is
// scheduled at the tick it names and none applies early. Caller holds updateMutex.
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

// BarrierActive reports whether the barrier owns this run's crossings, as the last
// refresh of the link left it.
func (s *NetworkSystem) BarrierActive() bool { return s.barrierActive.Load() }

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
		s.world.Log().Info("session", "msg", "network session active",
			"local", s.participantID(), "slot", s.world.Resources.Player.LocalSlot(),
			"coordinator", s.isCoordinator(),
			"barrier_delay_ticks", s.barrierDelayTicks(), "peers", peers)
	}
}

func (s *NetworkSystem) Update() {
	p := s.port()
	s.refreshLink(p)
	s.publishConnectionTelemetry(p)
	if r := s.world.Resources.Network; r != nil && s.isCoordinator() {
		r.Pace.Store(0) // the authority is the clock the others pace against
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
		s.release(p)
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
	s.pushSessionRecords(p)
	s.crossSettled(completedTick, active)
	s.flushCrossings(p, completedTick, active)
	if s.enabled && p != nil && p.IsRunning() && s.ticks%parameter.NetworkSyncTicks == 0 {
		s.sendCursorState(p)
	}
	s.release(p)
	s.publishTransportLoss(p)
	s.publishLinkBytes(p)
	s.publishLinkMeasurement(p, completedTick)
	if r := s.world.Resources.Network; r != nil && r.OnTickClosed != nil && s.enabled {
		r.OnTickClosed(completedTick)
	}
}

// pushSessionRecords raises the journaled session events this instance derives
// from its own transport. They are pushed once the tick body has settled, so a
// reproduction, which injects a tick's records after the tick, dispatches them
// where this run does: between ticks.
func (s *NetworkSystem) pushSessionRecords(p engine.NetworkPort) {
	// An instance nothing will correct any more owns what it predicted, so the
	// rewards it is holding are due. Cheap and silent while the ledger is empty,
	// which is every tick of a host and of a run with no session.
	s.world.SettlePredictedDeaths()
	if s.enabled {
		s.driveOwnLead(p)
		if p != nil && p.IsRunning() && s.isCoordinator() {
			s.expireWraiths()
		}
	}
}

// expireWraiths dismisses each participant away for parameter.WraithTimeout of game
// time, and the one the session layer gives a newcomer the place of, once each: the
// authority decides it as it decides every roster change, a bot whose holder waits
// too goes in its holder's crossing, and one on its way back waits for its arrival.
func (s *NetworkSystem) expireWraiths() {
	var yield uint32
	kept := func(uint32) bool { return false }
	if r := s.world.Resources.Network; r != nil {
		if r.DismissWraith != nil {
			yield = r.DismissWraith()
		}
		if r.WraithKept != nil {
			kept = r.WraithKept
		}
	}
	cursors := s.world.Components.Cursor
	var due []event.ParticipantDepartedPayload
	s.world.Components.Wraith.Each(func(e core.Entity, w *component.WraithComponent) bool {
		c, ok := cursors.GetComponent(e)
		if !ok || c.PeerID == 0 || int(c.PeerID) >= len(s.expiring) || s.expiring[c.PeerID] ||
			w.Waited < parameter.WraithTimeout && c.PeerID != yield || c.Holder != 0 && s.away(c.Holder) ||
			kept(c.PeerID) {
			return true
		}
		s.expiring[c.PeerID] = true
		due = append(due, event.ParticipantDepartedPayload{Participant: c.PeerID, Slot: c.Slot, Dismissed: true})
		return true
	})
	for i := range due {
		s.world.Log().Info("session", "msg", "wraith dropped", "participant", due[i].Participant,
			"slot", int(due[i].Slot), "yielded", due[i].Participant == yield)
		s.crossSession(event.EventParticipantDeparted, &due[i])
	}
}

// away reports whether a participant's cursor is a Wraith.
func (s *NetworkSystem) away(participant uint32) bool {
	slot, ok := s.participantSlot(participant)
	return ok && s.world.Components.Wraith.HasEntity(s.world.Resources.Player.Slot(slot))
}

// publishLinkMeasurement hands the transport this instance's progress report and
// copies the link estimate into telemetry. It is the only seam between network
// timing and the simulation, and it only copies: timing may pace a transport but
// never enter shared state, an RNG stream, a replay or a game decision.
func (s *NetworkSystem) publishLinkMeasurement(p engine.NetworkPort, completedTick uint64) {
	meter, ok := p.(engine.LinkMeasuringPort)
	if !ok || !s.enabled || !p.IsRunning() {
		return
	}
	meter.SetLinkReport(s.linkReport(completedTick))

	// The worst link decides what a player is told: a session is as constrained as
	// its most constrained edge, and an average would hide the peer that needs
	// saying. Loss is its own worst, since a lossy link need not be the slow one. A
	// link that is gone is not measured, so the last one leaving clears the cells.
	var worst linkpace.Metrics
	var worstPeer uint32
	loss := 0.0
	for _, peer := range meter.Peers() {
		m := meter.LinkMetric(peer)
		if m.Samples == 0 {
			continue
		}
		if worstPeer == 0 || m.RTT > worst.RTT {
			worst, worstPeer = m, peer
		}
		loss = max(loss, m.Loss)
	}
	s.statRTT.Store(worst.RTTMillis())
	s.statJitter.Store(worst.JitterMillis())
	s.statLinkBps.Store(int64(worst.Throughput))
	s.statLinkLoss.Store(int64(loss * 100))
	s.statLinkPeer.Store(int64(worstPeer))
	s.statSaturated.Store(worst.Saturated)
}

// linkReport describes this instance's picture for the peers measuring it.
// Caller MUST hold updateMutex: it reads the cursor store.
func (s *NetworkSystem) linkReport(completedTick uint64) link.LinkReport {
	r := link.LinkReport{
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
	s.release(p)
}

// drainWith is one poll of the endpoint, whichever door it came through.
func (s *NetworkSystem) drainWith(p engine.NetworkPort, poll func([]link.Inbound) int) {
	n := poll(s.buf[:])
	for i := range n {
		if !s.enabled {
			return
		}
		in := &s.buf[i]
		switch in.Kind {
		case link.InboundConnect:
			if in.Served && int(in.Peer) < len(s.served) {
				s.served[in.Peer] = true
				s.noteDown(uint32(in.Peer))
			}
			s.world.PushLocal(event.EventNetworkConnect, &event.NetworkConnectPayload{PeerID: uint32(in.Peer)})
			s.world.LatchSession()
		case link.InboundDisconnect:
			if int(in.Peer) < len(s.served) {
				s.served[in.Peer] = false
			}
			s.world.LatchSession()
			s.world.PushLocal(event.EventNetworkDisconnect, &event.NetworkDisconnectPayload{PeerID: uint32(in.Peer)})
			dropped := s.reportDisconnect(uint32(in.Peer), p.PeerCount())
			s.noticeDeparture(uint32(in.Peer), dropped)
		case link.InboundMessage:
			if r := s.world.Resources.Network; r != nil && in.Msg != nil && int(in.Peer) < len(r.Ingress) {
				r.Ingress[in.Peer].Add(uint64(link.HeaderSize + len(in.Msg.Payload)))
			}
			s.dispatchMessage(uint32(in.Peer), in.Msg)
		}
	}
}

// reportDisconnect names link loss when it happens. Losing the authority is
// called out apart on a guest, which plays on from the last authoritative state
// until a succession settles. It reports whether the link was a seat this run
// dropped.
func (s *NetworkSystem) reportDisconnect(peerID uint32, remaining int) (dropped bool) {
	authorityLost := peerID == s.authorityParticipant() && !s.isCoordinator()
	message := fmt.Sprintf("Participant %d disconnected", peerID)
	if peerID == s.relay && !authorityLost {
		message = "Relay connection lost; looking for another path to the authority"
	}
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
	// whenever any link does. A seat this run dropped leaves as it was told to.
	if r := s.world.Resources.Network; r != nil && r.OnPeerLost != nil {
		dropped = r.OnPeerLost(peerID)
	}
	if dropped {
		s.world.Log().Info("net", "msg", "seat link closed", "peer", peerID, "remaining_peers", remaining)
		return true
	}
	s.world.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{Message: message, Alert: true})
	s.world.Log().Warn("net", "msg", "peer link lost", "peer", peerID,
		"authority_lost", authorityLost, "remaining_peers", remaining)
	return false
}

// noticeDeparture changes no shared cursor: only a direct neighbour sees a lost
// link, at a moment its transport chose, so the coordinator crosses the departure
// and any other neighbour floods a notice. The identity returns to the pool when
// that crossing applies, or here for a participant with no cursor. dismissed is a
// seat this run dropped, which leaves for good.
func (s *NetworkSystem) noticeDeparture(peerID uint32, dismissed bool) {
	if _, crossed := s.participantSlot(peerID); !crossed {
		if r := s.world.Resources.Network; r != nil && r.OnDeparture != nil {
			r.OnDeparture(peerID, true)
		}
	}
	s.announceDeparture(peerID, 0, dismissed)
}

// announceDeparture crosses or forwards one departure, once. from is the link a
// notice arrived on, or zero when this instance observed the disconnect itself.
func (s *NetworkSystem) announceDeparture(peerID uint32, from uint32, dismissed bool) bool {
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
			&event.ParticipantDepartedPayload{Participant: peerID, Slot: slot, Dismissed: dismissed})
		return true
	}
	p := s.port()
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return true
	}
	body, err := json.Marshal(event.ParticipantDepartedPayload{Participant: peerID, Slot: slot, Dismissed: dismissed})
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
	if from == 0 || int(from) >= len(s.departed) || s.departed[from] {
		return
	}
	var p event.ParticipantDepartedPayload
	if err := json.Unmarshal(body, &p); err != nil {
		s.statDrop.Add(1)
		return
	}
	// A neighbour losing its edge does not retire a participant still linked here.
	if s.linked(p.Participant) {
		return
	}
	// A notice dismisses only a seat, which its holder dropped and any neighbour
	// passes on; a player leaves for good only by the authority's own word.
	if p.Dismissed {
		slot, ok := s.participantSlot(p.Participant)
		c, _ := s.world.Components.Cursor.GetComponent(s.world.Resources.Player.Slot(slot))
		p.Dismissed = ok && c.Holder != 0
	}
	if !s.announceDeparture(p.Participant, from, p.Dismissed) {
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

// Coordinator-only artifacts are accepted from the current authority, including
// a successor after handoff.
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
	if session := s.world.Session(); p == nil && s.world.FollowsJournal() && s.world.SessionShared() {
		peers, state = int(session.Peers), "down"
		if session.Live {
			state = "connected"
		}
	} else if p != nil {
		peers = p.PeerCount()
		if statePort, ok := p.(interface{ ConnectionState() link.ConnState }); ok {
			switch statePort.ConnectionState() {
			case link.StateConnected:
				state = "connected"
			case link.StateConnecting:
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

// dispatchMessage admits the steady-state message kinds the domain model defines:
// the D-3 artifact epoch and the D-13 owner-authored sync.
// Raw participant input is not one of them — a peer sends the resolved artifact,
// never the keystroke that produced it — so an unrecognised type is counted and
// discarded rather than translated.
func (s *NetworkSystem) dispatchMessage(from uint32, msg *link.Message) {
	if msg == nil {
		return
	}
	switch msg.Type {
	case network.MsgEvent:
		s.scheduleCrossings(from, msg.Payload)
	case network.MsgStateSync:
		s.scheduleCursorState(from, msg.Payload)
	case link.MsgBundle:
		err := link.Unbundle(msg.Payload, func(kind link.MessageType, body []byte) {
			if network.Bundles(kind) {
				s.dispatchMessage(from, &link.Message{Type: kind, Payload: body})
			} else {
				s.statDrop.Add(1)
			}
		})
		if err != nil {
			s.statDrop.Add(1)
		}
	case network.MsgDisconnect:
		s.receiveDeparture(from, msg.Payload)
	case network.MsgStateCorrection:
		s.receiveCorrection(from, msg.Payload)
	case network.MsgStateManifest, network.MsgStateRequest, network.MsgStateShard,
		network.MsgStateUnserved:
		s.receiveSelective(msg.Type, from, msg.Payload)
	case network.MsgSessionRestart:
		s.receiveSessionRestart(from, msg.Payload)
	case network.MsgRelayJoin, network.MsgRelayAdmit:
		// Neither floods: a relay asks on its own link and is answered on it.
		if r := s.world.Resources.Network; from != 0 && r != nil && r.OnRelayJoin != nil {
			r.OnRelayJoin(uint8(msg.Type), from, msg.Payload)
		}
	case network.MsgAuthorityReport, network.MsgAuthorityHandoff, network.MsgPeerList:
		// The address map travels with the succession it exists for: same term
		// gate, same flood, same session layer deduplicating by term and
		// participant. It says nothing about the world, so nothing here reads it.
		s.receiveAuthority(msg.Type, from, msg.Payload)
	default:
		s.statDrop.Add(1)
	}
}

// receiveCorrection queues a complete authoritative body for installation between
// ticks: draining holds the world lock an install takes itself. A malformed
// transfer resets the assembly, because every keyframe is self-sufficient and the
// recovery is the next one.
func (s *NetworkSystem) receiveCorrection(from uint32, body []byte) {
	if from == 0 || int(from) >= participantSlots {
		s.statDrop.Add(1)
		return
	}
	asm := &s.snapshot
	admitted, done, err := asm.AddChunk(body)
	if err != nil {
		*asm = link.SnapshotAssembly{}
		s.statDrop.Add(1)
		s.world.Log().Warn("converge", "msg", "correction chunk refused", "peer", from, "error", err.Error())
		return
	}
	if !admitted {
		s.statDuplicates.Add(1)
		return
	}
	// Passed on to the participants behind this instance, as every committed
	// artifact is: without it a relayed session would have crossings but no
	// corrections.
	if s.sendDown(from, network.MsgStateCorrection, body, 0, nil) {
		s.statRelayed.Add(1)
	}
	if !done {
		return
	}
	// The completed transfer is kept rather than cleared: it is what recognises a
	// copy that still arrives by a second path.
	tick, whole := asm.Result()
	s.statCorrections.Add(1)
	if r := s.world.Resources.Network; r != nil && r.OnCorrection != nil {
		r.OnCorrection(tick, whole)
	}
}

// receiveSelective queues a manifest, request, repair or refusal for the session
// layer, outside the world lock. None is flooded: a repair reaching a peer that did
// not ask names pages it retains nothing for, so forwarding a manifest is a
// retention decision the session layer makes (Corrections.forwardManifest).
func (s *NetworkSystem) receiveSelective(kind link.MessageType, from uint32, body []byte) {
	if from == 0 {
		s.statDrop.Add(1)
		return
	}
	if r := s.world.Resources.Network; r != nil && r.OnSelective != nil {
		r.OnSelective(uint8(kind), from, body)
	}
}

// receiveAuthority queues a succession frame for the session layer, which dedupes
// by term and participant and decides what to relay. These flood: a survivor two
// links from the lost authority learns of the loss only from reports crossing it.
func (s *NetworkSystem) receiveAuthority(kind link.MessageType, from uint32, body []byte) {
	if from == 0 || int(from) >= len(s.departed) || s.departed[from] {
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
	s.world.Log().Warn("net", "msg", "transport loss",
		"inbound_dropped", in, "outbound_refused", out)
	s.lastLostIn, s.lastLostOut = in, out
}

// publishLinkBytes exposes what the links carried, by kind before the stream's
// deflate and on the wire after it: the reading the netcode is planned by, which
// an in-process mesh, which deflates nothing, has no figures for.
func (s *NetworkSystem) publishLinkBytes(p engine.NetworkPort) {
	totals, ok := p.(interface{ LinkTotals() link.LinkTotals })
	if !ok {
		return
	}
	t := totals.LinkTotals()
	for k, stat := range s.statSentKind {
		stat.Store(int64(t.Framed[k]))
	}
	s.statWireOut.Store(int64(t.WireOut))
	s.statWireIn.Store(int64(t.WireIn))
}

// flushCrossings freezes the epoch under the producer lock, so concurrent crossings
// belong wholly to one marker. After an install moved the epoch past completedTick,
// resending a sent epoch would make receivers discard its frames as duplicates, so
// they wait for the next unsent one.
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
		Source: source, Run: s.localRun(), ProducedTick: epoch, Frames: pending, Leaf: s.leaf(),
	})
	if err != nil {
		s.statDrop.Add(int64(len(pending)))
		s.world.Log().Warn("net", "msg", "frames not encoded", "frames", len(pending), "error", err.Error())
		return
	}
	s.sendOwn(p, network.MsgEvent, body)
	s.statSent.Add(int64(len(pending)))
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
		s.world.Log().Warn("net", "msg", "frame not decoded", "error", err.Error())
		return
	}
	if batch.Source == 0 || int(batch.Source) >= len(s.epochs) {
		s.statDrop.Add(int64(max(1, len(batch.Frames))))
		return
	}
	if batch.Run != s.localRun() {
		s.statRefusedTick.Add(int64(max(1, len(batch.Frames))))
		return
	}
	local, authority := s.participantID(), s.authorityParticipant()
	raw := !batch.Committed && batch.Source != authority
	committing := raw && local == authority
	if raw && batch.Source != local {
		if !committing && !s.carries(from) {
			s.statUncarried.Add(1)
			return
		}
		s.noteDown(from)
	}
	switch {
	case batch.Source == local:
		// Committed and returned: those behind this instance need it once, it
		// measures the path to the authority, and it proves the link is one.
		if batch.Committed && s.noteOwnCommit(batch) {
			s.adoptRelay(from)
			s.forwardDown(from, network.MsgEvent, batch)
		}
		return
	case committing && from != s.ingress(batch.Source, from):
		// A raw epoch arrives on its producer's own link, or on its holder's for a bot
		// behind one. A copy on another of one already admitted is a duplicate; any
		// other is claimed.
		s.mu.Lock()
		dup := s.epochs[batch.Source].has(batch.ProducedTick, parameter.NetworkEpochWindow)
		s.mu.Unlock()
		if !dup {
			s.statForged.Add(int64(max(1, len(batch.Frames))))
		}
		return
	case raw && !committing:
		s.mu.Lock()
		fresh := s.rawEpochs[batch.Source].admit(batch.ProducedTick, parameter.NetworkEpochWindow)
		s.mu.Unlock()
		s.statUncommitted.Add(1)
		if fresh {
			s.relayRaw(network.MsgEvent, batch)
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

	next := s.localTick() + 1 // the tick a committing authority applies a late crossing at
	s.mu.Lock()
	admitted := s.epochs[batch.Source].admit(batch.ProducedTick, parameter.NetworkEpochWindow)
	installed, superseded, refused, overflowed, late, void, repeated := 0, 0, 0, 0, 0, 0, 0
	sourceFence := s.snapshotFences.Seq(link.PeerID(batch.Source))
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
			// A sequence this instance took already is a copy whatever epoch
			// carries it: a resend after a handoff, or a second path. It leaves
			// the committed copy with the void frames.
			if f.Frame.Seq != 0 && !s.seqs[batch.Source].admit(f.Frame.Seq, parameter.NetworkSequenceWindow) {
				repeated++
				f.Frame.Event = event.EventNone
				continue
			}
			a := barrierArtifact{
				frame: f.Frame, applyTick: f.ApplyTick, produced: batch.ProducedTick, source: batch.Source,
				origin: event.OriginNetwork, authored: batch.Source == s.authorityParticipant(),
			}
			if committing {
				if a.applyTick < next {
					late++
					if next-a.applyTick > parameter.NetworkCommitLateTicks {
						a.void, f.Frame.Event = true, event.EventNone
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
		if r := s.world.Resources.Network; r != nil && r.OnAuthorityAhead != nil && batch.ProducedTick > s.localTick() {
			r.OnAuthorityAhead()
		}
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
	if repeated > 0 {
		s.statDuplicates.Add(int64(repeated))
	}
	if overflowed > 0 {
		s.statScheduleFull.Add(int64(overflowed))
		s.world.Log().Warn("net", "msg", "barrier schedule full",
			"source", batch.Source, "produced_tick", batch.ProducedTick,
			"dropped", overflowed, "held", held, "bytes", s.scheduledBytes)
	}
	if superseded > 0 {
		s.statSuperseded.Add(int64(superseded))
		s.world.Log().Debug("converge", "msg", "snapshot refused crossings the installed world holds",
			"source", batch.Source, "produced_tick", batch.ProducedTick,
			"source_fence", sourceFence, "superseded", superseded)
	}

	if !admitted {
		s.statDuplicates.Add(1)
		return
	}
	// A void frame applies nowhere else, so it is not relayed; its fence closes here.
	batch.Frames = slices.DeleteFunc(batch.Frames, func(f event.ScheduledWireFrame) bool { return f.Frame.Event == event.EventNone })
	if committing {
		batch.Age = int64(next) - int64(batch.ProducedTick)
	}
	s.relayCommitted(from, committing, network.MsgEvent, batch)
}

// relayCommitted passes a committed copy down. The authority that commits one is
// its origin: the hop count restarts and every link takes it, the producer's
// included, which is how a producer learns what was committed.
func (s *NetworkSystem) relayCommitted(from uint32, committing bool, kind link.MessageType, batch event.WireBatch) {
	batch.Committed = true
	if committing {
		from, batch.Hops = 0, 0
	}
	s.forwardDown(from, kind, batch)
}

// noteDown records that a raw artifact arrived on from, or a participant joined
// through it: it is behind this instance until the epoch window passes in silence.
func (s *NetworkSystem) noteDown(from uint32) {
	if from != 0 && int(from) < len(s.downAt) {
		s.downAt[from] = max(1, s.localTick())
	}
}

// behind reports whether a link is behind this instance at tick.
func (s *NetworkSystem) behind(id uint32, tick uint64) bool {
	at := s.downAt[id]
	return at != 0 && tick <= at+parameter.NetworkEpochWindow
}

// carries reports whether this instance passes a participant's raw artifacts on
// toward the authority: one that joined through it or is behind it already, or any
// once its player turned relaying on, so nobody's traffic lands on a participant
// that did not offer to carry it.
func (s *NetworkSystem) carries(from uint32) bool {
	if from == 0 || int(from) >= len(s.served) {
		return false
	}
	r := s.world.Resources.Network
	return s.served[from] || s.behind(from, s.localTick()) || r != nil && r.Relaying != nil && r.Relaying()
}

// Carries is carries for the session layer, which answers a walking survivor with
// the record it follows only when it would carry that survivor's artifacts.
// Caller holds the world lock.
func (s *NetworkSystem) Carries(id uint32) bool { return s.carries(id) }

// Carry marks a participant that follows its host through this instance behind it,
// so the host's artifacts flow down to it before it has sent any of its own.
// Caller holds the world lock.
func (s *NetworkSystem) Carry(id uint32) { s.noteDown(id) }

// Reaches reports a path to the authority. Caller holds the world lock.
func (s *NetworkSystem) Reaches() bool { return s.up() != 0 }

// FollowThrough takes the link a handoff came through as this instance's path to
// the host it names, which it has no link of its own to. Caller holds the world lock.
func (s *NetworkSystem) FollowThrough(id uint32) {
	if id != 0 {
		s.relay = id
	}
}

// up is the link toward the authority: the authority itself, else the relay this
// instance joined through or adopted. Zero is no path.
func (s *NetworkSystem) up() uint32 {
	if authority := s.authorityParticipant(); authority != s.participantID() && s.linked(authority) {
		return authority
	}
	if s.relay != 0 && s.linked(s.relay) {
		return s.relay
	}
	return 0
}

// adoptRelay takes the link that returned this instance's own epoch committed as
// its path when it has none: the relay it joined through is gone, and this one
// carried its artifacts to the authority and back.
func (s *NetworkSystem) adoptRelay(from uint32) {
	if from == 0 || from == s.relay || s.up() != 0 {
		return
	}
	s.relay = from
	s.world.Log().Info("net", "msg", "relay adopted", "relay", from)
}

// Downstream lists the linked participants behind this instance: every link at
// the authority, else the ones it serves or that send it raw artifacts.
func (s *NetworkSystem) Downstream() []uint32 {
	meter, ok := s.port().(engine.LinkMeasuringPort)
	if !ok {
		return nil
	}
	peers := meter.Peers()
	if s.isCoordinator() {
		return peers
	}
	tick := s.localTick()
	return slices.DeleteFunc(slices.Clone(peers), func(id uint32) bool { return !s.behind(id, tick) })
}

// relayRaw passes a raw artifact on toward the authority, one hop further.
func (s *NetworkSystem) relayRaw(kind link.MessageType, batch event.WireBatch) {
	batch.Hops++
	if batch.Hops >= parameter.NetworkRelayHopLimit {
		s.statDrop.Add(1)
		s.world.Log().Warn("net", "msg", "relay hop limit reached",
			"source", batch.Source, "produced_tick", batch.ProducedTick)
		return
	}
	body, err := event.EncodeWireBatch(batch)
	if err != nil {
		s.statDrop.Add(int64(max(1, len(batch.Frames))))
		return
	}
	s.sendUp(kind, body)
}

// sendUp carries one raw artifact toward the authority on the link to it. With no
// such link and the authority alive this instance is behind a relay that left: it
// offers the artifact on every link not behind it, and adoptRelay takes whichever
// returns its epochs. With the authority lost it holds its own for the handoff to
// resend.
func (s *NetworkSystem) sendUp(kind link.MessageType, body []byte) {
	p := s.port()
	if to := s.up(); to != 0 {
		if p == nil || !p.Send(to, uint8(kind), body) {
			s.statDrop.Add(1)
		}
		return
	}
	meter, ok := p.(engine.LinkMeasuringPort)
	if !ok || s.statHostLost.Load() {
		s.statDrop.Add(1)
		return
	}
	tick := s.localTick()
	for _, id := range meter.Peers() {
		if !s.behind(id, tick) {
			p.Send(id, uint8(kind), body)
		}
	}
}

// sendDown passes one artifact to the participants behind this instance but the
// link it arrived on, every link at the authority; the link of source, when one is
// named, takes own in its place, or nothing when own is nil. Reports whether any
// took it. The port bundles epochs and syncs a link until a release (BundlingPort).
func (s *NetworkSystem) sendDown(except uint32, kind link.MessageType, body []byte, source uint32, own []byte) bool {
	p := s.port()
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return false
	}
	meter, ok := p.(engine.LinkMeasuringPort)
	if !ok {
		p.BroadcastExcept(except, uint8(kind), body)
		return true
	}
	tick, sent := s.localTick(), false
	for _, id := range meter.Peers() {
		if id == except || !(s.isCoordinator() || s.behind(id, tick)) {
			continue
		}
		b := body
		if id == source {
			if b = own; b == nil {
				continue
			}
		}
		sent = p.Send(id, uint8(kind), b) || sent
	}
	return sent
}

// sendOwn sends one of this instance's own artifacts: the authority's are committed
// as sent and go down every link, a guest's go up raw.
func (s *NetworkSystem) sendOwn(p engine.NetworkPort, kind link.MessageType, body []byte) {
	if s.isCoordinator() {
		p.Broadcast(uint8(kind), body)
		return
	}
	s.sendUp(kind, body)
}

// release sends each link what a drain or the tick's close left it, as one frame.
func (s *NetworkSystem) release(p engine.NetworkPort) {
	if b, ok := p.(engine.BundlingPort); ok {
		b.Release()
	}
}

// ingress is the link a participant's raw artifacts must reach the authority on: its
// own, else its holder's, so a relay answers for its subtree and nobody else can
// speak for it; else whichever link carried them. Caller MUST hold updateMutex.
func (s *NetworkSystem) ingress(source, from uint32) uint32 {
	if s.linked(source) {
		return source
	}
	var holder uint32
	s.world.Components.Cursor.Each(func(_ core.Entity, c *component.CursorComponent) bool {
		if c.PeerID == source {
			holder = c.Holder
			return false
		}
		return true
	})
	if holder != 0 && s.linked(holder) {
		return holder
	}
	return from
}

// linked reports whether a participant is on a direct link of this instance.
func (s *NetworkSystem) linked(id uint32) bool {
	switch p := s.port().(type) {
	case interface{ Connected(uint32) bool }:
		return p.Connected(id)
	case interface{ Peers() []uint32 }:
		return slices.Contains(p.Peers(), id)
	}
	return false
}

// observeAuthorityEpoch paces a guest against the authority. The sample is how
// late the epoch landed: this instance's completed tick minus the epoch's. The
// latest of a window trims, the earliest steps, and a step clears the window it
// has answered. The scheduler takes a step at the next tick and waits it out before
// the one after; a sample read while it waits counts the lateness the step answers.
func (s *NetworkSystem) observeAuthorityEpoch(produced uint64) {
	r := s.world.Resources.Network
	tick := s.localTick()
	serving := tick < s.paceSettled && tick+parameter.NetworkPaceServedTicks >= s.paceSettled
	if r == nil || s.participantID() == s.authorityParticipant() || serving {
		return
	}
	s.paceSamples[s.paceCount%len(s.paceSamples)] = int64(tick) - int64(produced)
	s.paceCount++
	if s.paceCount < len(s.paceSamples) {
		return
	}
	latest, earliest := slices.Max(s.paceSamples[:]), slices.Min(s.paceSamples[:])
	trim, step := paceDecision(latest, earliest)
	s.paceLatest, s.paceKnown = latest-step, true
	s.paceStale = step != 0 || latest-parameter.NetworkAheadTicks > parameter.NetworkPaceBandTicks
	s.statPaceLate.Store(latest)
	s.statPaceTrim.Store(int64(trim))
	r.Pace.Store(trim)
	if step != 0 {
		r.PaceStep.Add(step)
		s.statPaceSteps.Add(1)
		s.paceCount, s.paceSettled = 0, tick+parameter.NetworkPaceServedTicks
	}
}

// paceDecision turns a window's arrival offsets into a trim of the tick interval,
// in permille, and a whole-tick step; positive slows this instance down. Inside the
// band it rests. A step forward needs the earliest arrival past the threshold too:
// one jittered epoch is a trim, a whole window late is a clock to move at once. A
// step back is bounded by the debt the scheduler runs back to back.
func paceDecision(latest, earliest int64) (trimPermille int32, stepTicks int64) {
	late, lateAll := latest-parameter.NetworkAheadTicks, earliest-parameter.NetworkAheadTicks
	early := parameter.NetworkAheadTicks - parameter.NetworkPaceBandTicks - latest
	trim := func(ticks int64) int32 {
		return int32(min(ticks*parameter.NetworkPaceGainPermille, parameter.NetworkPacePermilleMax))
	}
	switch {
	case lateAll >= parameter.NetworkPaceStepTicks:
		return 0, lateAll
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
// same tick. The lead is the worst reading of the window: raising is immediate,
// lowering waits until every higher reading has aged out.
func (s *NetworkSystem) driveOwnLead(p engine.NetworkPort) {
	r := s.world.Resources.Network
	if r == nil || p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		return
	}
	target := s.ownLead()
	s.pathWorst = 0
	if s.leadCount == 0 {
		s.leadOf(r.BarrierDelayTicks) // the lead in force is the window's first reading
	}
	next := s.leadOf(target)
	if next == r.BarrierDelayTicks || next == s.leadPushed {
		return
	}
	s.leadPushed = next
	s.world.PushEventFull(event.EventPlayoutLead,
		&event.PlayoutLeadPayload{Ticks: next}, event.OriginSession, core.DomainPlayer)
}

// ownLead is this tick's reading of the lead: the age at which the authority
// committed this instance's newest epoch, which is the up path and the clock
// offset measured where they are paid, plus a tick for the authority to relay the
// crossing. A spike on a stream holds a burst of epochs that return in one tick,
// so the worst age since the last reading counts, not the newest alone. Until an
// epoch has come back the default stands; the authority waits one tick. The
// reading reaches a participant behind a relay on the same copy.
func (s *NetworkSystem) ownLead() uint64 {
	if s.participantID() == s.authorityParticipant() {
		return parameter.NetworkBarrierMinDelayTicks
	}
	if s.pathEpoch == 0 {
		return parameter.NetworkBarrierDelayTicks
	}
	ticks := max(s.pathAge, s.pathWorst) + parameter.NetworkRelaySlackTicks
	return uint64(min(max(ticks, parameter.NetworkBarrierMinDelayTicks), parameter.NetworkBarrierMaxDelayTicks))
}

// noteOwnCommit records an own epoch returned committed, reporting whether this is
// its first copy, and takes the newest one's age at commit, and the worst since
// the last reading, as the lead's.
func (s *NetworkSystem) noteOwnCommit(batch event.WireBatch) bool {
	s.mu.Lock()
	fresh := s.ownCommitted.admit(batch.ProducedTick, parameter.NetworkSequenceWindow)
	s.mu.Unlock()
	if batch.ProducedTick > s.pathEpoch {
		s.pathEpoch, s.pathAge = batch.ProducedTick, batch.Age
	}
	s.pathWorst = max(s.pathWorst, batch.Age)
	return fresh
}

// leadOf records this tick's reading and returns the worst of the window: a
// crossing that misses its lead costs a correction and one that clears it costs
// nothing, so a reading holds for NetworkBarrierRenegotiateTicks and the lead
// falls only as the higher readings age out, never a tick at a time.
func (s *NetworkSystem) leadOf(target uint64) uint64 {
	s.leadWindow[s.leadCount%uint64(len(s.leadWindow))] = target
	s.leadCount++
	return slices.Max(s.leadWindow[:min(s.leadCount, uint64(len(s.leadWindow)))])
}

// forwardDown passes one admitted committed epoch down unchanged but for the hop
// count: Source, ProducedTick and each frame's ApplyTick and sequence make the
// artifact identical everywhere, so a relay never restamps them. A leaf's own copy
// returns to it as its age alone, and its own sync not at all.
func (s *NetworkSystem) forwardDown(from uint32, kind link.MessageType, batch event.WireBatch) {
	if batch.Hops >= parameter.NetworkRelayHopLimit {
		s.statDrop.Add(1)
		s.world.Log().Warn("net", "msg", "relay hop limit reached",
			"source", batch.Source, "produced_tick", batch.ProducedTick)
		return
	}
	batch.Hops++
	body, err := event.EncodeWireBatch(batch)
	var own []byte
	if err == nil && batch.Leaf && batch.Committed && kind == network.MsgEvent {
		batch.Frames = nil
		own, err = event.EncodeWireBatch(batch)
	}
	if err != nil {
		s.statDrop.Add(int64(max(1, len(batch.Frames))))
		s.world.Log().Warn("net", "msg", "relay not encoded", "error", err.Error())
		return
	}
	source := uint32(0)
	if batch.Leaf && batch.Committed {
		source = batch.Source
	}
	if s.sendDown(from, kind, body, source, own) {
		s.statRelayed.Add(1)
	}
}

// leaf reports that nobody is behind this instance, so a committed copy of its own
// need not bring its frames back.
func (s *NetworkSystem) leaf() bool { return len(s.Downstream()) == 0 }

// applyDue publishes due artifacts in the same source/sequence order everywhere.
func (s *NetworkSystem) applyDue(nextTick uint64) {
	s.writeDueStates(nextTick)
	s.mu.Lock()
	due := s.due[:0]
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
	retain := s.retain[:0]
	for _, a := range due {
		if a.void {
			s.noteAppliedFrom(a.source, a.frame.Seq)
			continue
		}
		et, domain := a.frame.Event, a.frame.Domain
		payload, err := a.frame.Decode()
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		if !s.admissibleFromSource(et, payload, a.source, a.authored) {
			s.statForged.Add(1)
			s.world.Log().Warn("session", "msg", "artifact refused",
				"peer", a.source, "event", event.GetEventName(et), "apply_tick", a.applyTick)
			continue
		}
		if a.applyTick < nextTick {
			// Committed frames reach a paced guest before their tick, so this counts
			// a guest running ahead of its band; its next correction repairs it.
			s.statLate.Add(1)
		}
		event.BoundTick(payload, a.applyTick)
		ev := event.GameEvent{Type: et, Payload: payload, Origin: a.origin, Domain: domain}
		if a.source == localSource {
			// The local fence closes at dispatch: a capture read between the
			// schedule and the handlers must not claim an effect the world does
			// not yet hold. The sequence also journals it, whatever produced it.
			ev.CrossingSeq = a.frame.Seq
		}
		s.world.Resources.Event.Queue.PushReady(ev)
		if barrierBound(et) || a.source != localSource {
			retain = append(retain, a)
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
	s.retainApplied(retain)
	s.due, s.retain = due[:0], retain[:0]
	s.statAppliedLocal.Add(int64(local))
	s.statAppliedPeer.Add(int64(peer))
	s.statPeerArtifacts.Store(int64(peer))
	s.statPeerApplied.Store(peer != 0)
	s.statRecv.Add(int64(peer))
}

// Only the authority may author roster changes, which create or destroy shared
// identities, and a hold, which stops everyone: the one that sent the frame, or the
// one it applies under — a dropped guest sees the link close before its dismissal's
// tick and takes the term itself. Nobody acts as another participant's cursor
// (crossingActors). Other source authentication requires authenticated peers.
func (s *NetworkSystem) admissibleFromSource(et event.EventType, payload any, source uint32, authored bool) bool {
	switch et {
	case event.EventParticipantJoined, event.EventParticipantDeparted, event.EventGameHold, event.EventFSMTransition:
		return authored || source == s.authorityParticipant()
	}
	actor, ok := crossingActors[et]
	return !ok || s.actsAsOwn(source, actor(s.world, payload))
}

// actsAsOwn reports whether e is no other participant's cursor: source's own, or
// one no participant owns. Ownership is shared state, so every instance refuses the
// same artifacts at the same tick. Caller MUST hold updateMutex.
func (s *NetworkSystem) actsAsOwn(source uint32, e core.Entity) bool {
	c, ok := s.world.Components.Cursor.GetComponent(e)
	return !ok || c.PeerID == 0 || c.PeerID == source
}

// crossingActors reads the cursor a crossing acts as, for each type whose payload
// names one: by entity, or by roster slot for a captured key's input.
var crossingActors = map[event.EventType]func(*engine.World, any) core.Entity{
	event.EventCursorMoveRequest:               actorEntity(func(p *event.CursorMoveRequestPayload) core.Entity { return p.Entity }),
	event.EventGoldJumpRequest:                 actorEntity(func(p *event.GoldJumpRequestPayload) core.Entity { return p.Entity }),
	event.EventCursorDefeatState:               actorEntity(func(p *event.CursorDefeatStatePayload) core.Entity { return p.Entity }),
	event.EventEmberSpent:                      actorEntity(func(p *event.HeatBurstPayload) core.Entity { return p.Entity }),
	event.EventExplosionRequest:                actorEntity(func(p *event.ExplosionRequestPayload) core.Entity { return p.Entity }),
	event.EventExplosionBatchRequest:           actorEntity(func(p *event.ExplosionBatchRequestPayload) core.Entity { return p.Entity }),
	event.EventCombatAttackAreaCrossingRequest: actorEntity(func(p *event.CombatAttackAreaRequestPayload) core.Entity { return p.OwnerEntity }),
	event.EventCompositeMemberDestroyed:        actorEntity(func(p *event.CompositeMemberDestroyedPayload) core.Entity { return p.Entity }),
	event.EventDrainDefeated:                   actorEntity(func(p *event.DrainDefeatedPayload) core.Entity { return p.Entity }),
	event.EventInputPressed:                    actorSlot(func(p *event.InputPayload) uint8 { return p.Slot }),
	event.EventInputReleased:                   actorSlot(func(p *event.InputPayload) uint8 { return p.Slot }),
	event.EventInputReport:                     actorSlot(func(p *event.InputReportPayload) uint8 { return p.Slot }),
}

func actorEntity[T any](field func(*T) core.Entity) func(*engine.World, any) core.Entity {
	return func(_ *engine.World, p any) core.Entity {
		if v, ok := p.(*T); ok {
			return field(v)
		}
		return 0
	}
}

func actorSlot[T any](field func(*T) uint8) func(*engine.World, any) core.Entity {
	return func(w *engine.World, p any) core.Entity {
		if v, ok := p.(*T); ok {
			return w.Resources.Player.Slot(field(v))
		}
		return 0
	}
}

// publishBarrierTelemetry reports playout lead and epochs that ran before a marker.
func (s *NetworkSystem) publishBarrierTelemetry(nextTick uint64, p engine.NetworkPort) {
	if p == nil || !p.IsRunning() || p.PeerCount() == 0 {
		s.statPeerLag.Store(0)
		return
	}
	// The authority waits on nobody: it commits a late crossing at its next tick
	// (multi-player.md §3.5), so no peer epoch is required of it and it cannot fall
	// behind one.
	if s.isCoordinator() {
		s.statPeerLag.Store(0)
		s.statLag.Store(0)
		s.statStale.Store(false)
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

// publishStaleness reports how far behind the newest peer tick this instance
// stands (network.lag_ticks: on a paced guest, the margin its pacing holds) and
// whether it is out of step (network.stale): pacing's own verdict, read off the
// same window and band that pace it, so the badge derives nothing of its own. Ahead
// beyond the band, committed crossings reach it late; behind by a step, its lead
// grows. Neither gates play.
func (s *NetworkSystem) publishStaleness(nextTick uint64) {
	newest := s.NewestPeerEpoch()
	lag := uint64(0)
	if local := nextTick; newest > local {
		lag = newest - local
	}
	s.statLag.Store(int64(lag))
	s.statStale.Store(s.paceKnown && s.paceStale)
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
	r := s.world.Resources.Network
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}
		state := s.readCursorState(cursor, uint8(i))
		if r != nil && r.OnOwnState != nil && uint8(i) == s.world.Resources.Player.LocalSlot() {
			r.OnOwnState(state)
		}
		// The authority's own sync is committed as sent, to its next tick; a guest's
		// goes raw, for the authority to commit.
		f := event.ScheduledWireFrame{Frame: stateFrame(state)}
		if s.isCoordinator() {
			f.ApplyTick = s.localTick() + 1
		}
		body, err := event.EncodeWireBatch(event.WireBatch{
			Source: s.participantID(), Run: s.localRun(), Frames: []event.ScheduledWireFrame{f}, Leaf: s.leaf(),
		})
		if err != nil {
			s.statDrop.Add(1)
			continue
		}
		s.sendOwn(p, network.MsgStateSync, body)
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
	if batch.Run != s.localRun() {
		s.statRefusedTick.Add(1)
		return
	}
	local, authority := s.participantID(), s.authorityParticipant()
	raw := !batch.Committed && batch.Source != authority
	committing := raw && local == authority
	own := batch.Source == local
	switch {
	case own && !batch.Committed:
		return
	case committing && from != s.ingress(batch.Source, from):
		s.statForged.Add(1)
		return
	}
	if raw && !own {
		if !committing && !s.carries(from) {
			s.statUncarried.Add(1)
			return
		}
		s.noteDown(from)
	}
	fresh := batch.Frames[:0]
	for _, f := range batch.Frames {
		payload, err := f.Frame.Decode()
		p, ok := payload.(*event.CursorStatePayload)
		if err != nil || f.Frame.Event != event.EventCursorStateSync || !ok || int(p.Slot) >= parameter.MaxPlayers {
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
		if (!raw || committing) && !own { // an own sync is only forwarded
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
		s.relayRaw(network.MsgStateSync, batch)
		return
	}
	s.relayCommitted(from, committing, network.MsgStateSync, batch)
}

// writeDueStates publishes the owner-authored syncs due by nextTick, in the order
// every instance writes them, ahead of the crossings due with them. A published sync
// is a journal record, which is what lets a participant's run replay; the write is
// the dispatch. Caller holds the world lock.
func (s *NetworkSystem) writeDueStates(nextTick uint64) {
	if len(s.states) == 0 {
		return
	}
	due := s.dueStates[:0]
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
	queue := s.world.Resources.Event.Queue
	for _, st := range due {
		if !s.actsAsOwn(st.source, st.payload.Entity) {
			s.statForged.Add(1)
			continue
		}
		queue.PushReady(event.GameEvent{
			Type: event.EventCursorStateSync, Payload: st.payload,
			Origin: event.OriginNetwork, Domain: core.DomainPlayer,
		})
	}
	clear(due)
	s.dueStates = due[:0]
}

// writeCursorState applies one sync, dropping a stale or misaddressed one. The
// payload names both an entity and a slot; they must agree, because the entity
// selects the cells written and the slot keys the sequence that decides whether to
// write them at all. A disagreement would age one participant's state under
// another's sequence, so it is dropped rather than reconciled.
func (s *NetworkSystem) writeCursorState(p *event.CursorStatePayload) bool {
	cursor := s.world.ResolveCursor(p.Entity)
	if cursor == 0 || s.world.SimulatesLocally(cursor) || int(p.Slot) >= parameter.MaxPlayers ||
		s.world.Components.Wraith.HasEntity(cursor) {
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

// networkState is the barrier and session bookkeeping a replay copy carries. The
// correction reassembly and the drain window belong to a transport, which a replay
// copy only stands in for.
type networkState struct {
	crossings                                  []event.ScheduledWireFrame
	scheduled, applied                         []barrierArtifact
	scheduledBytes, suffixBytes                int
	epochs, rawEpochs, seqs                    [participantSlots]window
	ownCommitted                               window
	productionEpoch, crossSeq, appliedCrossSeq uint64
	appliedAhead                               map[uint64]struct{}
	localSource, snapshotAuthority, relay      uint32
	delayTicks, lastApplyTick, snapshotFloor   uint64
	encodeErr, suffixDropped                   int64
	barrierActive                              bool
	appliedPeerSeq, downAt                     [participantSlots]uint64
	snapshotFences                             link.CrossingFences
	syncSeq, ticks, lostSeq                    uint64
	lastSync, stateSeen, rawStateSeen          [parameter.MaxPlayers]uint64
	states                                     []pendingState
	departed, expiring                         [participantSlots]bool
	suffix                                     []localCrossing
	paceSamples                                [parameter.NetworkPaceWindow]int64
	paceCount                                  int
	paceLatest, pathAge                        int64
	paceKnown, paceStale                       bool
	paceSettled, pathEpoch                     uint64
	leadWindow                                 [parameter.NetworkBarrierRenegotiateTicks]uint64
	leadCount, leadPushed                      uint64
	lastLostIn, lastLostOut                    uint64
}

func (s *NetworkSystem) CopyState() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := networkState{
		crossings: slices.Clone(s.crossings), scheduled: slices.Clone(s.scheduled), applied: slices.Clone(s.applied),
		scheduledBytes: s.scheduledBytes, suffixBytes: s.suffixBytes, epochs: s.epochs, rawEpochs: s.rawEpochs,
		seqs: s.seqs, ownCommitted: s.ownCommitted,
		productionEpoch: s.productionEpoch, crossSeq: s.crossSeq, appliedCrossSeq: s.appliedCrossSeq,
		appliedAhead: maps.Clone(s.appliedAhead), localSource: s.localSource, snapshotAuthority: s.snapshotAuthority,
		delayTicks: s.delayTicks, lastApplyTick: s.lastApplyTick, snapshotFloor: s.snapshotFloor,
		encodeErr: s.encodeErr, suffixDropped: s.suffixDropped, barrierActive: s.barrierActive.Load(),
		appliedPeerSeq: s.appliedPeerSeq, downAt: s.downAt, relay: s.relay, snapshotFences: slices.Clone(s.snapshotFences),
		syncSeq: s.syncSeq, ticks: s.ticks, lostSeq: s.lostSeq, lastSync: s.lastSync, stateSeen: s.stateSeen,
		rawStateSeen: s.rawStateSeen, states: clonePendingStates(s.states), departed: s.departed, expiring: s.expiring,
		suffix:      slices.Clone(s.suffix),
		paceSamples: s.paceSamples, paceCount: s.paceCount, pathAge: s.pathAge,
		paceLatest: s.paceLatest, paceKnown: s.paceKnown, paceStale: s.paceStale, paceSettled: s.paceSettled, pathEpoch: s.pathEpoch,
		leadWindow: s.leadWindow, leadCount: s.leadCount, leadPushed: s.leadPushed,
		lastLostIn: s.lastLostIn, lastLostOut: s.lastLostOut,
	}
	return c
}

func (s *NetworkSystem) RestoreState(v any) error {
	c := v.(networkState)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crossings, s.scheduled, s.applied = slices.Clone(c.crossings), slices.Clone(c.scheduled), slices.Clone(c.applied)
	s.scheduledBytes, s.suffixBytes, s.epochs, s.rawEpochs = c.scheduledBytes, c.suffixBytes, c.epochs, c.rawEpochs
	s.seqs, s.ownCommitted = c.seqs, c.ownCommitted
	s.productionEpoch, s.crossSeq, s.appliedCrossSeq = c.productionEpoch, c.crossSeq, c.appliedCrossSeq
	s.appliedAhead, s.localSource, s.snapshotAuthority = maps.Clone(c.appliedAhead), c.localSource, c.snapshotAuthority
	if s.appliedAhead == nil {
		s.appliedAhead = make(map[uint64]struct{})
	}
	s.delayTicks, s.lastApplyTick, s.snapshotFloor = c.delayTicks, c.lastApplyTick, c.snapshotFloor
	s.encodeErr, s.suffixDropped = c.encodeErr, c.suffixDropped
	s.barrierActive.Store(c.barrierActive)
	s.appliedPeerSeq, s.downAt, s.relay, s.snapshotFences = c.appliedPeerSeq, c.downAt, c.relay, slices.Clone(c.snapshotFences)
	s.syncSeq, s.ticks, s.lostSeq, s.lastSync = c.syncSeq, c.ticks, c.lostSeq, c.lastSync
	s.stateSeen, s.rawStateSeen, s.states, s.departed = c.stateSeen, c.rawStateSeen, clonePendingStates(c.states), c.departed
	s.expiring = c.expiring
	s.suffix = slices.Clone(c.suffix)
	s.paceSamples, s.paceCount, s.pathAge = c.paceSamples, c.paceCount, c.pathAge
	s.paceLatest, s.paceKnown, s.paceStale, s.paceSettled, s.pathEpoch = c.paceLatest, c.paceKnown, c.paceStale, c.paceSettled, c.pathEpoch
	s.leadWindow, s.leadCount, s.leadPushed = c.leadWindow, c.leadCount, c.leadPushed
	s.lastLostIn, s.lastLostOut = c.lastLostIn, c.lastLostOut
	return nil
}

func clonePendingStates(states []pendingState) []pendingState {
	out := slices.Clone(states)
	for i := range out {
		if p := out[i].payload; p != nil {
			cp := *p
			cp.WeaponCharges, cp.WeaponCooldown = slices.Clone(p.WeaponCharges), slices.Clone(p.WeaponCooldown)
			out[i].payload = &cp
		}
	}
	return out
}
