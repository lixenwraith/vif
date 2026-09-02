package parameter

import "time"

// Transport cadence. Crossings use a fixed playout delay; owner-authored state is
// a periodic value sync whose interval trades freshness against traffic.
const (
	// NetworkBarrierDelayTicks gives an artifact 150ms to reach every participant.
	// The session carries this value so a higher-latency deployment can negotiate more.
	NetworkBarrierDelayTicks = 3

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

	// NetworkJoinReadyTimeout bounds how long a coordinator waits for a mid-run
	// joiner to install the world it was sent and confirm it. It is a link and
	// install bound rather than a game one: a participant that needs longer than
	// this has a link or a machine that cannot keep the playout lead, and refusing
	// its join is better than admitting a participant whose crossings will arrive
	// after the ticks they name.
	NetworkJoinReadyTimeout = 5 * time.Second

	// NetworkJoinLagTicks is how far behind the session a freshly installed
	// participant may land and still be admitted, in ticks.
	//
	// It is the playout lead, and that is not a coincidence: a participant N ticks
	// behind produces a crossing for tick Q+lead when the rest of the session is
	// already at Q+N, so the artifact is late by N-lead. At or under the lead it
	// still lands on time. The join measures its own lag against this and refuses
	// rather than joining a session it will immediately diverge from.
	NetworkJoinLagTicks = NetworkBarrierDelayTicks

	// NetworkJoinCatchUpTicks bounds the ticks a joining participant may simulate
	// to close the gap between the world it installed and the session's current
	// tick. The gap is the transfer and install cost, which is a function of world
	// size rather than of session length; this ceiling is far above the measured
	// cost and exists so a pathological link fails the join instead of stalling
	// inside it.
	NetworkJoinCatchUpTicks = 200

	// SnapshotCorrectionTicks is the correction cadence a session rests at, in
	// ticks. At the 50 ms tick this is 5 Hz, which is the fast end of §2.1's
	// hypothesis and the one the storm measurement said is only affordable with
	// deltas.
	//
	// The rate is low because it can be: both instances run the same deterministic
	// simulation, so a guest between corrections is extrapolating rather than
	// guessing, and a correction is a repair of the difference an unmodelled input
	// made rather than the picture itself.
	//
	// Phase 5 made it the *nominal* point of a bounded controller rather than the
	// cadence. What a peer actually receives is chosen per peer from its measured
	// link and its own demand, inside SnapshotCadenceMinTicks and
	// SnapshotCadenceMaxTicks, and never past the convergence floor below.
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

	// SnapshotUrgentDriftPercent and SnapshotUrgentRelevancePercent are where a
	// peer stops being ordinary, and both are percentages rather than counts
	// because an absolute threshold is a threshold about the world rather than
	// about the participant.
	//
	// A correction that moves five hundred shared entities is enormous in a quiet
	// world and unremarkable in a storm — measured, the storm's magnitude is the
	// whole shared population every cadence — so a fixed magnitude would pin every
	// storm at the fastest cadence the link allows and spend the entire uplink on
	// a condition that is simply what a storm looks like. What says the cadence is
	// no longer keeping up is the *rise*: how far the far end's correction
	// magnitude stands above its own recent level.
	//
	// Relevance is a comparison for the same reason, against the session's mean
	// rather than a count: with one guest there is nobody to prioritise against
	// and the whole link is already its own, and with several the question is
	// which of them has more at stake in the next correction.
	SnapshotUrgentDriftPercent     = 50
	SnapshotUrgentRelevancePercent = 100

	// SnapshotRelevanceRadius is how far from a participant's cursor a shared
	// entity is still that participant's business, in cells. It is a scheduling
	// hint and never a filter: a correction still carries the whole world, so a
	// wrong or stale radius costs a correction sent sooner than it was needed and
	// can never cost correctness.
	SnapshotRelevanceRadius = 24

	// SnapshotKeyframeCorrections is how many deltas the host sends between whole
	// captures at the nominal operating point. A delta is worthless without the
	// keyframe it names, so this is the longest a participant that missed one —
	// or that has just arrived — waits before it can apply anything again: 10
	// corrections is two seconds at the nominal cadence.
	//
	// It is also the loss bound. Nothing here acknowledges a correction, and
	// nothing needs to: a keyframe supersedes everything before it, so a lost
	// delta costs freshness for at most this many corrections and never
	// correctness. Under pressure the controller stretches it toward
	// SnapshotKeyframeMaxCorrections, because a keyframe costs six times a delta
	// on this world and stretching it spends recovery time the floor bounds
	// rather than freshness a player sees.
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
	// participant may go without a whole authoritative world.
	//
	// This is the promise the whole adaptive path is bounded by. Cadence and
	// keyframe interval are preferences the controller may trade away under
	// pressure; the floor is not. Their product may never exceed it, and a link
	// that cannot carry one whole world per floor window cannot sustain
	// convergence at all — which is refused at admission and reported as an
	// unrecoverable operating condition mid-session, never adapted past in
	// silence.
	//
	// Three seconds is 1.5 times the nominal keyframe period, which is the room
	// adaptation needs, and it is short enough that a participant that loses every
	// delta still re-converges inside a span a player experiences as a stutter
	// rather than as a broken session.
	SnapshotFloorKeyframeTicks = 60

	// SnapshotLinkUtilisation is the share of measured link capacity the
	// correction cadence may spend. The rest is headroom for the crossing epochs,
	// the owner-authored syncs and the digests, which travel on the same link and
	// are not in the controller's cost model.
	SnapshotLinkUtilisation = 0.75

	// SnapshotCadenceRecoverTicks and SnapshotCadenceRecoverKeyframe bound how
	// fast the operating point may move back toward nominal. Degradation is
	// immediate — a link that has narrowed has already narrowed — and recovery is
	// stepped, so one sample taken during a quiet moment cannot restore a cadence
	// the link has not regained.
	SnapshotCadenceRecoverTicks    = 2
	SnapshotCadenceRecoverKeyframe = 2

	// NetworkProbeInterval is how often a link measures a real round trip. It is
	// wall time rather than ticks because it measures the wire and not the
	// simulation: a paused instance still has a link, and a link that has gone
	// silent is exactly what a probe is for.
	//
	// Five probes a second costs 45 bytes each way per peer — under half a
	// kilobyte a second at MaxPlayers — against a correction stream measured in
	// hundreds of kilobytes. The estimator smooths over eight samples, so this is
	// also how quickly a link change becomes steerable: about a second and a half.
	NetworkProbeInterval = 200 * time.Millisecond

	// SnapshotFloorGraceTicks is how far past the floor a receiver waits before
	// calling its own condition unrecoverable. The floor is a publication
	// guarantee; a receiver additionally pays the transfer and the install, so
	// reporting a breach the instant the floor elapses would flag every ordinary
	// slow keyframe. One nominal keyframe period of grace is the smallest margin
	// that cannot fire on a healthy link.
	SnapshotFloorGraceTicks = SnapshotCorrectionTicks * SnapshotKeyframeCorrections

	// SnapshotStaleTicks is where the staleness indicator turns on: how far behind
	// the session's newest observed tick this instance may stand before a player
	// should be told the link rather than the game is the problem.
	//
	// It is the playout lead, for the same reason NetworkJoinLagTicks is: past it
	// this participant's own crossings reach the host after the tick they name, so
	// the host reorders them and the correction magnitude grows. Under it, nothing
	// is late and there is nothing to say.
	SnapshotStaleTicks = NetworkBarrierDelayTicks

	// SnapshotCorrectionQueue bounds the corrections one instance may hold
	// un-applied. A correction supersedes every earlier one, so the queue is
	// small on purpose and drops the *oldest* when it overflows: falling behind
	// should cost freshness, never the newest authority.
	SnapshotCorrectionQueue = 4

	// === Phase 6: hash-guided selective correction ===

	// SnapshotManifestPageRows is how many rows a manifest page holds at the
	// nominal partition. A page is the unit of both proof and repair, so it is a
	// trade between the two: larger pages hash and compare more cheaply and repair
	// more coarsely, smaller ones the reverse. Thirty-two cells is roughly one
	// storm circle's worth of a component store — small enough that a single
	// diverged entity does not drag a hundred unrelated ones onto the wire, and
	// large enough that a healthy section's page list stays short.
	SnapshotManifestPageRows = 32

	// SnapshotManifestMaxPages bounds a section's page count whatever its row
	// count. It is what keeps the page vector in a request bounded by the protocol
	// rather than by the world: at MaxPlayers-scale worlds a section is at most
	// this many hashes, so the descent from a section to its pages costs a fixed
	// amount of wire however large the store grows.
	SnapshotManifestMaxPages = 64

	// SnapshotManifestRetention is how many recent captures and manifests a host
	// keeps so it can answer a request naming an earlier one. A guest answers the
	// manifest it last received, so the retention only has to cover the round trip
	// plus the queue; past it the host answers with a keyframe instead, which is
	// the bounded fallback the whole protocol is allowed to reach.
	SnapshotManifestRetention = 4

	// SnapshotShardBytesMax bounds one repair. A shard set larger than this is not
	// a repair, it is a capture with extra steps: the host sends a keyframe
	// instead, which is smaller once the mismatch is that wide because it carries
	// no per-page identity or proof. It is also what keeps a repair inside one
	// transport frame.
	SnapshotShardBytesMax = 48 << 10

	// SnapshotManifestSilenceCorrections is how many manifests a peer may leave
	// unanswered before the host stops assuming it is in the selective protocol
	// and sends it whole bodies again.
	//
	// A guest answers every manifest — with a request or with the ack that records
	// convergence — so silence means the answer cannot get back: a peer reached
	// only by relay, or one whose uplink has failed while its downlink has not.
	// Neither can be repaired selectively, and both are still owed an authority,
	// so the host falls back to the Phase 5 stream for them. Three is one nominal
	// round trip plus a lost answer.
	SnapshotManifestSilenceCorrections = 3

	// SnapshotReplayTicks, SnapshotReplayRecords and SnapshotReplayBytes bound the
	// suffix of its own accepted crossings a guest retains for replay after a
	// correction rebases it.
	//
	// All three are needed and they bound different failures. The tick span is the
	// useful window — a correction older than the convergence floor is not
	// something local input should be replayed onto — the record count bounds a
	// participant typing or firing far faster than the cadence, and the byte bound
	// is what keeps a pathological payload from turning retention into an
	// unbounded buffer. Overflowing any of them makes the suffix unavailable
	// rather than partial: a partial replay is a guess, and this never guesses.
	SnapshotReplayTicks   = SnapshotFloorKeyframeTicks
	SnapshotReplayRecords = 512
	SnapshotReplayBytes   = 256 << 10

	// NetworkSuccessionTicks bounds a succession. A survivor that has neither
	// adopted a handoff nor been elected within it falls back to local
	// continuation and says so.
	//
	// It is a deadline rather than a timer to elect by: nothing about the choice
	// of successor depends on it, because a randomized or racing timer is exactly
	// what would let two instances elect themselves in one term. What it bounds is
	// how long an instance waits before admitting there is no succession — one
	// convergence floor, which is the window the session already promises a whole
	// authoritative world inside.
	NetworkSuccessionTicks = SnapshotFloorKeyframeTicks

	// NetworkMigrationBadgeTicks is how long the status bar shows MIGRATING after
	// a handoff is adopted. The badge marks a transition rather than a state, so
	// it is measured in ticks a person can read rather than held until something
	// clears it.
	NetworkMigrationBadgeTicks = 40

	// NetworkEpochWindow is how far behind a source's newest epoch a late one may
	// still be admitted. A mesh delivers by several paths at once, so epochs from one
	// source arrive out of order and a high-water mark alone would discard epochs the
	// receiver never applied. At 20 ticks/s this is just over three seconds — far
	// beyond any path an artifact can take and still meet its apply tick.
	NetworkEpochWindow = 64
)
