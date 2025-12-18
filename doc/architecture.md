# Vi-Fighter Architecture

## Overview

Vi-fighter is a terminal-based typing game built on a custom ECS engine with direct ANSI I/O. The architecture emphasizes type safety, zero-allocation hot paths, and strict separation of concerns.

## Package Structure

```
vi-fighter/
├── main.go              # Entry point, bootstrap, main loop
├── core/                # Entity types, game modes, crash handling
├── engine/              # ECS world, stores, resources, scheduler
├── components/          # Pure data components
├── systems/             # Game logic systems
├── events/              # Event types, queue, router
├── input/               # Terminal event parser (no game knowledge)
├── modes/               # Context-aware intent router
├── render/              # Render buffer, orchestrator, renderers
├── terminal/            # Direct ANSI terminal I/O
├── content/             # Content file management
├── manifest/            # System/renderer registration, FSM config
├── constants/           # Game configuration values
├── audio/               # Sound synthesis engine
└── assets/              # Embedded assets (splash font)
```

## Entity-Component-System

### Core Principles

- **Entities**: uint64 identifiers only (`core.Entity`)
- **Components**: Pure data structs, no logic
- **Systems**: All logic, operate on component sets
- **World**: Single source of truth for game state

### Generic Stores

Compile-time typed storage with zero reflection in hot paths.

| Store Type      | Description                                                   |
|-----------------|---------------------------------------------------------------|
| `Store[T]`      | Generic component storage with `Add`, `Get`, `Remove`, `All`  |
| `PositionStore` | Specialized store with `SpatialGrid` for O(1) spatial queries |
| `ResourceStore` | Type-safe global data (config, time, input state)             |

**Spatial Grid**: Fixed-capacity cells (15 entities max), 128-byte aligned, O(1) queries via `HasAny`, `GetAllAt`, `GetAllAtInto`.

### Component Stores

```go
type World struct {
    Resources      *ResourceStore
    Positions      *PositionStore           // Spatial-indexed
    Characters     *Store[CharacterComponent]
    Sequences      *Store[SequenceComponent]
    GoldSequences  *Store[GoldSequenceComponent]
    Decays         *Store[DecayComponent]
    Cleaners       *Store[CleanerComponent]
    Materializers  *Store[MaterializeComponent]
    Flashes        *Store[FlashComponent]
    Nuggets        *Store[NuggetComponent]
    Drains         *Store[DrainComponent]
    Cursors        *Store[CursorComponent]
    Protections    *Store[ProtectionComponent]
    Pings          *Store[PingComponent]
    Energies       *Store[EnergyComponent]
    Shields        *Store[ShieldComponent]
    Heats          *Store[HeatComponent]
    Boosts         *Store[BoostComponent]
    Splashes       *Store[SplashComponent]
    Timers         *Store[TimerComponent]
    MarkedForDeaths *Store[DeathComponent]
}
```

### Resources

| Resource            | Purpose                                               |
|---------------------|-------------------------------------------------------|
| `TimeResource`      | GameTime (pausable), RealTime, DeltaTime, FrameNumber |
| `ConfigResource`    | Screen/game dimensions, offsets                       |
| `InputResource`     | Current mode, buffer text, pause state                |
| `RenderConfig`      | Color mode, post-processing settings                  |
| `ZIndexResolver`    | Entity priority resolution                            |
| `GameStateResource` | Phase, spawn, boost timing                            |

## Event System

MPSC lock-free queue for inter-system communication.

```
Producer → EventQueue → ClockScheduler → EventRouter → Handler → Systems
(modes)    (lock-free)   (tick loop)     (dispatch)   (consume)  (engine)
```

### Event Flow

- **Queue**: Ring buffer (256), atomic CAS, `sync.Pool` for payloads
- **Router**: Handlers registered by event type, FIFO dispatch
- **Handler Interface**: `EventTypes() []EventType`, `HandleEvent(GameEvent)`

### Key Events

