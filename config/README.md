# FSM Configuration Reference

## File Structure

```
config/
├── game.toml           # Root config (required)
└── regions/            # External region files
    ├── main.toml
    ├── quasar.toml
    └── maze.toml
```

---

## Root Config (`game.toml`)

```toml
[systems]
disabled = ["system_name", ...]     # Disabled at FSM init

[regions.region_name]
initial = "StateName"               # Initial state (omit for dynamic regions)
file = "regions/file.toml"          # External state definitions
enabled_systems = ["system_name"]   # Enable on region spawn
disabled_systems = ["system_name"]  # Disable on region spawn
```

---

## State Definition

```toml
[states.StateName]
parent = "ParentState"              # Default: "Root"
on_enter = [...]                    # Actions on state entry
on_update = [...]                   # Actions every tick while active
on_exit = [...]                     # Actions on state exit
transitions = [...]                 # Transition rules
```

---

## Transitions

```toml
{ trigger = "EventName", target = "TargetState" }
{ trigger = "EventName", target = "TargetState", guard = "GuardName" }
{ trigger = "EventName", target = "TargetState", guard = "GuardName", guard_args = { ... } }
{ trigger = "Tick", target = "TargetState", guard = "StateTimeExceeds", guard_args = { ms = 1000 } }
```

`Tick` — evaluated every frame; use with guard to control timing.

---

## Actions

### Event Emission
```toml
{ action = "EmitEvent", event = "EventName" }
{ action = "EmitEvent", event = "EventName", payload = { field = value, ... } }
{ action = "EmitEvent", event = "EventName", payload = { field = 0 }, payload_vars = { field = "var_name" } }
```

`payload_vars` — injects FSM variable value into payload field at runtime.

### Region Control
```toml
{ action = "SpawnRegion", region = "region_name", initial_state = "StateName" }
{ action = "TerminateRegion", region = "region_name" }
{ action = "PauseRegion", region = "region_name" }
{ action = "ResumeRegion", region = "region_name" }
```

### Variables
```toml
{ action = "SetVar", payload = { name = "var_name", value = 0 } }
{ action = "IncrementVar", payload = { name = "var_name", delta = 1 } }
{ action = "DecrementVar", payload = { name = "var_name", delta = 1 } }
```

`delta` defaults to 1 if omitted.

### System Control
```toml
{ action = "EnableSystem", payload = { system_name = "name" } }
{ action = "DisableSystem", payload = { system_name = "name" } }
```

### Action Modifiers

```toml
{ action = "...", delay_ms = 500 }                    # Delay execution
{ action = "...", guard = "GuardName" }               # Conditional execution
{ action = "...", guard = "GuardName", guard_args = { ... } }
```

---

## Guards

### Factory Guards (parameterized)

| Guard               | Args                   | Description                          |
|---------------------|------------------------|--------------------------------------|
| `StateTimeExceeds`  | `ms`                   | Time in current state ≥ ms           |
| `StatusBoolEquals`  | `key`, `value`         | Status registry bool equals value    |
| `StatusIntCompare`  | `key`, `op`, `value`   | Status registry int comparison       |
| `RegionExists`      | `region`               | Region is currently active           |
| `VarEquals`         | `var`, `value`         | FSM variable equals value            |
| `VarCompare`        | `var`, `op`, `value`   | FSM variable comparison              |
| `ConfigIntCompare`  | `field`, `op`, `value` | ConfigResource int field comparison  |
| `ConfigBoolCompare` | `field`, `value`       | ConfigResource bool field comparison |

**Operators (`op`):** `eq`, `neq`, `gt`, `gte`, `lt`, `lte`

### Static Guards

| Guard                 | Description         |
|-----------------------|---------------------|
| `AlwaysTrue`          | Always passes       |
| `StateTimeExceeds2s`  | Time in state > 2s  |
| `StateTimeExceeds10s` | Time in state > 10s |

---

## Variable Payload Injection

Inject FSM variable values into event payloads:

```toml
{ action = "EmitEvent", event = "EventHeatAddRequest", payload = { delta = 0 }, payload_vars = { delta = "damage_multiplier" } }
```

Supported field types: `int`, `int64`, `uint`, `float64`

---

## Config Fields

Available fields for `ConfigIntCompare`:

| Field             | Description                            |
|-------------------|----------------------------------------|
| `color_mode`      | Render mode (0=256-color, 1=TrueColor) |
| `map_width`       | Simulation bounds width                |
| `map_height`      | Simulation bounds height               |
| `viewport_width`  | Terminal visible width                 |
| `viewport_height` | Terminal visible height                |
| `camera_x`        | Camera X offset                        |
| `camera_y`        | Camera Y offset                        |

Available fields for `ConfigBoolCompare`:

| Field            | Description          |
|------------------|----------------------|
| `crop_on_resize` | Resize behavior flag |

---

## Game Time

`time.game_elapsed_ms` available via `StatusIntCompare` (resets each game session):

```toml
{ trigger = "Tick", target = "LateGame", guard = "StatusIntCompare", guard_args = { key = "time.game_elapsed_ms", op = "gte", value = 60000 } }
```

---

## Execution Order

1. **FSM Init** — Apply `[systems].disabled`, enter initial states
2. **Region Spawn** — Apply region `enabled_systems`/`disabled_systems`
3. **State Enter** — Execute `on_enter` actions (root → leaf)
4. **Tick** — Process delayed actions, execute `on_update`, evaluate transitions
5. **State Exit** — Execute `on_exit` actions (leaf → root)

---

## Notes

- Transitions evaluated in definition order; first matching wins
- Delayed actions cleared on state exit
- Variables persist across state transitions, cleared on FSM reset
- Region system toggles applied immediately via `EventMetaSystemCommandRequest`