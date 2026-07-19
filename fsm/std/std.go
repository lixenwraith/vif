// Package std provides the standard action and guard library for fsm.Machine.
//
// Registration is split by coupling. Actions and guards operating only on
// machine state — variables, regions, timing, payload inspection — are always
// available. Those needing embedder cooperation are driven through Host.
//
// Host fields are optional. A nil field leaves its actions registered but inert
// and its guards reporting false, so a config referencing them still loads and
// validates. A zero Host therefore yields a pure machine, which is what
// non-game embedders (editors, tools, tests) want.
package std

import (
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/fsm"
)

// Host exposes embedder capabilities to FSM scripts
//
// Every field is optional. A nil field leaves its actions registered but inert
// and its guards reporting false, so a config referencing them still loads and
// validates. A zero Host yields a pure machine.
type Host[T any] struct {
	// Emit publishes an event to the embedder's queue
	Emit func(ctx T, et event.EventType, payload any)

	// SetSystem enables or disables a named game system
	SetSystem func(ctx T, name string, enabled bool)

	// Status keys are an open runtime set, resolved per evaluation
	StatusInt    func(ctx T, key string) (int64, bool)
	SetStatusInt func(ctx T, key string, v int64)
	StatusBool   func(ctx T, key string) (bool, bool)

	// Config fields are a closed compile-time set: these return an accessor
	// resolved once at load, so evaluation is a direct call and an unknown
	// field fails the load rather than silently comparing against zero
	ConfigInt  func(field string) (func(ctx T) int64, bool)
	ConfigBool func(field string) (func(ctx T) bool, bool)
}

// Register installs the standard actions and guards on m
// Must be called before LoadConfig; names are resolved at load time
func Register[T any](m *fsm.Machine[T], h Host[T]) {
	registerCoreActions(m, h)
	registerVariableActions(m)
	registerRegionActions(m, h)
	registerSystemActions(m, h)
	registerStatusActions(m, h)

	registerCoreGuards(m)
	registerCompoundGuards(m)
	registerPayloadGuards(m)
	registerStatusGuards(m, h)
	registerConfigGuards(m, h)
	registerStaticGuards(m)
}

// Ops returns the comparison operators accepted by the comparison guards
// Consumed by the schema exporter
func Ops() []string { return []string{"eq", "neq", "gt", "gte", "lt", "lte"} }

// alwaysFalse is the guard returned when the host lacks the needed capability
func alwaysFalse[T any]() fsm.GuardFunc[T] {
	return func(T, *fsm.RegionState, any) bool { return false }
}
