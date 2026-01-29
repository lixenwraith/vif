package manifest

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/fsm"
	"github.com/lixenwraith/vi-fighter/parameter"
)

func RegisterFSMComponents(m *fsm.Machine[*engine.World]) {
	// --- ACTIONS ---

	m.RegisterAction("EmitEvent", func(world *engine.World, args any) {
		emitArgs, ok := args.(*fsm.EmitEventArgs)
		if !ok {
			return
		}
		world.PushEvent(emitArgs.Type, emitArgs.Payload)
	})

	// Region control actions
	m.RegisterAction("SpawnRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		initialID, ok := m.GetStateID(rcArgs.InitialState)
		if !ok {
			return
		}
		_ = m.SpawnRegion(world, rcArgs.RegionName, initialID)
	})

	m.RegisterAction("TerminateRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		_ = m.TerminateRegion(world, rcArgs.RegionName)
	})

	m.RegisterAction("PauseRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		m.PauseRegion(rcArgs.RegionName)
	})

	m.RegisterAction("ResumeRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		m.ResumeRegion(rcArgs.RegionName)
	})

	// --- GUARD FACTORIES ---

	m.RegisterGuardFactory("StateTimeExceeds", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		duration := parameter.GameUpdateInterval
		if args != nil {
			if v, ok := args["ms"]; ok {
				switch val := v.(type) {
				case float64:
					duration = time.Duration(val) * time.Millisecond
				case int:
					duration = time.Duration(val) * time.Millisecond
				case int64:
					duration = time.Duration(val) * time.Millisecond
				}
			}
		}
		return func(world *engine.World, region *fsm.RegionState) bool {
			return region.TimeInState >= duration
		}
	})

	m.RegisterGuardFactory("StatusBoolEquals", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		key, _ := args["key"].(string)
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		return func(world *engine.World, region *fsm.RegionState) bool {
			if !world.Resources.Status.Bools.Has(key) {
				return false
			}
			return world.Resources.Status.Bools.Get(key).Load() == expected
		}
	})

	m.RegisterGuardFactory("StatusIntCompare", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		key, _ := args["key"].(string)
		op, _ := args["op"].(string)
		var value int64
		switch v := args["value"].(type) {
		case float64:
			value = int64(v)
		case int64:
			value = v
		case int:
			value = int64(v)
		}

		return func(world *engine.World, region *fsm.RegionState) bool {
			if !world.Resources.Status.Ints.Has(key) {
				return false
			}
			current := world.Resources.Status.Ints.Get(key).Load()

			switch op {
			case "eq":
				return current == value
			case "neq":
				return current != value
			case "gt":
				return current > value
			case "gte":
				return current >= value
			case "lt":
				return current < value
			case "lte":
				return current <= value
			default:
				return current == value
			}
		}
	})

	m.RegisterGuardFactory("RegionExists", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		regionName, _ := args["region"].(string)
		return func(world *engine.World, region *fsm.RegionState) bool {
			return machine.HasRegion(regionName)
		}
	})

	// --- GUARDS (Static) ---

	m.RegisterGuard("AlwaysTrue", func(world *engine.World, region *fsm.RegionState) bool {
		return true
	})

	m.RegisterGuard("StateTimeExceeds10s", func(world *engine.World, region *fsm.RegionState) bool {
		return region.TimeInState > 10*time.Second
	})

	m.RegisterGuard("StateTimeExceeds2s", func(world *engine.World, region *fsm.RegionState) bool {
		return region.TimeInState > 2*time.Second
	})
}