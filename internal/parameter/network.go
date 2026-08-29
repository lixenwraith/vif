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

	// NetworkDrainWindow bounds one tick's inbound translation, so a flooding
	// peer cannot stretch a tick without bound
	NetworkDrainWindow = 64
)
