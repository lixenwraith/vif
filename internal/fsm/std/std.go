// Package std is the standard action and guard library for fsm.Machine. Those over
// machine state (variables, regions, timing, payloads) always work; those needing
// the embedder go through Host. A nil Host hook leaves its actions inert and its
// guards false; Host.Refuse makes a nil hook fail the load of the event and system
// actions instead. A zero Host yields a pure machine. See doc/fsm-and-configuration.md.
package std

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
)

// Host exposes embedder capabilities to FSM scripts
//
// Every field is optional. A nil hook leaves its actions registered but inert
// and its guards reporting false, so a config referencing them still loads and
// validates. A zero Host yields a pure machine.
type Host[T any] struct {
	// Emit publishes an event to the embedder's queue
	Emit func(ctx T, et event.EventType, payload any)

	// SetSystem enables or disables a named game system
	SetSystem func(ctx T, name string, enabled bool)
	// ApplySystems sets every toggled system to what want reports, pushing
	// those decided reports and those whose wanted state changed
	ApplySystems func(ctx T, want, decided func(name string) bool)

	// Status keys are an open runtime set, resolved per evaluation
	StatusInt  func(ctx T, key string) (int64, bool)
	StatusBool func(ctx T, key string) (bool, bool)

	// Config fields are a closed compile-time set: these return an accessor
	// resolved once at load, so evaluation is a direct call and an unknown
	// field fails the load rather than silently comparing against zero
	ConfigInt  func(field string) (func(ctx T) int64, bool)
	ConfigBool func(field string) (func(ctx T) bool, bool)

	// Seed roots RandomVar: one value for a run, the same on every instance
	// that plays it; a nil Seed roots every draw at zero
	Seed func(ctx T) uint64

	// Names are what a payload's enum fields, and a species guard, take at load:
	// event.PayloadNames over a scenario's behavior programs; nil takes the code's
	Names *dialect.Names

	// Refuse, when set, fails the load of an event or system action whose hook
	// is nil with this reason, for an embedder whose scripts must not name what
	// does nothing
	Refuse string
}

// Register installs the standard actions and guards on m
// Must be called before a scenario loads; names are resolved at load time
func Register[T any](m *fsm.Machine[T], h Host[T]) {
	if h.Names == nil {
		h.Names = event.PayloadNames(component.Declared{})
	}
	registerCoreActions(m, h)
	registerVariableActions(m, h)
	registerRegionActions(m, h)
	registerSystemActions(m, h)
	registerStatusActions(m, h)

	registerCoreGuards(m)
	registerPayloadGuards(m, h)
	registerStatusGuards(m, h)
	registerConfigGuards(m, h)
	registerStaticGuards(m)
}

// alwaysFalse is the guard returned when the host lacks the needed capability
func alwaysFalse[T any]() fsm.GuardFunc[T] {
	return func(T, *fsm.RegionState, any) bool { return false }
}
