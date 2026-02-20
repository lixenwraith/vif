package asset

// DefaultGameplayFSMConfig is the embedded fallback FSM TOML configuration
const DefaultGameplayFSMConfig = `
# Root configuration with region definitions

[regions.main]
initial = "MainSpawnGold"

[regions.quasar]

[regions.storm]

[regions.monitor]
initial = "MonitorWarmup"
background = true

[regions.placeholder]

[states.Root]

# === Main Region ===

[states.MainCycle]
parent = "Root"
on_exit = [
    { action = "EmitEvent", event = "EventGoldCancel" }
]
transitions = [
    { trigger = "Tick", target = "MainEscalate", guard = "StatusIntCompare", guard_args = { key = "kills.drain", op = "gte", value = 10 } }
]

[states.MainSpawnGold]
parent = "MainCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" }
]
transitions = [
    { trigger = "EventGoldSpawned", target = "MainGoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "MainSpawnGoldRetry" }
]

[states.MainSpawnGoldRetry]
parent = "MainCycle"
transitions = [
    { trigger = "Tick", target = "MainSpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 100 } }
]

[states.MainGoldActive]
parent = "MainCycle"
transitions = [
    { trigger = "EventGoldComplete", target = "MainGoldComplete" },
    { trigger = "EventGoldTimeout", target = "MainGoldTimeout" },
    { trigger = "EventGoldDestroyed", target = "MainGoldDestroyed" }
]

[states.MainGoldComplete]
parent = "MainCycle"
on_enter = [
    { action = "EmitEvent", event = "EventHeatAddRequest", payload = { delta = 100 } },
    { action = "EmitEvent", event = "EventEnergyAddRequest", payload = { delta = 1000, type = 1 } }
]
transitions = [
    { trigger = "Tick", target = "MainDecayWait" }
]

[states.MainGoldTimeout]
parent = "MainCycle"
transitions = [
    { trigger = "Tick", target = "MainDecayWait" }
]

[states.MainGoldDestroyed]
parent = "MainCycle"
transitions = [
    { trigger = "Tick", target = "MainDecayWait" }
]

[states.MainDecayWait]
parent = "MainCycle"
transitions = [
    { trigger = "Tick", target = "MainDecayWave", guard = "StateTimeExceeds", guard_args = { ms = 5000 } }
]

[states.MainDecayWave]
parent = "MainCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDecayWave" }
]
transitions = [
    { trigger = "Tick", target = "MainSpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 5000 } }
]

[states.MainEscalate]
parent = "Root"
on_enter = [
    { action = "ResetStatusInt", payload = { key = "kills.drain" } },
    { action = "SpawnRegion", region = "quasar", initial_state = "QuasarFuse" },
    { action = "PauseRegion", region = "main" }
]
transitions = [
    { trigger = "Tick", target = "MainSpawnGold" }
]

# === Monitor Region ===

# Parallel region for global state and reset

[states.MonitorWarmup]
parent = "Root"
transitions = [
    { trigger = "Tick", target = "MonitorActive", guard = "Or", guard_args = { guards = [
        { name = "StatusIntCompare", args = { key = "heat.current", op = "gt", value = 0 } },
        { name = "StatusIntCompare", args = { key = "energy.current", op = "neq", value = 0 } },
    ] } },
]

[states.MonitorActive]
parent = "Root"
transitions = [
    { trigger = "EventHeatBurstNotification", target = "MonitorHeatBurst" },
    { trigger = "Tick", target = "MonitorGlobalReset", guard = "And", guard_args = { guards = [
        { name = "StatusIntCompare", args = { key = "heat.current", op = "eq", value = 0 } },
        { name = "StatusIntCompare", args = { key = "energy.current", op = "eq", value = 0 } }
    ] } },
]

[states.MonitorHeatBurst]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" },
]
transitions = [
    { trigger = "Tick", target = "MonitorActive" },
]

[states.MonitorGlobalReset]
parent = "Root"
on_enter = [
    # Cancel active sequences
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventDrainResume" },
    # Cancel enemy entities
    { action = "EmitEvent", event = "EventQuasarCancelRequest" },
    { action = "EmitEvent", event = "EventStormCancelRequest" },
    { action = "EmitEvent", event = "EventSwarmCancelRequest" },
    # Reset tracking
    { action = "ResetKillVars" },
    { action = "EmitEvent", event = "EventCycleDamageMultiplierReset" },
    # Terminate all gameplay regions
    { action = "TerminateRegion", region = "quasar" },
    { action = "TerminateRegion", region = "storm" },
    { action = "TerminateRegion", region = "placeholder" },
    { action = "TerminateRegion", region = "main" },
    # Reset visual cue
    { action = "EmitEvent", event = "EventStrobeRequest", payload = { color = { r = 255, g = 0, b = 0 }, intensity = 1.0, duration_ms = 250 } },
]
transitions = [
    { trigger = "Tick", target = "MonitorRespawn" },
]

[states.MonitorRespawn]
parent = "Root"
on_enter = [
    { action = "SpawnRegion", region = "main", initial_state = "MainSpawnGold" },
]
transitions = [
    { trigger = "Tick", target = "MonitorWarmup" },
]

# === Quasar Region ===

[states.QuasarFuse]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventGrayoutStart" },
    { action = "EmitEvent", event = "EventStrobeRequest", payload = { intensity = 0.8, duration_ms = 200 } },
    { action = "EmitEvent", event = "EventDrainPause" },
    { action = "EmitEvent", event = "EventFuseQuasarRequest" }
]
transitions = [
    { trigger = "EventQuasarSpawned", target = "QuasarGoldSpawn" }
]

[states.QuasarGoldCycle]
parent = "Root"
on_exit = [
    { action = "EmitEvent", event = "EventGoldCancel" }
]
transitions = [
    { trigger = "EventQuasarDestroyed", target = "QuasarEscalate", guard = "StatusIntCompare", guard_args = { key = "kills.quasar", op = "gte", value = 3 } },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
    { trigger = "Tick", target = "QuasarEscalate", guard = "StatusIntCompare", guard_args = { key = "kills.quasar", op = "gte", value = 3 } }
]

[states.QuasarGoldSpawn]
parent = "QuasarGoldCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" }
]
transitions = [
    { trigger = "EventGoldSpawned", target = "QuasarGoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "QuasarGoldSpawnRetry" }
]

[states.QuasarGoldSpawnRetry]
parent = "QuasarGoldCycle"
transitions = [
    { trigger = "Tick", target = "QuasarGoldSpawn", guard = "StateTimeExceeds", guard_args = { ms = 100 } }
]

[states.QuasarGoldActive]
parent = "QuasarGoldCycle"
transitions = [
    { trigger = "EventGoldComplete", target = "QuasarGoldComplete" },
    { trigger = "EventGoldTimeout", target = "QuasarGoldTimeout" },
    { trigger = "EventGoldDestroyed", target = "QuasarGoldDestroyed" }
]

[states.QuasarGoldComplete]
parent = "QuasarGoldCycle"
on_enter = [
    { action = "EmitEvent", event = "EventHeatAddRequest", payload = { delta = 100 } },
    { action = "EmitEvent", event = "EventEnergyAddRequest", payload = { delta = 1000, type = 1 } }
]
transitions = [
    { trigger = "Tick", target = "QuasarDustAll" }
]

[states.QuasarGoldTimeout]
parent = "QuasarGoldCycle"
transitions = [
    { trigger = "Tick", target = "QuasarDustAll" }
]

[states.QuasarGoldDestroyed]
parent = "QuasarGoldCycle"
transitions = [
    { trigger = "Tick", target = "QuasarDustAll" }
]

[states.QuasarDustAll]
parent = "QuasarGoldCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDustAllRequest" }
]
transitions = [
    { trigger = "Tick", target = "QuasarGoldSpawn" }
]

[states.QuasarExit]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" }
]

[states.QuasarEscalate]
parent = "Root"
on_enter = [
    { action = "ResetStatusInt", payload = { key = "kills.quasar" } },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventQuasarCancelRequest" },
    { action = "SpawnRegion", region = "storm", initial_state = "StormSetup" },
    { action = "TerminateRegion", region = "quasar" }
]

# === Storm Region ===

[states.StormSetup]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainPause" },
    { action = "EmitEvent", event = "EventStormSpawnRequest" }
]
transitions = [
    { trigger = "Tick", target = "StormActive", guard = "StateTimeExceeds", guard_args = { ms = 500 } }
]

[states.StormActive]
parent = "Root"
transitions = [
    { trigger = "EventStormDestroyed", target = "StormDecision" }
]

[states.StormDecision]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventCycleDamageMultiplierIncrease" },
    { action = "EmitEvent", event = "EventMetaStatusMessageRequest", payload = { message = "SPECIES DAMAGE INCREASED" } }
]
transitions = [
    { trigger = "Tick", target = "StormEscalatePlaceholderOne", guard = "StatusIntCompare", guard_args = { key = "kills.swarm", op = "gte", value = 20 } },
    { trigger = "Tick", target = "StormEscalatePlaceholderTwo", guard = "StatusIntCompare", guard_args = { key = "kills.storm", op = "gte", value = 3 } },
    { trigger = "Tick", target = "StormVictory" }
]

[states.StormEscalatePlaceholderOne]
parent = "Root"
on_enter = [
    { action = "ResetStatusInt", payload = { key = "kills.swarm" } },
    { action = "EmitEvent", event = "EventStormCancelRequest" },
    { action = "SpawnRegion", region = "placeholder", initial_state = "PlaceholderSetup" },
    { action = "TerminateRegion", region = "storm" }
]

[states.StormEscalatePlaceholderTwo]
parent = "Root"
on_enter = [
    { action = "ResetStatusInt", payload = { key = "kills.storm" } },
    { action = "EmitEvent", event = "EventStormCancelRequest" },
    { action = "SpawnRegion", region = "placeholder", initial_state = "PlaceholderSetup" },
    { action = "TerminateRegion", region = "storm" }
]

[states.StormVictory]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "storm" }
]

# === Placeholder Region ===

[states.PlaceholderSetup]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainPause" },
    { action = "EmitEvent", event = "EventStrobeRequest", payload = { color = { r = 255, g = 255, b = 255 }, intensity = 1.0, duration_ms = 500 } },
]
transitions = [
    { trigger = "Tick", target = "PlaceholderActive", guard = "StateTimeExceeds", guard_args = { ms = 500 } },
]

[states.PlaceholderActive]
parent = "Root"
# Placeholder: add gameplay transitions here
transitions = [
    { trigger = "Tick", target = "PlaceholderExit", guard = "StateTimeExceeds", guard_args = { ms = 5000 } }
]

[states.PlaceholderExit]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "placeholder" },
]
`