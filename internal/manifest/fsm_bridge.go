package manifest

import (
	"slices"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// RegisterFSMComponents installs the FSM standard library bound to the ECS world
func RegisterFSMComponents(m *fsm.Machine[*engine.World]) {
	std.Register(m, worldHost())
}

// worldHost binds std.Host capabilities to the ECS world
func worldHost() std.Host[*engine.World] {
	return std.Host[*engine.World]{
		// A region's actions run on every instance, so an artifact one raises is a
		// per-instance effect unless its class says otherwise. The Local class keeps it
		// off the wire; pushing it player-domain makes the record match, since one
		// journaled as shared would claim an agreement two instances legitimately lack.
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

		// A system toggles only if it handles the command, which cursor, camera, meta
		// and network do not, so an only_systems list cannot strand them
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

		// The session root is shared session identity, and the run moves before a
		// reset re-enters the machine, which the session counter does only after
		Seed: func(w *engine.World) uint64 {
			run := w.Resources.Event.Queue.Stamp().Run
			return vmath.DeriveSeed(w.Resources.Rand.DomainRoot(core.DomainShared)^vmath.Mix64(run), "fsm")
		},
	}
}
