package asset

// DefaultGameplayFSMConfig is the embedded fallback FSM TOML configuration
const DefaultGameplayFSMConfig = `
# === Root FSM configuration ===
# Simplified WASM-friendly gameplay script

# Global system configuration
[systems]
disabled_systems = ["wall", "fadeout", "navigation", "genetic", "music", "audio"]

[regions.main]
initial = "SpawnGold"

[regions.quasar]


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
    { trigger = "EventGoldSpawnFailed", target = "WaveWait" },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

[states.GoldActive]
parent = "Gameplay"
transitions = [
    { trigger = "EventGoldComplete", target = "Sweeping" },
    { trigger = "EventGoldTimeout", target = "WaveWait" },
    { trigger = "EventGoldDestroyed", target = "WaveWait" },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

# --- SWEEPING ---

[states.Sweeping]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventHeatAddRequest", payload = { delta = 100 } },
    { action = "EmitEvent", event = "EventEnergyAddRequest", payload = { delta = 1000, percentage = false, type = 1 } },
    { action = "EmitEvent", event = "EventCleanerSweepingRequest" },
]
transitions = [
    { trigger = "EventCleanerSweepingFinished", target = "WaveWait" },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

# --- WAVE CYCLE ---

[states.WaveWait]
parent = "Gameplay"
transitions = [
    { trigger = "Tick", target = "WaveEmit", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

[states.WaveEmit]
parent = "Gameplay"
on_enter = [
    { action = "EmitEvent", event = "EventDecayWave", guard = "VarEquals", guard_args = { var = "wave_type", value = 0 } },
    { action = "EmitEvent", event = "EventBlossomWave", guard = "VarEquals", guard_args = { var = "wave_type", value = 1 } },
    { action = "IncrementVar", payload = { name = "wave_type", delta = 1 } },
    { action = "ModuloVar", payload = { name = "wave_type", delta = 2 } },
]
transitions = [
    { trigger = "Tick", target = "SpawnGold", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
    { trigger = "EventHeatBurstNotification", target = "QuasarHandoff" },
]

# --- QUASAR HANDOFF ---

[states.QuasarHandoff]
parent = "Gameplay"
on_enter = [
    { action = "SpawnRegion", region = "quasar", initial_state = "QuasarFuse" },
    { action = "PauseRegion", region = "main" },
]
transitions = [
    { trigger = "Tick", target = "WaveWait" },
]


# === Quasar region states ===

[states.QuasarCycle]
parent = "Root"

[states.QuasarFuse]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutStart" },
    { action = "SetVar", payload = { name = "qwave_type", value = 0 } },
    { action = "EmitEvent", event = "EventFuseQuasarRequest" },
]
transitions = [
    { trigger = "EventQuasarSpawned", target = "QuasarWait" },
]

# --- QUASAR WAVE CYCLE ---

[states.QuasarWait]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventHeatBurstNotification", target = "QuasarBurstExit" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
    { trigger = "Tick", target = "QuasarWaveEmit", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
]

[states.QuasarWaveEmit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDecayWave", guard = "VarEquals", guard_args = { var = "qwave_type", value = 0 } },
    { action = "EmitEvent", event = "EventBlossomWave", guard = "VarEquals", guard_args = { var = "qwave_type", value = 1 } },
    { action = "IncrementVar", payload = { name = "qwave_type", delta = 1 } },
    { action = "ModuloVar", payload = { name = "qwave_type", delta = 2 } },
]
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = 0 } },
    { trigger = "EventHeatBurstNotification", target = "QuasarBurstExit" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
    { trigger = "Tick", target = "QuasarWait", guard = "StateTimeExceeds", guard_args = { ms = 5000 } },
]

# --- QUASAR END ---

[states.QuasarFail]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventQuasarCancelRequest" },
]
transitions = [
    { trigger = "Tick", target = "QuasarExit" },
]

[states.QuasarBurstExit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventQuasarCancelRequest" },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" },
]

[states.QuasarExit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" },
]
`