package manifest

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
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
		duration := constants.GameUpdateInterval // Default: 1 tick
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

	// TODO: Add more game-specific guards here to test

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
	{ trigger = "Tick", target = "TrySpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 2000 } }
]

[states.GoldActive]
parent = "Gameplay"
transitions = [
	{ trigger = "EventGoldCollected", target = "TrySpawnGold" },
	{ trigger = "EventGoldExpired", target = "TrySpawnGold" }
]
`