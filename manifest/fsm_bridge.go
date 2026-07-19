package manifest

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/fsm"
	"github.com/lixenwraith/vi-fighter/fsm/std"
)

// RegisterFSMComponents installs the FSM standard library bound to the ECS world,
// plus game-specific actions that have no generic form
func RegisterFSMComponents(m *fsm.Machine[*engine.World]) {
	std.Register(m, worldHost())
	registerGameActions(m)
}

// worldHost binds std.Host capabilities to the ECS world
func worldHost() std.Host[*engine.World] {
	return std.Host[*engine.World]{
		Emit: (*engine.World).PushEvent,

		SetSystem: func(w *engine.World, name string, enabled bool) {
			w.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
				SystemName: name,
				Enabled:    enabled,
			})
		},

		StatusInt: func(w *engine.World, key string) (int64, bool) {
			if !w.Resources.Status.Ints.Has(key) {
				return 0, false
			}
			return w.Resources.Status.Ints.Get(key).Load(), true
		},
		SetStatusInt: func(w *engine.World, key string, v int64) {
			w.Resources.Status.Ints.Get(key).Store(v)
		},
		StatusBool: func(w *engine.World, key string) (bool, bool) {
			if !w.Resources.Status.Bools.Has(key) {
				return false, false
			}
			return w.Resources.Status.Bools.Get(key).Load(), true
		},

		ConfigInt:  engine.ConfigIntAccessor,
		ConfigBool: engine.ConfigBoolAccessor,
	}
}

// registerGameActions installs actions with no generic equivalent
func registerGameActions(m *fsm.Machine[*engine.World]) {
	// ResetKillVars zeroes the per-cycle kill counters
	// Candidate for removal: four ResetStatusInt actions in config express the same
	m.RegisterAction("ResetKillVars", func(w *engine.World, args any) {
		ints := w.Resources.Status.Ints
		ints.Get("kills.drain").Store(0)
		ints.Get("kills.swarm").Store(0)
		ints.Get("kills.quasar").Store(0)
		ints.Get("kills.storm").Store(0)
	})
}
