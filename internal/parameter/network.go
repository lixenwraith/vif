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
