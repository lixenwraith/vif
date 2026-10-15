package parameter

import "time"

// Transport cadence. Every participant stamps its own crossings with a lead its own
// link to the authority asks for; owner-authored state is a periodic value sync.
const (
	// NetworkBarrierDelayTicks is the lead a guest stamps with before its link to the
	// authority is measured, and the tick budget a join is admitted and a guest is
	// called stale against.
	NetworkBarrierDelayTicks = 3

	// NetworkBarrierMinDelayTicks and NetworkBarrierMaxDelayTicks bound a stamped
	// lead. One tick is the authority's own: its crossings wait only for the tick
	// that produced them to close. One second is where a lead stops being a buffer.
	NetworkBarrierMinDelayTicks = 1
	NetworkBarrierMaxDelayTicks = 20

	// NetworkBarrierRenegotiateTicks is how long a lower measurement holds before a
	// participant's own lead follows it down, to the highest reading of that
	// window; raising is immediate.
	NetworkBarrierRenegotiateTicks = SnapshotFloorKeyframeTicks

	// NetworkRelaySlackTicks is the tick a guest's lead leaves the authority to
	// relay its crossing to the others before they reach the tick it names.
	NetworkRelaySlackTicks = 1

	// NetworkAheadTicks is how late, in ticks, a guest lets the authority's epochs
	// reach it so its own crossings apply that much sooner. Everyone else's arrive
	// that late on it too, and are repaired there, so only it pays.
	NetworkAheadTicks = 0

	// NetworkPaceBandTicks is the dead zone a guest's pacing rests in: the latest
	// authority epoch in a window lands between this many ticks early and
	// NetworkAheadTicks late.
	NetworkPaceBandTicks = 1

	// NetworkPaceWindow is how many authority epochs one pace decision reads; the
	// latest of them trims and the earliest steps, so one jittered arrival never
	// moves whole ticks. It is also how many round trips, probed or relayed, the
	// own lead's worst reading covers.
	NetworkPaceWindow = 10

	// NetworkPaceGainPermille and NetworkPacePermilleMax trim a guest's tick interval
	// per tick of error, and bound it: the world runs up to 8% fast or slow while a
	// guest drifts back into its band.
	NetworkPaceGainPermille = 20
	NetworkPacePermilleMax  = 80

	// NetworkPaceStepTicks is the error a guest closes in one step rather than by
	// trimming, when every epoch of the window shows it: a lobby that released it a
	// round trip early is corrected at once, a single jittered arrival only trims.
	// A backwards step is bounded by the debt the scheduler will run back to back.
	NetworkPaceStepTicks     = 3
	NetworkPaceStepBackTicks = 2

	// NetworkPaceServedTicks is how many local ticks a step takes to be served: the
	// scheduler adds it after the next tick and waits it out before the one after.
	NetworkPaceServedTicks = 2

	// NetworkCommitLateTicks is how late a guest's crossing may reach the authority
	// and still be committed, at the authority's next tick. Past it the crossing is
	// void: an action that old would land on a world its player no longer saw.
	NetworkCommitLateTicks = 20

	// NetworkHoldTicks is how far ahead of the clock a correction or manifest is
	// worth waiting for; further ahead is a guest behind, which takes it at once.
	NetworkHoldTicks = NetworkPaceStepTicks + NetworkPaceBandTicks

	// NetworkSlowWindow, NetworkSlowLatePerSecond and NetworkSlowBytesPerSecond are
	// the default eviction policy for a participant too slow to keep up: over the
	// window, its epochs reached the authority late this often and its
	// corrections cost this much. Both must hold; zero disables either.
	NetworkSlowWindow         = 20 * time.Second
	NetworkSlowLatePerSecond  = 5.0
	NetworkSlowBytesPerSecond = 8 << 10

	// NetworkIngressBytesPerSecond is what the authority accepts from a link over
	// the same window, per participant the link carries. A guest drains about a
	// sixth of it while playing; past it, the link and its subtree are evicted.
	NetworkIngressBytesPerSecond = 64 << 10

	// NetworkSyncTicks is the period between owner-authored state syncs (D-13).
	// One cursor's payload is small; this keeps remote presentation responsive.
	NetworkSyncTicks = 6

	// NetworkDigestTicks is the cadence of runtime D-11 parity probes. Adjacent
	// peers compare the same completed tick; equality on every mesh edge implies
	// equality across the connected session graph.
	NetworkDigestTicks = NetworkSyncTicks

	// NetworkResyncNoticeTicks keeps the green SYNCED acknowledgement visible for
	// one second after the last mismatching peer agrees again.
	NetworkResyncNoticeTicks = 20

	// NetworkDesyncSamples is how many consecutive disagreeing digest samples make a
	// divergence a report rather than a blip. One sample can disagree while the two
	// instances still agree about the run: an artifact that arrived after its apply
	// tick lands on one side a tick late, and the next sample finds them equal again.
	// Two samples report one 300ms digest interval after the first disagreement.
	NetworkDesyncSamples = 2

	// NetworkDivergedRecordsLogged bounds how many differing snapshot record names
	// one diagnosis carries. A real divergence names one or two; a reset or a lost
	// artifact names most of the surface, and the first few plus a count says that
	// just as well as a hundred would.
	NetworkDivergedRecordsLogged = 6

	// NetworkDivergedSamples is where a divergence stops being transient. Nothing
	// re-derives the missing artifact, so past this point the two runs are different
	// games and the participant needs the session again rather than a warning.
	NetworkDivergedSamples = 5

	// NetworkDrainWindow bounds one tick's inbound translation, so a flooding
	// peer cannot stretch a tick without bound
	NetworkDrainWindow = 64

	// NetworkRelayHopLimit bounds how far one artifact travels. Per-source epoch
	// dedupe is what actually terminates flooding; this is the backstop that keeps a
	// bug in it from becoming unbounded traffic, and 16 exceeds the diameter of any
	// graph MaxPlayers participants can form.
	NetworkRelayHopLimit = 16

	// SessionVacantReset is how long an unbounded dedicated host holds an emptied,
	// parked session before starting a fresh one: a world nobody came back to is not
	// the one the next guest should be dropped into, and a minute covers a reconnect.
	// Wall time on purpose: the clock it is measured beside is stopped.
	SessionVacantReset = time.Minute

	// SessionRejoinWindow and SessionRejoinInterval bound a participant redialling
	// the coordinator that told it the session is rebuilding on another scenario. The
	// window covers a world build, so it is generous; the interval is what a refused
	// dial costs. Wall time: neither run has a clock while this is measured.
	SessionRejoinWindow   = 30 * time.Second
	SessionRejoinInterval = 250 * time.Millisecond

	// NetworkJoinReadyTimeout bounds how long a coordinator waits for a mid-run
	// joiner to install the world it was sent and confirm it, a link and install
	// bound: a participant that needs longer cannot keep the playout lead, and its
	// crossings would arrive after the ticks they name.
	NetworkJoinReadyTimeout = 5 * time.Second

	// NetworkJoinLagTicks is how far behind the session, in ticks, a freshly
	// installed participant may land and still be admitted. It is the default playout
	// lead: N ticks behind, a crossing stamped for Q+lead meets a session at Q+N, late
	// by N-lead. Fixed rather than the live lead, so one participant is not admitted
	// and refused a second apart.
	NetworkJoinLagTicks = NetworkBarrierDelayTicks

	// NetworkJoinCatchUpTicks bounds the ticks a joining participant may simulate to
	// reach the session's tick. The gap is the transfer and install cost, a function
	// of world size rather than session length; this ceiling is far above it, so a
	// pathological link fails the join instead of stalling inside it.
	NetworkJoinCatchUpTicks = 200

	// SnapshotCorrectionTicks is the nominal correction cadence, in ticks: 5 Hz at the
	// 50 ms tick. It is low because both instances run the same deterministic
	// simulation, so a correction repairs what an unmodelled input changed. Each peer's
	// cadence follows its link and demand within SnapshotCadenceMinTicks and
	// SnapshotCadenceMaxTicks, never past the convergence floor.
	SnapshotCorrectionTicks = 4

	// SnapshotCadenceMinTicks is the fastest a correction may be published to one
	// peer — 10 Hz — and the freshness a participant with a busy neighbourhood or a
	// drifting prediction is given when its link can carry it.
	SnapshotCadenceMinTicks = 2

	// SnapshotCadenceMaxTicks is the slowest adaptation may go. It equals the
	// convergence floor deliberately: at the very bottom the schedule *is* the
	// floor — one whole authoritative world per floor window and nothing else —
	// and a cadence slower than that could not honour the floor at any keyframe
	// interval.
	SnapshotCadenceMaxTicks = SnapshotFloorKeyframeTicks

	// SnapshotCadenceQuietTicks is where a participant with nothing relevant near
	// it and a prediction the last correction did not have to move settles. It is
	// what pays for SnapshotCadenceMinTicks somewhere else on the same uplink:
	// relevance is a budget reallocation, not a free increase.
	SnapshotCadenceQuietTicks = 8

	// SnapshotUrgentDriftPercent and SnapshotUrgentRelevancePercent are where a peer
	// stops being ordinary, as percentages because a count is about the world rather
	// than the participant: drift is how far the far end's correction magnitude rises
	// above its own recent level, relevance how far its share stands above the
	// session's mean (see linkpace.Bounds).
	SnapshotUrgentDriftPercent     = 50
	SnapshotUrgentRelevancePercent = 100

	// SnapshotRelevanceRadius is how far from a participant's cursor a shared
	// entity is still that participant's business, in cells. It is a scheduling
	// hint and never a filter: a correction still carries the whole world, so a
	// wrong or stale radius costs a correction sent sooner than it was needed and
	// can never cost correctness.
	SnapshotRelevanceRadius = 24

	// SnapshotKeyframeCorrections is how many deltas the host sends between whole
	// captures at the nominal operating point: the longest a participant that missed a
	// keyframe, or just arrived, waits to apply anything (two seconds), and the loss
	// bound, since a keyframe supersedes every delta before it. Under pressure the
	// controller stretches it toward SnapshotKeyframeMaxCorrections.
	SnapshotKeyframeCorrections = 10

	// SnapshotKeyframeMinCorrections is one: every correction a whole capture.
	// That is not a degenerate case but the cheapest schedule that honours the
	// floor, and it is where a link at the very bottom of its range sits.
	SnapshotKeyframeMinCorrections = 1

	// SnapshotKeyframeMaxCorrections bounds how far the interval may stretch. Past
	// this the recovery from one lost keyframe stops being a bounded wait a player
	// would sit through, whatever the floor allows.
	SnapshotKeyframeMaxCorrections = 30

	// SnapshotFloorKeyframeTicks is the convergence floor: the most ticks a
	// participant may go without a whole authoritative world. Cadence times keyframe
	// interval never exceeds it; a link that cannot carry one world per floor window
	// is refused at admission and reported mid-session. Three seconds is 1.5 times
	// the nominal keyframe period, the room adaptation needs.
	SnapshotFloorKeyframeTicks = 60

	// SnapshotLinkUtilisation is the share of measured link capacity the
	// correction cadence may spend. The rest is headroom for the crossing epochs,
	// the owner-authored syncs and the digests, which travel on the same link and
	// are not in the controller's cost model.
	SnapshotLinkUtilisation = 0.75

	// SnapshotBulkQueue is how long a link's undelivered bytes may take to drain,
	// past the round trip and probe interval every backlog reading includes, before
	// no further correction chunk joins them. SnapshotBulkQueueBytes floors it, and
	// is the whole allowance while the link's rate is unmeasured.
	SnapshotBulkQueue      = 100 * time.Millisecond
	SnapshotBulkQueueBytes = 8 << 10

	// SnapshotKeyframeDrain is the longest a link may take to deliver a whole world
	// sent on schedule. A slower one meets the floor with proofs and takes a whole
	// world on request, and its operating point is reported constrained.
	SnapshotKeyframeDrain = 500 * time.Millisecond

	// SnapshotCorrectionChunkBytes is how much of a correction body one frame
	// carries mid-session. A body is released a chunk at a time as its link drains,
	// so an epoch behind it waits for one chunk rather than for a whole world.
	SnapshotCorrectionChunkBytes = 4 << 10

	// SnapshotCadenceRecoverTicks and SnapshotCadenceRecoverKeyframe bound how
	// fast the operating point may move back toward nominal. Degradation is
	// immediate — a link that has narrowed has already narrowed — and recovery is
	// stepped, so one sample taken during a quiet moment cannot restore a cadence
	// the link has not regained.
	SnapshotCadenceRecoverTicks    = 2
	SnapshotCadenceRecoverKeyframe = 2

	// NetworkProbeInterval is how often a link measures a real round trip, in wall
	// time because it measures the wire: a paused instance still has a link. Five
	// small probes a second are nothing beside the correction stream, and with the
	// estimator's 1/8 smoothing a link change is steerable in about 1.5 seconds.
	NetworkProbeInterval = 200 * time.Millisecond

	// NetworkProbeLostAfter is how long a link may go without any echo before its
	// probes count as lost. A stream loses nothing, so this is a link that stopped.
	NetworkProbeLostAfter = time.Second

	// SnapshotFloorGraceTicks is how far past the floor a receiver waits before
	// calling its own condition unrecoverable: the floor bounds publication, and a
	// receiver also pays the transfer and the install. One nominal keyframe period is
	// the smallest margin that cannot fire on a healthy link.
	SnapshotFloorGraceTicks = SnapshotCorrectionTicks * SnapshotKeyframeCorrections

	// SnapshotStaleTicks is how far behind the session's newest observed tick this
	// instance may stand before the staleness indicator blames the link. It is the
	// default playout lead, as NetworkJoinLagTicks is: past it this participant's
	// crossings reach the host after the tick they name. Fixed rather than live, so
	// the indicator does not flicker with the lead it reads.
	SnapshotStaleTicks = NetworkBarrierDelayTicks

	// SnapshotCorrectionQueue bounds the corrections one instance may hold
	// un-applied. A correction supersedes every earlier one, so the queue is
	// small on purpose and drops the *oldest* when it overflows: falling behind
	// should cost freshness, never the newest authority.
	SnapshotCorrectionQueue = 4

	// === Hash-guided selective correction ===

	// SnapshotManifestPageRows is how many rows a manifest page holds at the nominal
	// partition. A page is the unit of proof and repair: larger pages compare more
	// cheaply and repair more coarsely. Thirty-two keeps one diverged entity from
	// dragging a hundred unrelated ones onto the wire, and a healthy page list short.
	SnapshotManifestPageRows = 32

	// SnapshotManifestMaxPages bounds a section's page count whatever its row
	// count. It is what keeps the page vector in a request bounded by the protocol
	// rather than by the world: at MaxPlayers-scale worlds a section is at most
	// this many hashes, so the descent from a section to its pages costs a fixed
	// amount of wire however large the store grows.
	SnapshotManifestMaxPages = 64

	// SnapshotManifestRetention is how many recent captures and manifests a host
	// keeps so it can answer a request naming an earlier one. A peer's indexes are
	// spaced so its descent, two round trips, stays inside them; a request naming a
	// tick let go was overtaken by a later index and is not answered.
	SnapshotManifestRetention = 4

	// SnapshotShardBytesMax bounds one repair. A shard set larger than this is not
	// a repair, it is a capture with extra steps: the host sends a keyframe
	// instead, which is smaller once the mismatch is that wide because it carries
	// no per-page identity or proof. It is also what keeps a repair inside one
	// transport frame.
	SnapshotShardBytesMax = 48 << 10

	// SnapshotManifestSilenceCorrections is how many manifests, beyond those its
	// round trip and backlog hold in flight, a peer may leave unanswered before it
	// is sent whole bodies again. Every manifest is answered, so silence means the
	// answer cannot get back — a peer reached only by relay, or an uplink that
	// failed — and neither is repaired selectively.
	SnapshotManifestSilenceCorrections = 3

	// SnapshotReplayTicks, SnapshotReplayRecords and SnapshotReplayBytes bound the
	// suffix of its own crossings an instance retains for replay after a correction:
	// the useful span (one convergence floor), a participant acting far faster than
	// the cadence, and a pathological payload. A replay that needs a dropped record
	// is refused whole rather than run partial, since a partial replay is a guess.
	SnapshotReplayTicks   = SnapshotFloorKeyframeTicks
	SnapshotReplayRecords = 512
	SnapshotReplayBytes   = 256 << 10

	// PredictionLedgerMax bounds the shared derivations a guest holds waiting for an
	// authoritative world to prove them. Unlike the replay suffix it overflows by
	// releasing the oldest unproved: paying a reward a cadence early beats losing it,
	// and a guest that fills this inside one convergence floor has a correction
	// problem the ledger cannot fix.
	PredictionLedgerMax = 1024

	// NetworkSuccessionTicks bounds a succession: a survivor that has neither adopted
	// a handoff nor taken the term within one convergence floor continues locally and
	// says so. A deadline, never a timer to choose by: the successor is a function of
	// the roster and chain, and a racing timer could let two instances take one term.
	NetworkSuccessionTicks = SnapshotFloorKeyframeTicks

	// NetworkRejoinPassInterval paces a survivor's walk down the succession list. A
	// pass tries every candidate once; this is the pause before the list is walked
	// again, so a successor that is merely slow is retried without the list
	// becoming a spin.
	NetworkRejoinPassInterval = time.Second

	// NetworkMigrationBadgeTicks is how long the status bar shows MIGRATING after
	// a handoff is adopted. The badge marks a transition rather than a state, so
	// it is measured in ticks a person can read rather than held until something
	// clears it.
	NetworkMigrationBadgeTicks = 40

	// NetworkApplyWindowTicks is the furthest ahead of this instance's own tick an
	// arriving artifact may name and still be scheduled, since nothing retires a tick
	// this run never reaches. It clears the largest legitimate gap, a fresh join's
	// (NetworkJoinCatchUpTicks), plus a convergence floor for a peer running ahead.
	NetworkApplyWindowTicks = NetworkJoinCatchUpTicks + SnapshotFloorKeyframeTicks

	// NetworkScheduledMax and NetworkScheduledBytes bound the artifacts one instance
	// holds waiting for their apply tick: the count against many small artifacts, the
	// bytes against one pathological payload. Both sit far above any legitimate
	// production rate, so an overflow is a sender not playing the game, and is counted.
	NetworkScheduledMax   = 4096
	NetworkScheduledBytes = 4 << 20

	// NetworkScheduledPerSource is one participant's share of the schedule, so a
	// flooding peer fills its own share rather than everyone's.
	NetworkScheduledPerSource = NetworkScheduledMax / MaxPlayers

	// NetworkAdmitWindow, NetworkAdmitBurst and NetworkAdmitTracked bound how often
	// one dialling address may be admitted: each join costs the session a whole world
	// for one connect. Keyed by address, since an identity is released when the
	// connection drops; one NAT is one key, so the burst is generous. Tracked bounds
	// the limiter itself, so the defence cannot become the leak.
	NetworkAdmitWindow  = time.Minute
	NetworkAdmitBurst   = 6
	NetworkAdmitTracked = 1024

	// NetworkStreamLevel is the deflate level each link's stream runs at. Levels 1-6
	// drop their history on any flushed block under 128 bytes, which is most bursts;
	// 7 is the cheapest that keeps it, a third of level 1's bytes on recorded traffic.
	NetworkStreamLevel = 7

	// NetworkWriteLinger is how long a link's writer waits after the first frame of
	// a burst for the rest of it, so one tick's frames leave in one write.
	NetworkWriteLinger = 2 * time.Millisecond

	// NetworkEpochWindow is how far behind a source's newest epoch a late one may
	// still be admitted. A mesh delivers by several paths at once, so epochs from one
	// source arrive out of order and a high-water mark alone would discard epochs the
	// receiver never applied. At 20 ticks/s this is just over three seconds — far
	// beyond any path an artifact can take and still meet its apply tick.
	NetworkEpochWindow = 64

	// NetworkSequenceWindow is how far behind a source's newest crossing sequence
	// a receiver still recognises one it took, and how far behind its newest epoch
	// returned committed a producer still knows one was. A handoff resends the
	// retained suffix, whose records this covers at any input rate.
	NetworkSequenceWindow = 512
)
