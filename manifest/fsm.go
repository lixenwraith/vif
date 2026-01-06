package manifest

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/fsm"
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
		duration := constant.GameUpdateInterval
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
		return func(world *engine.World) bool {
			return machine.TimeInState() >= duration
		}
	})

	m.RegisterGuardFactory("StatusBoolEquals", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		key, _ := args["key"].(string)
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		return func(world *engine.World) bool {
			if !world.Resource.Status.Bools.Has(key) {
				return false
			}
			return world.Resource.Status.Bools.Get(key).Load() == expected
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

		return func(world *engine.World) bool {
			if !world.Resource.Status.Ints.Has(key) {
				return false
			}
			current := world.Resource.Status.Ints.Get(key).Load()

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
		return func(world *engine.World) bool {
			return machine.HasRegion(regionName)
		}
	})

	// --- GUARDS (Static) ---

	m.RegisterGuard("AlwaysTrue", func(world *engine.World) bool {
		return true
	})

	m.RegisterGuard("StateTimeExceeds10s", func(world *engine.World) bool {
		return m.TimeInState() > 10*time.Second
	})

	m.RegisterGuard("StateTimeExceeds2s", func(world *engine.World) bool {
		return m.TimeInState() > 2*time.Second
	})
}

// DefaultGameplayFSMConfig returns the default TOML configuration matching the legacy ClockScheduler logic
// This effectively ports the hardcoded Phase switch into data
const DefaultGameplayFSMConfig = `
initial = "TrySpawnGold"

[states.Gameplay]
parent = "Root"

# === GOLD SPAWN CYCLE ===

[states.TrySpawnGold]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" }
]
transitions = [
    { trigger = "EventGoldSpawned", target = "GoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "GoldRetryWait" }
]

[states.GoldRetryWait]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "TrySpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 1 } }
]

[states.GoldActive]
parent = "Gameplay"
transitions = [
    { trigger = "EventGoldComplete", target = "PreSweepCheck" },
    { trigger = "EventGoldTimeout", target = "DecayWait" },
    { trigger = "EventGoldDestroyed", target = "DecayWait" }
]

# === SWEEPING EVALUATION ===

[states.PreSweepCheck]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "SweepingPhaseHot", guard = "StatusBoolEquals", guard_args = { key = "heat.at_max", value = true } },
    { trigger = "Tick", target = "SweepingPhaseNormal" }
]

[states.SweepingPhaseHot]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" }
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "FusePhase" }
]

[states.SweepingPhaseNormal]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventHeatSet", payload = { value = 100 } },
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" }
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "DecayWait" }
]

# === QUASAR PHASE ===

[states.QuasarPhase]
parent = "Gameplay"
on_exit = [
    { action = "EmitEvent", event = "EventGoldCancel" }
]
transitions = [
    { trigger = "EventQuasarDestroyed", target = "QuasarCooldown" }
]

[states.FusePhase]
parent = "QuasarPhase"
on_enter = [
    { action = "EmitEvent", event = "EventFuseDrains" }
]
transitions = [
    { trigger = "EventQuasarSpawned", target = "QuasarSpawnGold" }
]

[states.QuasarSpawnGold]
parent = "QuasarPhase"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" }
]
transitions = [
    { trigger = "EventGoldSpawned", target = "QuasarGoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "QuasarGoldRetry" }
]

[states.QuasarGoldRetry]
parent = "QuasarPhase"
transitions = [
    { trigger = "Tick", target = "QuasarSpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 100 } }
]

[states.QuasarGoldActive]
parent = "QuasarPhase"
transitions = [
    { trigger = "EventGoldComplete", target = "QuasarSpawnGold" },
    { trigger = "EventGoldTimeout", target = "QuasarSpawnGold" },
    { trigger = "EventGoldDestroyed", target = "QuasarSpawnGold" }
]

[states.QuasarCooldown]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "DecayWait", guard = "StateTimeExceeds", guard_args = { ms = 500 } }
]

# === DECAY/BLOSSOM WAVES ===

[states.DecayWait]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "DecayAnimation", guard = "StateTimeExceeds", guard_args = { ms = 5000 } }
]

[states.DecayAnimation]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventDecayWave" }
]
transitions = [
    { trigger = "Tick", target = "TrySpawnGold2", guard = "StateTimeExceeds", guard_args = { ms = 3000 } }
]

[states.TrySpawnGold2]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" }
]
transitions = [
    { trigger = "EventGoldSpawned", target = "GoldActive2" },
    { trigger = "EventGoldSpawnFailed", target = "GoldRetryWait2" }
]

[states.GoldRetryWait2]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "TrySpawnGold2", guard = "StateTimeExceeds", guard_args = { ms = 1 } }
]

[states.GoldActive2]
parent = "Gameplay"
transitions = [
    { trigger = "EventGoldComplete", target = "PreSweepCheck2" },
    { trigger = "EventGoldTimeout", target = "BlossomWait" },
    { trigger = "EventGoldDestroyed", target = "BlossomWait" }
]

[states.PreSweepCheck2]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "SweepingPhaseHot", guard = "StatusBoolEquals", guard_args = { key = "heat.at_max", value = true } },
    { trigger = "Tick", target = "SweepingPhaseNormal2" }
]

[states.SweepingPhaseNormal2]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventHeatSet", payload = { value = 100 } },
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" }
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "BlossomWait" }
]

[states.BlossomWait]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "BlossomAnimation", guard = "StateTimeExceeds", guard_args = { ms = 5000 } }
]

[states.BlossomAnimation]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventBlossomWave" }
]
transitions = [
    { trigger = "Tick", target = "TrySpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 3000 } }
]
`