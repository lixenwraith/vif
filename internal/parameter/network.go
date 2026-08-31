package parameter

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

	// NetworkEpochWindow is how far behind a source's newest epoch a late one may
	// still be admitted. A mesh delivers by several paths at once, so epochs from one
	// source arrive out of order and a high-water mark alone would discard epochs the
	// receiver never applied. At 20 ticks/s this is just over three seconds — far
	// beyond any path an artifact can take and still meet its apply tick.
	NetworkEpochWindow = 64
)
