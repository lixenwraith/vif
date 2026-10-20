package engine

// SharedStateSaver is implemented by a system that holds state outside any
// component store which can change a future shared outcome (D-19), exactly the
// systems the manifest declares "state", which its declaration test checks. It never
// carries what a shared store holds, what install re-derives, or any player-domain
// value (D-1, D-6). Durations count from the capture tick.
type SharedStateSaver interface {
	// SaveShared serializes this system's future-affecting private state. The
	// encoding must be canonical: two instances holding equal state must produce
	// equal bytes, because a capture is compared as well as installed.
	SaveShared() ([]byte, error)

	// LoadShared installs what SaveShared produced. It is called on a staging
	// world before the swap, so a returned error rejects the whole snapshot rather
	// than leaving a live world half-installed.
	LoadShared(data []byte) error
}

// SharedStateChecker lets a carrier refuse a record before anything is written,
// when acceptance depends on live state a staging world lacks: a genetic registry's
// species set is entered by a level transition. The live instance asks it in the
// dry run before any store is touched, so it must not mutate anything.
type SharedStateChecker interface {
	CheckShared(data []byte) error
}

// LocalStateSaver is implemented by a system holding state outside the stores that a
// copy of it must take from its source to continue as the source would: player-domain
// state, pending work, timers. A journal's opening carries it from the run, a
// checkpoint from a replay copy. It is the whole of the system, so a restore skips
// LoadShared, which re-derives as a joiner would. LoadLocal pushes no events.
type LocalStateSaver interface {
	SaveLocal() ([]byte, error)
	LoadLocal(data []byte) error
}

// StateCopier is LocalStateSaver for state that does not encode: caches a copy would
// otherwise rebuild, and a transport. A checkpoint carries it in memory; a journal's
// opening does not, and its replay re-derives it. CopyState's value shares no storage
// with the system.
type StateCopier interface {
	CopyState() any
	RestoreState(any) error
}

// Toggled is a system with an FSM-driven enable flag, which a replay copy carries
// beside the system's own state.
type Toggled interface {
	Enabled() bool
	SetEnabled(bool)
}

// SnapshotProfile is a system's declared snapshot obligation, generated from the
// manifest beside its domain profile.
type SnapshotProfile uint8

const (
	// SnapshotNone declares the system holds no future-affecting state outside the
	// component stores. Asserted, not assumed: implementing SharedStateSaver while
	// declaring this fails the boundary suite.
	SnapshotNone SnapshotProfile = iota
	// SnapshotState declares the system holds such state and implements
	// SharedStateSaver to carry it.
	SnapshotState
)

var snapshotProfileNames = [...]string{"none", "state"}

// String names the profile for diagnostics and test failures
func (p SnapshotProfile) String() string {
	if int(p) >= len(snapshotProfileNames) {
		return "invalid"
	}
	return snapshotProfileNames[p]
}
