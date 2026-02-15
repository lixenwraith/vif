package asset

// DefaultGameplayFSMConfig is the embedded fallback FSM TOML configuration
const DefaultGameplayFSMConfig = `
# === Root FSM configuration ===

# Global system configuration
# Systems listed here are disabled at FSM init
[systems]
# disabled = ["drain", "swarm", "quasar", "storm"]  # Uncomment for typing-focused mode

[regions.main]
initial = "SpawnGold"

[regions.quasar]
enabled_systems = ["quasar"]

[regions.maze]
enabled_systems = ["storm"]


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


# === Quasar region states ===

[states.QuasarCycle]
parent = "Root"

[states.QuasarFuse]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventDrainPause" },
    { action = "EmitEvent", event = "EventGrayoutStart" },
    { action = "EmitEvent", event = "EventFuseQuasarRequest" },
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
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
]

[states.QuasarGoldRetry]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventHeatBurstNotification", target = "QuasarDustAll" },
    { trigger = "Tick", target = "QuasarGoldSpawn", guard = "StateTimeExceeds", guard_args = { ms = 100 } },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
]

[states.QuasarGoldActive]
parent = "QuasarCycle"
transitions = [
    { trigger = "Tick", target = "QuasarFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventHeatBurstNotification", target = "MazeHandoff" },
    { trigger = "EventGoldTimeout", target = "QuasarDustAll" },
    { trigger = "EventGoldDestroyed", target = "QuasarDustAll" },
    { trigger = "EventQuasarDestroyed", target = "QuasarExit" },
]

# --- DUST ALL ---

[states.QuasarDustAll]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventDustAllRequest"},
]
transitions = [
    { trigger = "Tick", target = "QuasarGoldSpawn"},
]

# --- MAZE HANDOFF ---

[states.MazeHandoff]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "SpawnRegion", region = "maze", initial_state = "MazeSetup" },
    { action = "TerminateRegion", region = "quasar" },
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

# --- MAIN HANDOFF ---

[states.QuasarExit]
parent = "QuasarCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventGrayoutEnd" },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "quasar" },
]


# === Maze region states ===

[states.MazeCycle]
parent = "Root"

# --- MAZE SETUP ---

[states.MazeSetup]
parent = "MazeCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventStormCancelRequest" },
    { action = "EmitEvent", event = "EventLevelSetup", payload = { width = 500, height = 250, clear_entities = true } },
    { action = "EmitEvent", event = "EventMazeSpawnRequest", payload = { cell_width = 20, cell_height = 10, braiding = 0.5, block_mask = 255, visual = { box_style = 1, fg_color = { r = 200, g = 200, b = 200 }, render_fg = true, bg_color = { r = 40, g = 40, b = 40 }, render_bg = true }, room_count = 1, rooms = [{ center_x = 250, center_y = 125, width = 100, height = 40 }] } },
    { action = "EmitEvent", event = "EventWallPushCheckRequest" },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "EmitEvent", event = "EventStormSpawnRequest" },
]
transitions = [
    { trigger = "Tick", target = "MazeActive", guard = "StateTimeExceeds", guard_args = { ms = 100 } },
]

[states.MazeActive]
parent = "MazeCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" },
]
transitions = [
    { trigger = "EventHeatBurstNotification", target = "MainHandoff" },
    { trigger = "Tick", target = "MazeFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventStormDied", target = "MazeVictory" },
    { trigger = "EventGoldComplete", target = "MazeGoldRespawn" },
    { trigger = "EventGoldTimeout", target = "MazeGoldRespawn" },
    { trigger = "EventGoldDestroyed", target = "MazeGoldRespawn" },
]

[states.MazeGoldRespawn]
parent = "MazeCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldSpawnRequest" },
]
transitions = [
    { trigger = "EventHeatBurstNotification", target = "MainHandoff" },
    { trigger = "Tick", target = "MazeFail", guard = "StatusIntCompare", guard_args = { key = "heat.current", op = "eq", value = "0" } },
    { trigger = "EventStormDied", target = "MazeVictory" },
    { trigger = "EventGoldSpawned", target = "MazeActive" },
]

# --- MAZE END ---

[states.MazeVictory]
parent = "MazeCycle"
on_enter = [
    { action = "EmitEvent", event = "EventMetaStatusMessageRequest", payload = { message = "Storm Died!" } },
]
transitions = [
    { trigger = "Tick", target = "MainHandoff", guard = "StateTimeExceeds", guard_args = { ms = 2000 } },
]

[states.MazeFail]
parent = "MazeCycle"
on_enter = [
    { action = "EmitEvent", event = "EventStormCancelRequest" },
]
transitions = [
    { trigger = "Tick", target = "MainHandoff" },
]

# --- MAIN HANDOFF ---

[states.MainHandoff]
parent = "MazeCycle"
on_enter = [
    { action = "EmitEvent", event = "EventGoldCancel" },
    { action = "EmitEvent", event = "EventDrainPause" },
    { action = "EmitEvent", event = "EventWallDespawnAll" },
    { action = "EmitEvent", event = "EventLevelSetup", payload = { width = 0, height = 0, clear_entities = true } },
    { action = "EmitEvent", event = "EventDrainResume" },
    { action = "TerminateRegion", region = "quasar" },
    { action = "ResumeRegion", region = "main" },
    { action = "TerminateRegion", region = "maze" },
]
`