package manifest

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/fsm"
)

// RegisterFSMComponents registers all guards and actions with the FSM
func RegisterFSMComponents(m *fsm.Machine[*engine.World]) {
	// --- ACTIONS STATIC ---

	// EmitEvent: The universal glue; Takes a pre-compiled payload and pushes it to the World
	m.RegisterAction("EmitEvent", func(world *engine.World, args any) {
		emitArgs, ok := args.(*fsm.EmitEventArgs)
		if !ok {
			return
		}

		// Resolve payload: if it's a pointer to a struct, pass it directly
		world.PushEvent(emitArgs.Type, emitArgs.Payload)
	})

	// --- GUARD FACTORIES (Parameterized) ---

	// StateTimeExceeds: Configurable duration guard
	m.RegisterGuardFactory("StateTimeExceeds", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		duration := constant.GameUpdateInterval // Default: 1 tick
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

	// StatusBoolEquals: Query status registry bool value
	// Args: key (string), value (bool, default true)
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

	// StatusIntCompare: Query status registry int with comparison operator
	// Args: key (string), op (string: eq|neq|gt|gte|lt|lte), value (int64)
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

	// --- GUARDS STATIC ---

	// AlwaysTrue: Helper for unconditional transitions
	m.RegisterGuard("AlwaysTrue", func(world *engine.World) bool {
		return true
	})

	// TODO: Set more game-specific guards here to test

	// StateTimeExceeds10s: Checks if the FSM has been in the current state for > 10 seconds
	m.RegisterGuard("StateTimeExceeds10s", func(world *engine.World) bool {
		return m.TimeInState() > 10*time.Second
	})

	// StateTimeExceeds2s: For faster retry loops
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
{ trigger = "EventGoldComplete", target = "SweepingPhase" },
{ trigger = "EventGoldTimeout", target = "DecayWait" },
{ trigger = "EventGoldDestroyed", target = "DecayWait" }
]

# === SWEEPING PHASE (post gold-complete) ===

[states.SweepingPhase]
parent = "Gameplay"
transitions = [
{ trigger = "EventFuseDrains", target = "FusePhase" },
{ trigger = "EventCleanerSweepingFinished", target = "DecayWait" }
]

# === QUASAR PHASE (parent for all quasar substates) ===

[states.QuasarPhase]
parent = "Gameplay"
# Exit transition catchable from all children
transitions = [
{ trigger = "EventQuasarDestroyed", target = "QuasarCooldown" }
]

[states.FusePhase]
parent = "QuasarPhase"
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
# Gold complete during quasar: dust triggered by DustSystem, respawn gold
{ trigger = "EventGoldComplete", target = "QuasarSpawnGold" },
{ trigger = "EventGoldTimeout", target = "QuasarSpawnGold" },
{ trigger = "EventGoldDestroyed", target = "QuasarSpawnGold" }
]

[states.QuasarCooldown]
parent = "Gameplay"
transitions = [
{ trigger = "Tick", target = "TrySpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 500 } }
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
{ trigger = "EventGoldComplete", target = "SweepingPhase" },
{ trigger = "EventGoldTimeout", target = "BlossomWait" },
{ trigger = "EventGoldDestroyed", target = "BlossomWait" }
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