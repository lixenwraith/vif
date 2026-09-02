package manifest

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/fsm/std"
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
		// A region's actions run on every instance — the machine is shared and every
		// instance enters the same state — so an artifact one of them raises is a
		// per-instance effect unless its class says otherwise. The Local class
		// already says so: it is what keeps the artifact off the wire. Stamping the
		// record to match is what makes it honest, because a per-instance effect
		// journaled as shared is a record two instances legitimately differ on
		// while claiming they should not.
		Emit: func(w *engine.World, t event.EventType, payload any) {
			if event.ClassOf(t) == event.ClassLocal {
				w.PushLocal(t, payload)
				return
			}
			w.PushEvent(t, payload)
		},

		// A declared required dependency outranks the config: refusing here is
		// the runtime half of the load-time check in app.checkSystems
		SetSystem: func(w *engine.World, name string, enabled bool) {
			if !enabled && !w.AllowSystemDisable(name) {
				return
			}
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
	// ResetKillVars zeroes every per-cycle kill counter.
	// MetaSystem registers these keys before Freeze, so Get is a lookup here.
	m.RegisterAction("ResetKillVars", func(w *engine.World, args any) {
		ints := w.Resources.Status.Ints
		for i := component.SpeciesType(1); i < component.SpeciesCount; i++ {
			ints.Get("kills." + component.SpeciesNames[i]).Store(0)
		}
		ints.Get("kills.total").Store(0)
	})
}