| Event                               | Producer            | Consumer         |
|-------------------------------------|---------------------|------------------|
| `EventCharacterTyped`               | modes.Router        | EnergySystem     |
| `EventDeleteRequest`                | modes.Router        | EnergySystem     |
| `EventHeatAdd/Set`                  | Various             | HeatSystem       |
| `EventEnergyAdd/Set`                | Various             | EnergySystem     |
| `EventShieldActivate/Deactivate`    | EnergySystem        | ShieldSystem     |
| `EventShieldDrain`                  | DrainSystem         | ShieldSystem     |
| `EventCleanerRequest`               | EnergySystem        | CleanerSystem    |
| `EventDirectionalCleanerRequest`    | HeatSystem, modes   | CleanerSystem    |
| `EventGoldSpawned/Complete/Timeout` | GoldSystem          | SplashSystem     |
| `EventSplashRequest`                | EnergySystem, modes | SplashSystem     |
| `EventTimerStart`                   | SplashSystem        | TimeKeeperSystem |
| `EventRequestDeath`                 | Various             | DeathSystem      |
| `EventPingGridRequest`              | modes.Router        | PingSystem       |
| `EventNuggetJumpRequest`            | modes.Router        | NuggetSystem     |

## Input Architecture

Two-package split for maximum decoupling.

### Input Package (Pure Parser)

Converts terminal events to semantic `Intent` structs with no game knowledge.

**State Machine** (7 states):
- `StateIdle` → `StateCount` → `StateOperatorWait` → motion/operator
- Character target states for f/F/t/T
- Prefix states for g-commands (gg, go)

**Intent Types**: Motion, OperatorMotion, TextChar, ModeSwitch, Special, NuggetJump, FireCleaner, Escape

### Modes Package (Context Router)

Validates intents against game state, routes to systems via events.

**Flow**:
```
Terminal.Event → input.Machine.Process() → Intent
Intent → modes.Router.Handle() → events/operations
```

**Motion Functions**: Pure calculations returning `MotionResult{StartX, StartY, EndX, EndY, Type, Style, Valid}`

**Operators**: `OpMove` updates cursor, `OpDelete` emits `EventDeleteRequest`

## Concurrency Model

### Threading

- **Main Loop**: Single-threaded ECS updates (16ms frame tick)
- **Input Goroutine**: Terminal polling → channel → main loop
- **Clock Scheduler**: Dedicated goroutine (50ms game tick)
- **Systems**: Synchronous execution in priority order

### Synchronization

- **World Lock**: `sync.RWMutex` for component access
- **GameState Atomics**: Boost, cursor error, grayout, frame counter
- **PausableClock**: Game time stops in COMMAND mode

### Clock Scheduler

- 50ms tick interval
- Frame synchronization via channels (`frameReady`, `updateDone`)
- Event dispatch → FSM update → Systems update
- Pause-aware with drift correction

## Game Systems

### Priority Order

| Priority | System           | Description                            |
|----------|------------------|----------------------------------------|
| 5        | BoostSystem      | Boost timer expiration                 |
| 6        | ShieldSystem     | Activation/deactivation, passive drain |
| 8        | HeatSystem       | Heat events, manual cleaner trigger    |
| 10       | EnergySystem     | Input processing, energy management    |
| 15       | SpawnSystem      | Content generation                     |
| 18       | NuggetSystem     | Nugget spawning/collection             |
| 20       | GoldSystem       | Gold sequence lifecycle                |
| 22       | CleanerSystem    | Cleaner physics/collision              |
| 25       | DrainSystem      | Hostile entity movement                |
| 30       | DecaySystem      | Sequence degradation                   |
| 35       | FlashSystem      | Destruction effects                    |
| 300      | PingSystem       | Grid timer, crosshair state            |
| 800      | SplashSystem     | Visual feedback, gold timer            |
| 890      | TimeKeeperSystem | Lifecycle timer management             |
| 900      | CullSystem       | Entity destruction                     |

### Communication Patterns

**Event-Driven**: EnergySystem, ShieldSystem, CleanerSystem, GoldSystem, NuggetSystem, SplashSystem, TimeKeeperSystem, PingSystem

**Update-Based**: BoostSystem, SpawnSystem, DrainSystem, DecaySystem

## Rendering Architecture

### Pipeline

```
RenderOrchestrator.RenderFrame()
    → Clear buffer
    → Content renderers (priority order)
    → Post-processors (Grayout, Dim)
    → FlushToTerminal (zero-copy)
```

### Buffer

- `RenderBuffer`: Dense grid compositor with blend modes
- Write masks for selective post-processing
- Dirty tracking for efficient updates

### Priorities

| Priority | Layer                             |
|----------|-----------------------------------|
| 0        | Background                        |
| 100      | Grid (ping highlights)            |
| 150      | Splash                            |
| 200      | Entities (characters)             |
| 300      | Effects (shield, decay, cleaners) |
| 350      | Drain                             |
| 390-395  | Post-processing                   |
| 400      | UI (heat meter, status, cursor)   |
| 500      | Overlay                           |

