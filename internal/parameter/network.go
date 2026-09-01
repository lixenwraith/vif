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

	// SnapshotCorrectionTicks is the authoritative correction cadence, in ticks.
	// At the 50 ms tick this is 5 Hz, which is the fast end of §2.1's hypothesis
	// and the one the storm measurement said is only affordable with deltas.
	//
	// The rate is low because it can be: both instances run the same deterministic
	// simulation, so a guest between corrections is extrapolating rather than
	// guessing, and a correction is a repair of the difference an unmodelled input
	// made rather than the picture itself. Phase 5 drives this from the link
	// instead of from a constant.
	SnapshotCorrectionTicks = 4

	// SnapshotKeyframeCorrections is how many deltas the host sends between whole
	// captures. A delta is worthless without the keyframe it names, so this is the
	// longest a participant that missed one — or that has just arrived — waits
	// before it can apply anything again: 10 corrections is two seconds at the
	// cadence above.
	//
	// It is also the loss bound. Nothing here acknowledges a correction, and
	// nothing needs to: a keyframe supersedes everything before it, so a lost
	// delta costs freshness for at most this many corrections and never
	// correctness.
	SnapshotKeyframeCorrections = 10

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

	// NetworkEpochWindow is how far behind a source's newest epoch a late one may
	// still be admitted. A mesh delivers by several paths at once, so epochs from one
	// source arrive out of order and a high-water mark alone would discard epochs the
	// receiver never applied. At 20 ticks/s this is just over three seconds — far
	// beyond any path an artifact can take and still meet its apply tick.
	NetworkEpochWindow = 64
)
