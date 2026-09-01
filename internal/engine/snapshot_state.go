package engine

// SharedStateSaver is implemented by a system that holds state outside any
// component store which can change a future shared outcome (D-19).
//
// The rule the interface exists to serve is stated in the multiplayer plan's
// hidden-state survey: a snapshot must carry everything that decides a future
// shared outcome, and much of that is not in a store — a maze generator's
// position, a learned route table, a genetic population, a derivation phase. The
// survey's conclusion was that no inventory of it exists. This is the inventory:
// a system either declares "state" in the manifest and implements this, or
// declares nothing and is asserted to hold nothing, and the boundary suite fails
// the pair that disagree. A system that quietly grows private state fails the
// build rather than a session, which is the same construction that made D-15's
// domain profiles mechanical rather than reviewed.
//
// What belongs here is what a component store cannot hold and install cannot
// re-derive. What does not: anything already in a shared entity's store, anything
// recomputed at install (the flow field, the spatial index, the passability grid),
// and any player-domain value at all — a snapshot describes the shared world and
// nothing else (D-1, D-6).
//
// Durations are written relative to the tick the capture names, never as absolute
// instants. Since the simulation clock became tick-derived (SimTime) an instant is
// already a function of the tick, so the two agree; writing the relative form
// keeps them agreeing if a capture is ever rebased onto a different tick.
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
