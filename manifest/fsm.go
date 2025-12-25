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
	{ trigger = "EventGoldComplete", target = "DecayWait" },
	{ trigger = "EventGoldTimeout", target = "DecayWait" },
	{ trigger = "EventGoldDestroyed", target = "DecayWait" }
]

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
	{ trigger = "EventGoldComplete", target = "BlossomWait" },
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