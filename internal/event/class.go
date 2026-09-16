package event

import "github.com/lixenwraith/vi-fighter/internal/core"

// EventClass is an event type's replication class (D-10). It answers one
// question: must this record appear identically in every instance's journal?
// Shared and Bus do, Local does not, and Stamped is resolved per event from
// GameEvent.Domain because the type alone cannot decide it.
type EventClass uint8

const (
	// ClassUnset is the zero value, so an unannotated constant is visible rather
	// than silently defaulting into a real class. Treated as not replicated.
	ClassUnset EventClass = iota
	// ClassLocal is never replicated: per-instance view, audio, operator surface,
	// and player-domain simulation whose effects never reach shared state.
	ClassLocal
	// ClassShared is emitted and consumed by shared systems and re-derived
	// identically on every instance, so every instance journals it.
	ClassShared
	// ClassBus is owner-resolved and affects shared state: the D-3 crossing.
	ClassBus
	// ClassStamped is decided per event, not per type. The producer resolves the
	// replication domain and stamps GameEvent.Domain; the filter reads the stamp.
	ClassStamped
)

var classNames = [...]string{"unset", "local", "shared", "bus", "stamped"}

// String names the class for diagnostics and test failures
func (c EventClass) String() string {
	if int(c) >= len(classNames) {
		return "invalid"
	}
	return classNames[c]
}

// ClassOf returns the declared replication class of an event type.
// An out-of-range type reports ClassUnset rather than panicking: callers are
// filters and telemetry, and neither should take down a running instance.
func ClassOf(et EventType) EventClass {
	if et < 0 || int(et) >= len(eventClasses) {
		return ClassUnset
	}
	return eventClasses[et]
}

// Replicated reports whether a dispatched event belongs in the transported set,
// which is Shared union Bus (D-10). Stamped types defer to the domain the
// producer resolved: shared crosses, player does not.
func Replicated(et EventType, domain core.Domain) bool {
	switch ClassOf(et) {
	case ClassShared, ClassBus:
		return true
	case ClassStamped:
		return domain == core.DomainShared
	default:
		return false
	}
}

// EventPhase says whether a record derived from shared state is still this
// instance's own prediction. A guest predicts the shared world forward between
// corrections, so a death it derives can be undone by a correction and derived
// again; the player-domain rewards that follow one are in no capture and no
// correction can take them back.
type EventPhase uint8

const (
	// PhaseSettled is the default and the whole of a run nobody corrects: the
	// record states a fact this instance owns.
	PhaseSettled EventPhase = iota
	// PhasePredicted marks a derivation this instance may yet have to make again.
	// Presentation follows it; a reward waits for the authority to prove it.
	PhasePredicted
)

// Derivation names the edge-triggered types a predicting instance can raise more
// than once for one underlying fact, because the shared state they read is
// non-monotonic there. A shared-domain push of one is stamped PhasePredicted and
// confirmed against the next authoritative world.
func Derivation(et EventType) bool { return et == EventSpeciesKilled }
