package asset

// DefaultGameplayFSMConfig is the embedded fallback FSM TOML configuration
const DefaultGameplayFSMConfig = `
[regions.main]
initial = "SpawnGold"

[regions.quasar]

[regions.storm]
enabled_systems = ["storm"]


# === Main gameplay region ===

[states.Gameplay]
parent = "Root"
transitions = [
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

[states.SpawnGold]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" },
]
transitions = [
    { trigger = "EventGoldSpawned", target = "GoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "DecayWait" },
]

[states.GoldActive]
parent = "Gameplay"
transitions = [
    { trigger = "EventGoldComplete", target = "PreSweepCheck" },
    { trigger = "EventGoldTimeout", target = "DecayWait" },
    { trigger = "EventGoldDestroyed", target = "DecayWait" },
]

[states.PreSweepCheck]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "SweepingHot", guard = "StatusBoolEquals", guard_args = { key = "heat.at_max", value = true } },
    { trigger = "Tick", target = "SweepingNormal" },
]

[states.SweepingHot]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" },
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "QuasarHandoff" },
]

[states.SweepingNormal]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventHeatAddRequest", payload = { delta = 100 } },
    { action = "EmitEvent", event = "EventEnergyAddRequest", payload = { delta = 1000, percentage = false, type = 1 } },
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" },
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "DecayWait" },
]

[states.DecayWait]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "DecayWave", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
]

[states.DecayWave]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventDecayWave" },
]
transitions = [
    { trigger = "Tick", target = "SpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
]

[states.QuasarHandoff]
parent = "Gameplay"
on_enter = [
    { action = "SpawnRegion", region = "quasar", initial_state = "QuasarFuse" },
    { action = "PauseRegion", region = "main" },
]
transitions = [
    { trigger = "Tick", target = "DecayWait" },
]


# === Quasar region ===

[states.QuasarCycle]
parent = "Root"
transitions = [
    { trigger = "EventHeatBurstNotification", target = "StormHandoff" },
]

[states.QuasarFuse]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutStart" },
    { action = "EmitEvent", event = "EventStrobeRequest", payload = { color = { r = 255, g = 0, b = 0 }, intensity = 1.0, duration_ms = 500 } },
    { action = "EmitEvent", event = "EventDrainPause" },
    { action = "EmitEvent", event = "EventFuseQuasarRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventQuasarSpawned", target = "QuasarGoldSpawn" },
]

[states.QuasarGoldSpawn]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventGoldSpawned", target = "QuasarGoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "QuasarGoldRetry" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
]

[states.QuasarGoldRetry]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "Tick", target = "QuasarGoldSpawn", guard = "StateTimeExceeds", guard_args = { ms = 100 } },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
]

[states.QuasarGoldActive]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventGoldComplete", target = "QuasarGoldReward" },
    { trigger = "EventGoldTimeout", target = "QuasarDustAll" },
    { trigger = "EventGoldDestroyed", target = "QuasarDustAll" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
]

[states.QuasarGoldReward]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventHeatAddRequest", payload = { delta = 100 } },
    { action = "EmitEvent", event = "EventEnergyAddRequest", payload = { delta = 1000, percentage = false, type = 1 } },
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
    { trigger = "Tick", target = "QuasarGoldSpawn" },
]

[states.QuasarDustAll]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDustAllRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "Tick", target = "QuasarGoldSpawn" },
]

[states.StormHandoff]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "SpawnRegion", region = "storm", initial_state = "StormSetup" },
    { action = "TerminateRegion", region = "quasar" },
]

[states.QuasarFail]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventCycleDamageMultiplierReset" },
    { action = "EmitEvent", event = "EventQuasarCancelRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarExit" },
]

[states.QuasarExit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" },
]


# === Storm region ===

[states.StormCycle]
parent = "Root"
transitions = [
    { trigger = "EventHeatBurstNotification", target = "StormBurstHandoff" },
]

[states.StormSetup]
parent = "StormCycle"
on_enter = [
    { action = "EmitEvent", event = "EventStormSpawnRequest" },
]
transitions = [
    { trigger = "Tick", target = "StormFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "Tick", target = "StormActive", guard = "StateTimeExceeds", guard_args = { ms = 100 } },
]

[states.StormActive]
parent = "StormCycle"
transitions = [
    { trigger = "Tick", target = "StormFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventStormDied", target = "StormVictory" },
]

[states.StormVictory]
parent = "StormCycle"
on_enter = [
    { action = "EmitEvent", event = "EventCycleDamageMultiplierIncrease" },
    { action = "EmitEvent", event = "EventMetaStatusMessageRequest", payload = { message = "Storm Defeated!" } },
]
transitions = [
    { trigger = "Tick", target = "MainHandoff" },
]

[states.StormFail]
parent = "StormCycle"
on_enter = [
    { action = "EmitEvent", event = "EventCycleDamageMultiplierReset" },
    { action = "EmitEvent", event = "EventStormCancelRequest" },
]
transitions = [
    { trigger = "Tick", target = "MainHandoff" },
]

[states.StormBurstHandoff]
parent = "StormCycle"
on_enter = [
    { action = "EmitEvent", event = "EventCycleDamageMultiplierIncrease" },
    { action = "EmitEvent", event = "EventStormCancelRequest" },
    { action = "EmitEvent", event = "EventMetaStatusMessageRequest", payload = { message = "Heat Burst! Multiplier++" } },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "storm" },
]

[states.MainHandoff]
parent = "StormCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "storm" },
]
`