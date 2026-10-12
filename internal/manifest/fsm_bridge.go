package manifest

import (
	"slices"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
)

// RegisterFSMComponents installs the FSM standard library bound to the ECS world
func RegisterFSMComponents(m *fsm.Machine[*engine.World]) {
	std.Register(m, worldHost())
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
		// the runtime half of the load-time check in resource.checkSystems
		SetSystem: func(w *engine.World, name string, enabled bool) {
			if !enabled && !w.AllowSystemDisable(name) {
				return
			}
			w.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
				SystemName: name,
				Enabled:    enabled,
			})
		},

		// A system toggles only if it handles the command, which cursor, camera and
		// network do not, so an only_systems list cannot strand them
		Systems: func(w *engine.World) []string {
			var names []string
			for _, sys := range w.Systems() {
				if h, ok := sys.(event.Handler); ok && slices.Contains(h.EventTypes(), event.EventMetaSystemCommandRequest) {
					names = append(names, sys.Name())
				}
			}
			return names
		},

		StatusInt: func(w *engine.World, key string) (int64, bool) {
			if !w.Resources.Status.Ints.Has(key) {
				return 0, false
			}
			return w.Resources.Status.Ints.Get(key).Load(), true
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
