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

		return func(world *engine.World, region *fsm.RegionState) bool {
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

// DefaultGameplayFSMConfig returns the default TOML configuration
const DefaultGameplayFSMConfig = `
[regions]
main = { initial = "TrySpawnGold" }

# =============================================================================
# MAIN REGION - Normal gameplay cycle
# =============================================================================

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
    { trigger = "Tick", target = "SweepingHot", guard = "StatusBoolEquals", guard_args = { key = "heat.at_max", value = true } },
    { trigger = "Tick", target = "SweepingNormal" }
]

[states.SweepingHot]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" }
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "QuasarHandoff" }
]

[states.SweepingNormal]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventHeatSet", payload = { value = 100 } },
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" }
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "DecayWait" }
]

# === QUASAR HANDOFF (main pauses here) ===

[states.QuasarHandoff]
parent = "Gameplay"
on_enter = [
    { action = "SpawnRegion", region = "quasar", initial_state = "QuasarFuse" },
    { action = "PauseRegion", region = "main" }
]
transitions = [
    { trigger = "Tick", target = "DecayWait" }
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
    { trigger = "Tick", target = "SweepingHot", guard = "StatusBoolEquals", guard_args = { key = "heat.at_max", value = true } },
    { trigger = "Tick", target = "SweepingNormal2" }
]

[states.SweepingNormal2]
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

# =============================================================================
# QUASAR REGION - Spawned dynamically, self-contained
# =============================================================================

[states.QuasarCycle]
parent = "Root"

[states.QuasarFuse]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventFuseDrains" }
]
transitions = [
    { trigger = "EventQuasarSpawned", target = "QuasarGoldSpawn" }
]

[states.QuasarGoldSpawn]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" }
]
transitions = [
    { trigger = "EventGoldSpawned", target = "QuasarGoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "QuasarGoldRetry" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" }
]

[states.QuasarGoldRetry]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarGoldSpawn", guard = "StateTimeExceeds", guard_args = { ms = 100 } },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" }
]

[states.QuasarGoldActive]
parent = "QuasarCycle"
transitions = [
    { trigger = "EventGoldComplete", target = "QuasarGoldSpawn" },
    { trigger = "EventGoldTimeout", target = "QuasarGoldSpawn" },
    { trigger = "EventGoldDestroyed", target = "QuasarGoldSpawn" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" }
]

[states.QuasarExit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" }
]
`

/*

State trace (multi-region)

```
Region: main (default)
────────────────────────────────────────────────────────
Root
└── Gameplay
    ├── TrySpawnGold [on_enter: GoldSpawnRequest]
    │   └→ GoldSpawned → GoldActive
    │   └→ GoldSpawnFailed → GoldRetryWait
    ├── GoldRetryWait → Tick[1ms] → TrySpawnGold
    ├── GoldActive
    │   └→ GoldComplete → PreSweepCheck
    │   └→ Timeout/Destroyed → DecayWait
    ├── PreSweepCheck
    │   └→ Tick[heat.at_max=true] → SweepingHot
    │   └→ Tick → SweepingNormal
    ├── SweepingHot [on_enter: CleanerRequest]
    │   └→ CleanerFinished → QuasarHandoff
    ├── SweepingNormal [on_enter: HeatSet(100), CleanerRequest]
    │   └→ CleanerFinished → DecayWait
    ├── QuasarHandoff [on_enter: SpawnRegion(quasar), PauseRegion(main)]
    │   └→ Tick → DecayWait  (fires when resumed)
    ├── DecayWait → Tick[5s] → DecayAnimation
    ├── DecayAnimation [on_enter: DecayWave] → Tick[3s] → TrySpawnGold2
    ├── TrySpawnGold2 [on_enter: GoldSpawnRequest]
    ├── GoldRetryWait2 → Tick[1ms] → TrySpawnGold2
    ├── GoldActive2
    │   └→ GoldComplete → PreSweepCheck2
    │   └→ Timeout/Destroyed → BlossomWait
    ├── PreSweepCheck2
    │   └→ Tick[heat.at_max=true] → SweepingHot  (reuses same path)
    │   └→ Tick → SweepingNormal2
    ├── SweepingNormal2 [on_enter: HeatSet(100), CleanerRequest]
    │   └→ CleanerFinished → BlossomWait
    ├── BlossomWait → Tick[5s] → BlossomAnimation
    └── BlossomAnimation [on_enter: BlossomWave] → Tick[3s] → TrySpawnGold

Region: quasar (spawned dynamically)
────────────────────────────────────────────────────────
Root
└── QuasarCycle
    ├── QuasarFuse [on_enter: FuseDrains]
    │   └→ QuasarSpawned → QuasarGoldSpawn
    ├── QuasarGoldSpawn [on_enter: GoldSpawnRequest]
    │   └→ GoldSpawned → QuasarGoldActive
    │   └→ GoldSpawnFailed → QuasarGoldRetry
    │   └→ QuasarDestroyed → QuasarExit
    ├── QuasarGoldRetry
    │   └→ Tick[100ms] → QuasarGoldSpawn
    │   └→ QuasarDestroyed → QuasarExit
    ├── QuasarGoldActive
    │   └→ GoldComplete/Timeout/Destroyed → QuasarGoldSpawn (loop)
    │   └→ QuasarDestroyed → QuasarExit
    └── QuasarExit [on_enter: GoldCancel, ResumeRegion(main), TerminateRegion(quasar)]
```

*/