### Masks

| Mask         | Content                    |
|--------------|----------------------------|
| `MaskGrid`   | Background, ping overlay   |
| `MaskEntity` | Characters, nuggets        |
| `MaskShield` | Cursor shield              |
| `MaskEffect` | Decay, cleaners, flashes   |
| `MaskUI`     | Heat meter, status, cursor |

## FSM Integration

Hierarchical FSM for game phase management.

```
TrySpawnGold → GoldActive → DecayWait → DecayAnimation → TrySpawnGold
                    ↓
              GoldRetryWait
```

**Events drive transitions**: `EventGoldSpawned`, `EventGoldSpawnFailed`, `EventGoldComplete`, `EventGoldTimeout`, `EventDecayComplete`

**Actions**: `EmitEvent` pushes events to queue on state entry

**Guards**: `StateTimeExceeds` for timed transitions

## High-Precision Entities

Decay, Cleaner, and Materialize entities use dual-state model.

- **Primary**: Integer grid position in `PositionStore` for spatial queries
- **Overlay**: Float precision in domain component for physics

**Sync Protocol**: Update float → sync grid position on cell change

## Timing Architecture

**Duration-Based** (countdown each frame):
- `FlashComponent.Remaining`
- `CursorComponent.ErrorFlashRemaining`
- `SplashComponent.Remaining`
- `TimerComponent.Remaining`
- `PingComponent.GridTimer`
- `BoostComponent.Remaining`

**Timestamp-Based** (interval actions):
- `DrainComponent.LastMoveTime`, `LastDrainTime`
- `ShieldComponent.LastDrainTime`
- `NuggetComponent.SpawnTime`
- `GoldSequenceComponent.StartTimeNano`

**TimeKeeperSystem**: Centralized entity lifecycle via `TimerComponent`, tags with `MarkedForDeathComponent` when expired.

## Protection System

`ProtectionComponent.Mask` flags:

| Flag                | Effect                             |
|---------------------|------------------------------------|
| `ProtectFromDecay`  | Immune to decay                    |
| `ProtectFromDrain`  | Immune to drain                    |
| `ProtectFromCull`   | Survives out-of-bounds             |
| `ProtectFromDelete` | Immune to delete operators         |
| `ProtectAll`        | Completely indestructible (cursor) |

## Z-Index System

Priority-based entity selection for overlapping positions.

| Z-Index | Entity Type      |
|---------|------------------|
| 0       | Background       |
| 100     | Spawn Characters |
| 200     | Nugget           |
| 300     | Decay            |
| 400     | Drain            |
| 500     | Shield           |
| 1000    | Cursor           |

## Sequence Types

| Type  | Source           | Effect                 |
|-------|------------------|------------------------|
| Blue  | SpawnSystem      | Positive scoring       |
| Green | SpawnSystem      | Positive scoring       |
| Red   | DecaySystem only | Heat reset penalty     |
| Gold  | GoldSystem       | 10-char bonus sequence |

**Decay Chain**: Blue(Dark) → Green(Bright) → Green(Dark) → Red(Bright) → Red(Dark) → Destroyed

## Commands

| Command         | Effect                   |
|-----------------|--------------------------|
| `:q`            | Exit game                |
| `:new`          | Reset game state         |
| `:heat <n>`     | Set heat value (debug)   |
| `:energy <n>`   | Set energy value (debug) |
| `:boost`        | Activate boost (debug)   |
| `:spawn on/off` | Toggle spawning (debug)  |
| `:debug`        | Show debug overlay       |
| `:help`         | Show help overlay        |

## Vi Motions

**Basic**: h, j, k, l, Space
**Line**: 0, ^, $
**Word**: w, b, e, W, B, E
**Screen**: gg, G, go, H, M, L
**Paragraph**: {, }, %
**Find/Till**: f, F, t, T, ;, ,
**Search**: /, n, N
**Delete**: x, dd, d{motion}, D

## Special Keys (Normal Mode)

| Key        | Action                              |
|------------|-------------------------------------|
| Tab        | Jump to nugget (10 energy)          |
| ESC        | Activate ping grid (1 sec)          |
| Enter      | Manual cleaner (≥10 heat, costs 10) |
| Arrow keys | Same as h/j/k/l                     |