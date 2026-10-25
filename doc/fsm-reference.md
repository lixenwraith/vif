# FSM Configuration Reference

## Configuration Resolution

Entry config search order:

1. `-s <path-or-name>`, or `[paths] scenario` in `vif.toml` — file, directory
   containing `scenario.toml`, or a name resolved as `scenario/<name>/scenario.toml`
   under the configured roots;
2. `scenario/main/scenario.toml` under `-config-dir <root>`;
3. the same under the user root, normally `~/.config/vif`;
4. the same under the checkout's `wad/` for a binary in its `bin/`, then under
   `/usr/local/etc/vif` and `/etc/vif`;
5. embedded default (`internal/asset/scenario/`; forced with `-d`).

`-d` is mutually exclusive with both `-s` and `-f`; by itself it selects the
embedded scenario and content corpus. For the runtime design behind this reference, see
[`doc/fsm-and-configuration.md`](fsm-and-configuration.md). The complete
cross-resource order is in [`doc/filesystem-layout.md`](filesystem-layout.md). A bot
graph is written in this dialect with its own actions and guards
([Bots](bots.md#writing-a-graph)).

The embedded main campaign is the only shipped source of `main`; `-s main`
falls back to it. A `scenario/main/` directory is an optional user override.

Region `file` and `macro_files` references resolve relative to the entry config's
directory and cannot climb out of it. A region file holds only `[states]`, and a
state defined twice across files fails the load. A scenario is at most 64 `.toml`
files, 256 KiB each and 1 MiB in all. A joiner plays the coordinator's exact bytes:
its own copy when the digests match, else the coordinator's, sent under the same
bounds and held in memory. Installed layout:

```text
~/.config/vif/
├── scenario/
│   ├── main/              # optional override: a copy of internal/asset/scenario/
│   ├── blank/scenario.toml
│   ├── simon/scenario.toml
│   └── td/
│       ├── scenario.toml
│       ├── td_macros.toml # macro library
│       └── td_*.toml      # region files
└── input/
    └── keymap.toml
```

`vif -check [-s <name-or-path>]` validates the resolved settings, scenario (FSM, the
system names it toggles and the required dependencies a disable strands), keymap,
audio overrides, content and every bot graph, then exits; all state-level FSM
errors are reported in one pass. `vif -schema` prints the machine schema (events
with payload fields, guards, actions, operators and config fields) as JSON.

---

## Root Config (`scenario.toml`)

```toml
description = "A short description for scenario selection."
macro_files = ["macros.toml"]       # Macro libraries, relative to this file

[systems]
disabled_systems = ["system_name", ...] # Disabled at FSM init
only_systems = ["system_name", ...]     # Or (not both): every other toggled system disabled

[map]
min_width = 50                      # Floor under the map's size (optional)
min_height = 20
fixed = "500x250"                   # Size the map holds throughout, for front-ends (optional)
cursors = false                     # Players hold slots, no cursor on the map (optional)

[regions.region_name]
initial = "StateName"               # Initial state (omit for dynamic regions)
file = "file.toml"                  # External state definitions
background = true                   # Left out of the status bar's fsm.state summary
enabled_systems = ["system_name"]   # On while the region runs
disabled_systems = ["system_name"]  # Off while it runs, unless a running system requires it
only_systems = ["system_name"]      # Or (not both with disabled_systems): every toggled
                                    # system no running region names is off while it runs
```

---

## Macros

A repeated shape is written once and invoked by name; the loader expands it before
anything below sees the document. The entry and its `macro_files` define macros,
and every scenario file, the entry included, invokes them:

```toml
# macros.toml
[macros.Gateway]
params = { group = "int", count = "string", next = "string" }
body = { parent = "Root", on_enter = [
    { action = "EmitEvent", event = "EventGatewaySpawnRequest", payload = { anchor_entity = 0, species = 7, group_id = "${group}" }, payload_vars = { anchor_entity = "gw_anchor" } },
    { action = "IncrementVar", payload = { name = "${count}" } },
], transitions = [{ trigger = "Tick", target = "${next}" }] }

# a region file
[states]
TdGwTL = { macro = "Gateway", group = 3, count = "l_p1n", next = "TdP1TR" }
```

Types, defaults, splicing into arrays, `each` and the rules are in
[TOML macros](toml-macros.md); `wad/scenario/td/td_macros.toml` is a full library.

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
{ trigger = "EventName", target = "TargetState", guard = "GuardName", guard_args = { ... } }
{ trigger = "Tick", target = "TargetState", guard = "StateTimeExceeds", guard_args = { ms = 1000 } }
```

`Tick` — evaluated every game tick; use with guard to control timing.

A region is shared state: in a live session the authority's machine takes its
transitions and every instance applies each at one tick, a playout lead after the
authority took it; the unguarded `Tick` hops after a transition's target ride the
same crossing, so a chain of them costs one lead. A `[local]` event (its
class in `internal/event/type.go`) cannot steer a region, since only the
authority's player could move it (D-20). The loader accepts one;
`TestFSMTriggersAreReplicated` holds the shipped scenarios to the rule.

### Variable Capture from Payloads

Extract event payload fields into FSM variables when a transition matches:

```toml
{ trigger = "EventSpeciesCreated", target = "NextState",
  guard = "PayloadIntCompare", guard_args = { field = "species", op = "eq", value = 5 },
  capture_vars = { entity = "pylon_entity" } }
```

`species` uses the `component.SpeciesType` values: drain `1`, swarm `2`,
quasar `3`, storm `4`, pylon `5`, snake `6`, eye `7`, tower `8`, and kraken `9`.

`capture_vars` maps payload field names to FSM variable names. Fields resolve by `toml` struct tag first, then Go field name. Captured values are set **before** the target state's `on_enter` executes.

Supported field kinds: all signed and unsigned integer widths (including
`core.Entity`), `float32`, `float64`, and `bool` (`true` = 1, `false` = 0).

Combine with guards and payload injection:

```toml
# Capture a pylon from the shared species response, then inject it into a follow-up request
{ trigger = "EventSpeciesCreated", target = "AttachGateway",
  guard = "PayloadIntCompare", guard_args = { field = "species", op = "eq", value = 5 },
  capture_vars = { entity = "anchor_id" } }
# In AttachGateway on_enter:
{ action = "EmitEvent", event = "EventGatewaySpawnRequest", payload = { anchor_entity = 0 }, payload_vars = { anchor_entity = "anchor_id" } }
```

### Transition Actions

Actions attached to a transition execute between the exit and enter phases:

```toml
{ trigger = "EventGoldCompleted", target = "NextWave", actions = [
    { action = "IncrementVar", payload = { name = "wave" } },
] }
```

Execution order on a matching transition:
`capture_vars` → `on_exit` (leaf → LCA) → transition `actions` → `on_enter` (LCA → target leaf).

A transition targeting the currently active state executes its actions only
(no exit/enter, no time reset).

### Internal Transitions

`internal = true` consumes the trigger without changing state. No `target`,
no `on_exit`/`on_enter`, `TimeInState` unaffected. Declared on a composite
state, it fires while **any** descendant is active — the pattern for counting
events without leaving the current state:

```toml
[states.TowerCycle]
transitions = [
    { trigger = "EventSpeciesKilled", internal = true,
      guard = "PayloadIntCompare", guard_args = { field = "species", op = "eq", value = 5 }, actions = [
        { action = "IncrementVar", payload = { name = "pylons_dead" } },
    ] },
]
```

An internal transition consumes the event/tick and stops bubbling; place it
below higher-priority transitions of the same trigger. An internal `Tick`
transition with a passing guard executes every tick and shadows all outer
`Tick` transitions — equivalent to a guarded `on_update`.

---

## Lifecycle Hooks

`on_enter` — root → leaf on entry. `on_exit` — leaf → root on exit.
`on_update` — every tick for **every state on the active path**, root → leaf.

### Delayed Actions

`delay_ms` counts down in region time and survives transitions while the
owning state remains on the active path. Delayed actions are cleared when
their owning state exits. An action's guard is checked when it is scheduled, not
when its delay runs out. Transition actions with `delay_ms` are owned by the
target state; internal-transition actions by the active leaf. Captures store a
deterministic compiled action identity, so delayed entry, update, and transition
actions resume the same work after join or correction.

### Reconciled Local Lifecycle

A live snapshot install moves the Shared FSM directly to the authority's state.
It does not replay normal entry/exit work because the captured Shared world
already contains those results. For a persistent instance-local hold, mark the
immediate `ClassLocal` event actions on the common parent that owns its lifetime:

```toml
[states.EncounterHold]
parent = "Root"
on_enter = [
  { action = "EmitEvent", event = "EventDrainPause", reconcile = true },
]
on_exit = [
  { action = "EmitEvent", event = "EventDrainResume", reconcile = true },
]

[states.EncounterSetup]
parent = "EncounterHold"
```

Marked exits run leaf-to-root with old variables, then marked entries run
root-to-leaf with imported variables. A changed `payload_vars` value also moves
the scope by releasing the old hold before acquiring the new one. The staging
world emits no reconciliation events.

`reconcile = true` is valid only on an unguarded, non-delayed `EmitEvent` in
`on_enter`/`on_exit`, and its event must be `ClassLocal`. Never mark rewards,
spawns, sounds, strobes, or other one-shot work. Acquisition and release belong
on one common parent so normal transitions and whole-region retirement use the
same boundary.

---

## Actions

### Event Emission

```toml
{ action = "EmitEvent", event = "EventName" }
{ action = "EmitEvent", event = "EventName", payload = { field = value, ... } }
{ action = "EmitEvent", event = "EventHeatAddRequest", payload = { entity = 0, delta = 0 }, payload_vars = { entity = "player_entity", delta = "bonus" } }
```

The event name and payload are compiled at load: an unknown event or a value of the
wrong type fails it. Without `payload` the event carries its payload type
zero-valued. `payload_vars` injects an FSM variable's value into a payload field at
runtime. Supported field kinds: all signed and unsigned integer widths, `float32`,
`float64`, `bool`, and `string`; a negative value is ignored for an unsigned field.
A string takes the value's decimal form in place of each `{}`, so
`payload = { message = "Level {}" }, payload_vars = { message = "level" }` reads
"Level 3".

### Region Control

```toml
{ action = "SpawnRegion", region = "region_name", initial_state = "StateName" }
{ action = "TerminateRegion", region = "region_name" }
{ action = "PauseRegion", region = "region_name" }
{ action = "ResumeRegion", region = "region_name" }
```

`region` must be declared under `[regions]` and `initial_state` must name a state,
or the load fails. Spawning an active region does nothing. Each of the four
reconciles the systems with the regions that now run, so a region's
`enabled_systems`, `disabled_systems` and `only_systems` hold while it runs and
hand back when it pauses or ends ([System Control](#system-control)). A paused
region keeps its state, time and pending delays, runs nothing and drops its events.

### Variables

```toml
{ action = "SetVar", payload = { name = "var_name", value = 0 } }
{ action = "IncrementVar", payload = { name = "var_name", delta = 1 } }
{ action = "DecrementVar", payload = { name = "var_name", delta = 1 } }
```

`delta` defaults to 1 for `IncrementVar`, `DecrementVar`, `MultiplyVar` and
`DivideVar` when it is omitted or zero and no `source_var` is given.

### Arithmetic Actions

```toml
{ action = "MultiplyVar", payload = { name = "var_name", delta = 2 } }
{ action = "DivideVar", payload = { name = "var_name", delta = 2 } }
{ action = "ModuloVar", payload = { name = "var_name", delta = 10 } }
{ action = "ClampVar", payload = { name = "var_name", min = 0, max = 100 } }
{ action = "CopyVar", payload = { name = "dest_var", source_var = "src_var" } }
```

Division or modulo by zero is a no-op; `ModuloVar` has no default `delta`, so
omitting it is one too.

### Variable-to-Variable Operations

Use `source_var` to reference another variable's value instead of a literal:

```toml
# Add wave_bonus to score
{ action = "IncrementVar", payload = { name = "score", source_var = "wave_bonus" } }

# Set damage to base_damage * multiplier (two steps)
{ action = "CopyVar", payload = { name = "damage", source_var = "base_damage" } }
{ action = "MultiplyVar", payload = { name = "damage", source_var = "multiplier" } }
```

### Clamped Operations

Apply bounds with `min`/`max` (optional, independent):

```toml
{ action = "IncrementVar", payload = { name = "heat", delta = 10, max = 100 } }
{ action = "DecrementVar", payload = { name = "energy", delta = 5, min = 0 } }
{ action = "SetVar", payload = { name = "level", source_var = "raw_level", min = 1, max = 99 } }
```

### Random Draw

```toml
{ action = "RandomVar", payload = { name = "color", min = 1, max = 4, seed_var = "seed", index_var = "step" } }
```

Sets `name` to a value in `[min, max]` that is a pure function of the run's seed,
`seed_var` and `index_var`. The same index gives the same value, so a sequence is
recomputed element by element instead of stored. The run's seed is shared session
identity and moves on every reset, so every instance draws the same values (D-8).
`name` and `index_var` are required; `seed_var` is optional and reseeds a sequence
within a run. A `max` below `min` fails the load.

### Config to Variable

```toml
{ action = "ConfigToVar", payload = { field = "viewport_width", name = "vw" } }
```

Reads a `ConfigResource` field value into an FSM variable at execution time. Enables
dynamic position arithmetic based on runtime dimensions. Under a shared map the
viewport fields read the map bounds, so a level laid out around them is the same on
every participant (D-14).

Available fields (same set as `ConfigIntCompare`):
`map_width`, `map_height`, `viewport_width`, `viewport_height`, `camera_x`, `camera_y`, `color_mode`

### Nested payload_vars (Dot-Path Injection)

`payload_vars` supports dot-separated paths for injecting FSM variables into
nested struct fields and slice elements. Each path segment resolves by `toml`
struct tag first, then Go field name. Numeric segments index into slices.

**Syntax:** Keys containing dots must be quoted in TOML.

```toml
# Inject variable "cx" into Rooms[0].CenterX
{ action = "EmitEvent", event = "EventMazeSpawnRequest",
  payload = { rooms = [{ width = 15, height = 11 }] },
  payload_vars = { "rooms.0.center_x" = "cx", "rooms.0.center_y" = "cy" }
}
```

**Path resolution per segment:**

- Struct field: matches `toml:"tag_name"`, falls back to Go field name
- Slice index: numeric literal (0-based), must be within pre-defined slice bounds
- Pointer: auto-dereferenced

The payload slice must be pre-populated in `payload` with the correct number of
elements. `payload_vars` overwrites individual fields; it does not append
elements. A key without dots names a top-level field.

### System Control

```toml
{ action = "EnableSystem", payload = { system_name = "name" } }
{ action = "DisableSystem", payload = { system_name = "name" } }
```

A disable is dropped while a running system requires it. `ReconcileSystems` sets
the systems to what the running regions want: the union of their `only_systems`
lists, or the `[systems]` baseline when none has one, plus their `enabled_systems`,
less their `disabled_systems`, with every system left running keeping what it
requires on. A system the running regions decide, or whose wanted state changed,
goes to it; any other keeps what `EnableSystem`, `DisableSystem` or `:system` left
it at. The runtime runs it after init, reset, an installed capture and each
`:region` command, and the region actions run it, so a scenario has no need to.

### Status Registry Actions

```toml
{ action = "StatusToVar", payload = { key = "kills.storm", name = "storm_mark" } }
```

Scripts read the status registry and never write it. `StatusToVar` marks where a
count starts, and `StatusIntCompare` with `since = "storm_mark"` compares the value
minus that mark, so a scenario counts kills since its own escalation while
`kills.*` keeps the game's totals.

### Action Modifiers

```toml
{ action = "...", delay_ms = 500 }                    # Delay execution
{ action = "...", guard = "GuardName" }               # Conditional execution
{ action = "...", guard = "GuardName", guard_args = { ... } }
{ action = "EmitEvent", event = "EventDrainPause", reconcile = true } # Persistent local lifecycle only
```

An action guard sees no event payload, so a payload guard there is always false.

## Cursor Lifecycle

The world spawns with no cursor. A configuration must spawn one explicitly:

```toml
{ action = "EmitEvent", event = "EventCursorSpawnRequest", payload = { center = true, heat = 10, energy = 100 } }
{ action = "EmitEvent", event = "EventCursorSpawnRequest", payload = { x = 10, y = 5, slot = 1, heat = 10, energy = 100 } }
{ action = "EmitEvent", event = "EventCursorDespawnRequest", payload = { all = true } }
```

`center` overrides `x`/`y`; `auto` overrides `slot` with the lowest free index;
`heat` and `energy` are the cursor's starting values, zero when omitted, and the
first spawn's pair is what every joining participant's cursor starts with. A despawn
names `all`, an `entity` or a `slot`. `EventCursorSpawned` carries `entity`, `slot`,
`x`, `y`; `EventCursorSpawnFailed` signals a full roster, a taken slot or a blocked
map. A cursor-less configuration is legal.

With `[map] cursors = false` a spawn, a joiner's arrival included, rosters its slot
with no cursor on the map: only its slot, owner and control, so nothing draws,
moves, targets or drains it, and `EventCursorSpawned` carries `x = -1`, `y = -1`.
Its player plays through the keys the scenario captures and their buttons, each
press from that slot, and counts in `player.count`.

Capture the entity to address that cursor in later actions:

```toml
{ trigger = "EventCursorSpawned", target = "Arm", capture_vars = { entity = "player_entity" } }
# In Arm on_enter:
{ action = "EmitEvent", event = "EventHeatSetRequest", payload = { entity = 0, value = 10 }, payload_vars = { entity = "player_entity" } }
```

A player-scoped command names its cursor in `entity`; zero names none. The
owner-authored ones (energy, heat, boost, shield, weapon; D-13) act only on a cursor
this instance owns, the rest (move, ping, typing, gold jump, tower) on any live one.
A captured entity names the first cursor on every instance, so to set each
participant's own heat, energy and ember emit `EventCursorArmRequest { heat, energy,
ember }`, which acts on the cursor this instance drives, as `wad/scenario/simon` does.

---

## Player Metrics

Per-cursor state is published under `player.<slot>.<metric>`:

| Key                              | Description                |
|----------------------------------|----------------------------|
| `player.<n>.entity`              | Entity id, 0 when empty    |
| `player.<n>.control`             | 0=this instance drives it (a person or a bot), 2=remote, -1=empty |
| `player.<n>.x`, `player.<n>.y`   | Position, -1 when empty or off the map |
| `player.<n>.energy.current`      | Energy                     |
| `player.<n>.heat.current`        | Heat                       |
| `player.<n>.heat.overheat`       | Overheat accumulator       |
| `player.<n>.heat.at_max`         | Heat at cap                |
| `player.<n>.heat.ember`          | Ember decay active         |
| `player.<n>.shield.active`       | Shield up                  |
| `player.<n>.boost.active`, `player.<n>.boost.remaining` | Boost up, its time left in ns |
| `player.<n>.weapon.<kind>`       | Holds `rod`, `launcher`, `disruptor`, `turret`, `emitter` or `gun` |
| `player.<n>.weapon.orbs`         | Orbs                       |
| `player.<n>.typing.max_streak`   | Longest correct run        |

`player.count` and `player.local` describe the roster.
`session.any_defeated` becomes true when any rostered cursor reports zero heat
and zero energy; all shipped scenarios use it for game over.
`session.all_defeated` serves a scenario that needs unanimous defeat.
Both derive from replicated owner reports and are safe shared-region guards.
Reports carry their production tick; a reset rejects reports from the previous attempt.

The bare keys `energy.current`, `heat.current`, `heat.overheat`, `heat.at_max`,
`heat.ember`, `shield.active`, `boost.active`, `boost.remaining`, `weapon.rod`,
`weapon.launcher`, `weapon.disruptor`, `weapon.turret`, `weapon.emitter`, `weapon.gun`,
`weapon.orbs`, `typing.max_streak`, `player.x`, `player.y` mirror **the slot this
instance drives**. That is what "the player" means on a status bar or in an operator
command. The mirror follows the roster rather than slot 0 because slot 0 is the
coordinator's cursor, which no guest authors.

They are therefore **instance-local by construction and must not be a guard key
in a shared region** — a shared region has to take the same transition on every
instance (D-20), and a guard that names a cursor names its slot:
`player.0.energy.current`. A participant that drives no cursor — a dedicated
host — mirrors nothing and the bare keys read as reset. A bot graph reads its own
instance, where the bare keys are the bot's slot; see
[Bots](bots.md#writing-a-graph).

Occurrence counters stay roster-wide totals: `typing.correct`, `typing.errors`,
`shield.shield_hit`, `energy.spend_count`, `energy.crossed_zero_count`;
`energy.damage_multiplier` is one value for the session.

---

## Guards

### Factory Guards (parameterized)

| Guard               | Args                   | Description                          |
|---------------------|------------------------|--------------------------------------|
| `StateTimeExceeds`  | `ms`                   | Time in current state ≥ ms; `ms` must be positive |
| `StatusBoolEquals`  | `key`, `value`         | Status registry bool equals value (default `true`) |
| `StatusIntCompare`  | `key`, `op`, `value`, `since` | Status registry int comparison; `since` subtracts a variable |
| `RegionExists`      | `region`               | Region is spawned, paused or not     |
| `VarEquals`         | `var`, `value`         | FSM variable equals value            |
| `VarCompare`        | `var`, `op`, `value`   | FSM variable comparison              |
| `ConfigIntCompare`  | `field`, `op`, `value` | ConfigResource int field comparison  |
| `ConfigBoolCompare` | `field`, `value`       | ConfigResource bool field equals value (default `true`) |
| `VarCompareVar`     | `var_a`, `op`, `var_b` | Compare two FSM variables            |

**Operators (`op`):** `eq`, `neq`, `gt`, `gte`, `lt`, `lte`. An omitted or
unrecognized `op` compares `eq`; the loader does not reject one. A status key the
registry does not hold makes a status guard false.

```toml
{ trigger = "Tick", target = "Victory", guard = "VarCompareVar", guard_args = { var_a = "kills", op = "gte", var_b = "target_kills" } }
```

### Static Guards

| Guard                 | Description         |
|-----------------------|---------------------|
| `AlwaysTrue`          | Always passes       |
| `StateTimeExceeds2s`  | Time in state > 2s  |
| `StateTimeExceeds10s` | Time in state > 10s |

---

## Compound Guards

Combine multiple guards with logical operators.

### And (all must pass)

```toml
{ trigger = "Tick", target = "TargetState", guard = "And", guard_args = { guards = [
    { name = "StatusIntCompare", args = { key = "player.0.heat.current", op = "eq", value = 0 } },
    { name = "StatusIntCompare", args = { key = "player.0.energy.current", op = "eq", value = 0 } }
]}}
```

### Or (at least one must pass)

```toml
{ trigger = "Tick", target = "Critical", guard = "Or", guard_args = { guards = [
    { name = "StatusIntCompare", args = { key = "player.0.energy.current", op = "lte", value = 10 } },
    { name = "VarEquals", args = { var = "shield_broken", value = 1 } }
]}}
```

### Not (negates one guard)

```toml
{ trigger = "Tick", target = "Seek", guard = "Not", guard_args = { guard = {
    name = "StatusBoolEquals", args = { key = "player.0.boost.active", value = true } } } }
```

Compound guards support nesting:

```toml
{ trigger = "Tick", target = "Special", guard = "And", guard_args = { guards = [
    { name = "StateTimeExceeds", args = { ms = 1000 } },
    { name = "Or", args = { guards = [
        { name = "VarEquals", args = { var = "mode", value = 1 } },
        { name = "VarEquals", args = { var = "mode", value = 2 } }
    ]}}
]}}
```

An empty or omitted `guards` array evaluates to `true`, for `Or` as for `And`.

---

## Payload Guards

Inspect event payload fields directly in guards. Fields resolve by `toml`
struct tag first, then Go field name — identical to `capture_vars` and
`payload_vars`.

### PayloadIntCompare

```toml
{ trigger = "EventSpeciesKilled", target = "BossKilled", guard = "PayloadIntCompare", guard_args = { field = "species", op = "eq", value = 5 } }
```

### PayloadIntCompareVar

```toml
{ trigger = "EventInputPressed", target = "Hit", guard = "PayloadIntCompareVar", guard_args = { field = "tick", op = "lte", var = "close" } }
```

Compares a payload field with an FSM variable, as a press's `tick`, the
`engine.ticks` it was made at, with a window's marks, so a press counts however late
it arrives. A window takes `engine.ticks` into `open` with `StatusToVar` and sets
`close` to `open` plus its length; a press with `open < tick <= close` hits (two of
these in an `And`). A press made at the close can arrive up to
`engine.input_horizon` ticks later (none solo, 40 in a session), so the miss waits:
take that key into `deadline`, `IncrementVar` it with `source_var = "close"`, and
miss on `StatusIntCompare` of `engine.ticks` with `since = "deadline"`,
`op = "gte"`, `value = 0`.

### PayloadBoolEquals

```toml
{ trigger = "EventCursorDefeatState", target = "Down", guard = "PayloadBoolEquals", guard_args = { field = "defeated", value = true } }
```

`value` defaults to `true`.

### PayloadStringEquals

```toml
{ trigger = "EventInputPressed", target = "Pressed", guard = "PayloadStringEquals", guard_args = { field = "key", value = "1" } }
```

A state that turns keys into buttons captures them for its lifetime and reads each
press by its key, typed or clicked on the key's button ([Input and modes](input-and-modes.md) §1):

```toml
[states.Buttons]
on_enter = [
  { action = "EmitEvent", event = "EventKeyCaptureRequest", payload = { keys = "12345" }, reconcile = true },
  { action = "EmitEvent", event = "EventButtonSpawnRequest", payload = { key = "1", color = { r = 0, g = 200, b = 0 }, sound = "tone_a4" } },
]
on_exit = [{ action = "EmitEvent", event = "EventKeyCaptureRequest", reconcile = true }]
transitions = [
  { trigger = "EventInputPressed", internal = true, guard = "PayloadStringEquals",
    guard_args = { field = "key", value = "1" }, actions = [{ action = "IncrementVar", payload = { name = "ones" } }] },
]
```

A press and its release carry `slot`, `key`, `source` and `tick`, a release also
`lost`. Each press is followed by one `EventInputReleased`: the key's own release
where the slot's keyboard reports one, as a bot's does, at once with `lost = true`
where it taps. `releases = true` on the capture asks a console keyboard for
releases too, and `EventInputReport { slot, releases }` says which a slot does
while keys are captured.

A button lights and sounds on every press of its key by itself, held lit until the
release, so a machine judges a press and never plays its tone.
`EventButtonLightRequest { button, duration_ms }` lights the button at that place
in the row, counted from 1, with its sound; a spawn on a key that already has a
button is refused. A button shows its key in the splash font once it is about ten
cells square, so a scenario sets a `[map]` floor that leaves room for its row.
`wad/scenario/simon` is the worked example.

### PayloadExists

```toml
{ trigger = "EventGoldCompleted", target = "HasData", guard = "PayloadExists" }
```

`PayloadIntCompare` reads integer and float fields, truncating a float. Payload
guards return `false` if:

- No payload present (Tick transitions, nil payload)
- Field does not exist
- Field type mismatch

Combine with compound guards:

```toml
{ trigger = "EventMetaSystemCommandRequest", target = "DrainOn", guard = "And", guard_args = { guards = [
    { name = "PayloadStringEquals", args = { field = "system_name", value = "drain" } },
    { name = "PayloadBoolEquals", args = { field = "enabled", value = true } }
]}}
```

---

## Variables

Variables are **machine-global**: shared across all regions and states.
They persist across transitions and region spawn/terminate; they are cleared on
FSM init/reset and replaced by an installed capture. Regions that respawn must
explicitly re-zero their counters in the setup state.

---

## Timing Caveat

`StateTimeExceeds` reads `TimeInState`, which is **region-scoped** and resets
on every transition in the region. On a transition declared on an ancestor
state, it measures time since the region's last transition — not time within
the ancestor's subtree. Rapid child-state cycling (e.g. spawn-retry loops)
starves ancestor-level timeouts.

---

## Config Fields

Available fields for `ConfigIntCompare`:

| Field             | Description                            |
|-------------------|----------------------------------------|
| `color_mode`      | Render mode (0=256-color, 1=TrueColor) |
| `map_width`       | Simulation bounds width                |
| `map_height`      | Simulation bounds height               |
| `viewport_width`  | Drawable width, map bounds if shared   |
| `viewport_height` | Drawable height, map bounds if shared  |
| `camera_x`        | Camera X offset                        |
| `camera_y`        | Camera Y offset                        |

Available fields for `ConfigBoolCompare`:

| Field            | Description          |
|------------------|----------------------|
| `crop_on_resize` | Resize behavior flag |

`color_mode`, `camera_x` and `camera_y` describe this terminal, so they differ
between instances; the first read of one in a shared session logs a D-14 warning.

---

## Game Time

`time.game_elapsed_ms` available via `StatusIntCompare` (zero again after each reset):

```toml
{ trigger = "Tick", target = "LateGame", guard = "StatusIntCompare", guard_args = { key = "time.game_elapsed_ms", op = "gte", value = 60000 } }
```

---

## Execution Order

1. **FSM Init** — enter each region with an `initial` state (in name order), then
   reconcile the systems with the running regions; a reset first runs every region's
   `on_exit` (leaf → root)
2. **Region Spawn** — enter the region, then reconcile the systems; a pause, resume
   or terminate reconciles too
3. **Tick** — process delayed actions, execute `on_update` (root → leaf), evaluate transitions (leaf → root, first match wins)
4. **Event** — each queued event, including one an action emitted, reaches the FSM
   before any system handler; each unpaused region takes at most one matching
   transition (leaf → root, first match wins)
5. **Transition** — `capture_vars`, `on_exit` (leaf → LCA), transition `actions`, `on_enter` (LCA → leaf)

---

## Notes

- One transition per region per tick or per event; a matching leaf transition
  shadows ancestors
- `Root` lifecycle actions are rejected during loading because `Root` is shared
  by every region; put one-shot setup in a dedicated region's initial state
- Event payloads are shared compiled templates: handlers must treat them as
  immutable; `payload_vars` produces an isolated copy
- Region-control actions (`TerminateRegion` on self) are supported in
  `on_enter`/`on_exit`/transition actions; unsupported in `on_update`. From
  `on_exit` the terminate takes effect once the exit phase that is running ends:
  the region's exit actions run once, and a transition under way skips its
  `actions` and `on_enter`
- A state whose parent chain never reaches `Root` is refused at load, as is a
  region whose initial state the document does not define
- A key the loader does not read is ignored, not refused, so a misspelled argument
  reads as absent; only `[map]`, macro definitions and invocations, and the top
  level of a region or macro file refuse one. `-check` catches unknown event,
  action, guard, state and config field names, undeclared regions in region
  actions, and unknown systems in `[systems]` and region lists
