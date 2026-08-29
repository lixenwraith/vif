package parameter

// Transport cadence. Crossings go out the tick they are pushed; owner-authored
// state is a periodic value sync, so its interval trades staleness against traffic.
const (
	// NetworkSyncTicks is the period between owner-authored state syncs (D-13).
	// One cursor's payload is small, so the interval is short enough that a
	// shared collection resolved through a peer's shield or ember reads a value
	// no more than this many ticks old.
	NetworkSyncTicks = 6

	// NetworkDrainWindow bounds one tick's inbound translation, so a flooding
	// peer cannot stretch a tick without bound
	NetworkDrainWindow = 64
)
