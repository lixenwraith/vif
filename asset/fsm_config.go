package asset

// DefaultGameplayFSMConfig returns the default FSM TOML configuration
const DefaultGameplayFSMConfig = `

# === Root FSM configuration ===
[regions]
main = { initial = "SpawnGold" }


# === Main gameplay region states ===

[states.Gameplay]
parent = "Root"

# --- GOLD SPAWN CYCLE ---

[states.SpawnGold]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" },
]
transitions = [
    { trigger = "EventGoldSpawned", target = "GoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "DecayWait" },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

[states.GoldActive]
parent = "Gameplay"
transitions = [
    { trigger = "EventGoldComplete", target = "PreSweepCheck" },
    { trigger = "EventGoldTimeout", target = "DecayWait" },
    { trigger = "EventGoldDestroyed", target = "DecayWait" },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

# --- SWEEPING EVALUATION ---

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

# --- QUASAR HANDOFF ---

[states.QuasarHandoff]
parent = "Gameplay"
on_enter = [
    { action = "SpawnRegion", region = "quasar", initial_state = "QuasarFuse" },
    { action = "PauseRegion", region = "main" },
]
transitions = [
    { trigger = "Tick", target = "DecayWait" },
]

# --- DECAY WAVES ---

[states.DecayWait]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "DecayAnimation", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

[states.DecayAnimation]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventDecayWave" },
]
transitions = [
    { trigger = "Tick", target = "SpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 3000 } },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]


# === Quasar region states ===
# spawned dynamically via SpawnRegion action

# --- QUASAR CYCLE ---

[states.QuasarCycle]
parent = "Root"

[states.QuasarFuse]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGrayoutStart" },
    { action = "EmitEvent", event = "EventFuseDrains" },
]
transitions = [
    { trigger = "EventQuasarSpawned", target = "QuasarGoldSpawn" },
]

[states.QuasarGoldSpawn]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventHeatBurstNotification", target = "QuasarDustAll" },
    { trigger = "EventGoldSpawned", target = "QuasarGoldActive" },
    { trigger = "EventGoldSpawnFailed", target = "QuasarGoldRetry" },
    { trigger = "EventQuasarDestroyed", target = "QuasarWin" },
]

[states.QuasarGoldRetry]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventHeatBurstNotification", target = "QuasarDustAll" },
    { trigger = "Tick", target = "QuasarGoldSpawn", guard = "StateTimeExceeds", guard_args = { ms = 100 } },
    { trigger = "EventQuasarDestroyed", target = "QuasarWin" },
]

[states.QuasarGoldActive]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventHeatBurstNotification", target = "QuasarDustAll" },
    { trigger = "EventGoldTimeout", target = "QuasarGoldSpawn" },
    { trigger = "EventGoldDestroyed", target = "QuasarGoldSpawn" },
    { trigger = "EventQuasarDestroyed", target = "QuasarWin" },
]

# --- DUST ALL ---
# Overheat burst

[states.QuasarDustAll]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDustAll"},
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventQuasarDestroyed", target = "QuasarWin" },
]

# --- QUASAR END ---

[states.QuasarWin]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventBuffAddRequest", payload = { buff = 0 } },
]
transitions = [
    { trigger = "Tick", target = "QuasarExit" },
]

[states.QuasarFail]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventQuasarCancelRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarExit" },
]

# --- MAIN HANDOFF ---

[states.QuasarExit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" },
]
`