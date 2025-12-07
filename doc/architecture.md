# Vi-Fighter Architecture

## Core Paradigms

### Entity-Component-System (ECS)
**Strict Rules:**
- Entities are ONLY identifiers (uint64)
- Components contain ONLY data, NO logic
- Systems contain ALL logic, operate on component sets
- World is the single source of truth for all game state

### Generic ECS Architecture

Vi-fighter uses **compile-time generics-based ECS** (Go 1.18+) that eliminates reflection from the hot path.

**Core Components:**

1. **Generic Stores** (`Store[T]`):
   - Typed component storage with compile-time type checking
   - Operations: `Add(entity, component)`, `Get(entity)`, `Remove(entity)`, `All()`
   - Thread-safe via internal `sync.RWMutex`
   - Zero allocations for component access

2. **PositionStore** (Specialized):
   - Extends `Store[PositionComponent]` with spatial indexing via `SpatialGrid`
   - Dense 2D grid with fixed-capacity cells (max 15 entities per cell)
   - Operations: `GetAllAt(x, y)`, `GetAllAtInto(x, y, buf)`, `HasAny(x, y)`, `Move(...)`
   - Zero-allocation queries via `GetAllAtInto()` with caller-provided buffers
   - Multi-entity support enables cursor/character overlap handling

3. **Query System** (`QueryBuilder`):
   - Type-safe component intersection queries
   - Uses sparse set intersection starting with smallest store
   - Example: `world.Query().With(world.Positions).With(world.Characters).Execute()`

4. **Entity Creation Pattern**:
   ```go
   // Basic Pattern (no collision checking)
   entity := world.CreateEntity()
   world.Positions.Add(entity, components.PositionComponent{X: x, Y: y})
   world.Characters.Add(entity, components.CharacterComponent{Rune: r, Style: s})
   ```

   **Batch Pattern** (for collision-sensitive spawning):
   ```go
   entities := make([]engine.Entity, 0, count)
   positions := make([]components.PositionComponent, 0, count)

   for i := 0; i < count; i++ {
       entity := world.CreateEntity()
       entities = append(entities, entity)
       positions = append(positions, components.PositionComponent{X: x+i, Y: y})
   }

   batch := world.Positions.BeginBatch()
   for i, entity := range entities {
       batch.Add(entity, positions[i])
   }

   if err := batch.Commit(); err != nil {
       for _, entity := range entities {
           world.DestroyEntity(entity)
       }
       return false
   }

   for i, entity := range entities {
       world.Characters.Add(entity, characters[i])
   }
   ```

**World Structure:**
```go
type World struct {
    Resources      *ResourceStore  // Thread-safe global data

    Positions      *PositionStore
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
    Shields        *Store[ShieldComponent]
    Splashes       *Store[SplashComponent]

    allStores []AnyStore
}
```

### Resource System

The Resource System provides generic, thread-safe access to global shared data without coupling systems to `GameContext`.

**Core Resources:**

1. **`TimeResource`** - Time data:
   - `GameTime` (time.Time): Current game time (pausable, stops in COMMAND mode)
   - `RealTime` (time.Time): Wall clock time (always advances)
   - `DeltaTime` (time.Duration): Time since last update
   - `FrameNumber` (int64): Current frame count

2. **`ConfigResource`** - Immutable configuration:
   - `GameWidth`, `GameHeight`: Game area dimensions
   - `ScreenWidth`, `ScreenHeight`: Terminal dimensions
   - `GameX`, `GameY`: Game area offset

3. **`InputResource`** - Current input state:
   - `GameMode` (int): Current mode (Normal, Insert, Search, Command)
   - `CommandText`, `SearchText` (string): Buffer text
   - `IsPaused` (bool): Pause state

**Access Pattern:**
```go
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

    width := config.GameWidth
    now := timeRes.GameTime

    // Access game state through GameContext
    s.ctx.State.AddHeat(1)
    snapshot := s.ctx.State.ReadSpawnState()
}
```

**GameContext Role:**
- OS/Window management, Event routing, State orchestration
- `GameState` access via `ctx.State`
- `EventQueue` integration via `ctx.PushEvent()` (wraps `events.EventQueue` for producer access)
- `AudioEngine` access via `ctx.AudioEngine.PlaySound()`
- `CursorEntity` reference for systems to query/update cursor position via ECS
- Mode state tracking (NORMAL, INSERT, SEARCH, COMMAND, OVERLAY)

### Event-Driven Communication

The event system provides high-performance, type-safe inter-system communication through a dedicated `events` package (`github.com/lixenwraith/vi-fighter/events`). This architecture strictly separates message definitions from execution logic, eliminating circular dependencies between game systems.

**Package Structure:**

| File | Description |
|------|-------------|
| `types.go` | `EventType` enums and `GameEvent` envelope struct |
| `payloads.go` | Structured data carriers (e.g., `CharacterTypedPayload`, `DeleteRequestPayload`) |
| `queue.go` | Lock-free ring buffer (256 capacity, atomic CAS operations) |
| `router.go` | Generic dispatch logic (`Router[T]`) connecting events to handlers |

**Architecture Flow (MPSC Pattern):**
```
Producer → EventQueue → ClockScheduler → EventRouter → Handler[T] → Systems
(modes pkg)  (lock-free)   (tick loop)    (dispatch)    (consume)   (engine pkg)
```

**Core Principles:**
- **Package Separation**: `events` package contains only message definitions, no game logic
- **Generic Routing**: `Router[T any]` and `Handler[T any]` prevent import cycles between `events` and `engine`
- **Type Safety**: Compile-time type checking via generics (Go 1.24+)
- **Lock-Free Queue**: Atomic CAS operations, safe for concurrent writes from Input goroutine and Game Loop
- **Zero Allocation**: Steady-state operation with no GC pressure (uses `sync.Pool` for high-frequency payloads)
- **Centralized Dispatch**: Events dispatched before `World.Update()` in clock tick

**Event Types:**

| Event | Producer | Consumer | Payload |
|-------|----------|----------|---------|
| `EventCharacterTyped` | InputHandler (modes) | EnergySystem | `*CharacterTypedPayload{Char, X, Y}` |
| `EventEnergyTransaction` | InputHandler (modes) | EnergySystem | `*EnergyTransactionPayload{Amount, Source}` |
| `EventNuggetJumpRequest` | InputHandler (modes) | NuggetSystem | `nil` |
| `EventDeleteRequest` | InputHandler (modes) | EnergySystem | `*DeleteRequestPayload{StartX, StartY, EndX, EndY, RangeType}` |
| `EventShieldActivate` | EnergySystem | ShieldSystem | `nil` |
| `EventShieldDeactivate` | EnergySystem | ShieldSystem | `nil` |
| `EventShieldDrain` | DrainSystem | ShieldSystem | `*ShieldDrainPayload{Amount}` |
| `EventCleanerRequest` | EnergySystem | CleanerSystem | `nil` |
| `EventDirectionalCleanerRequest` | InputHandler (modes), EnergySystem | CleanerSystem | `*DirectionalCleanerPayload{OriginX, OriginY}` |
| `EventCleanerFinished` | CleanerSystem | (observers) | `nil` |
| `EventSplashRequest` | EnergySystem, InputHandler (modes) | SplashSystem | `*SplashRequestPayload{Text, Color, OriginX, OriginY}` |
| `EventGoldSpawned` | GoldSystem | SplashSystem | `*GoldSpawnedPayload{SequenceID, OriginX, OriginY, Length, Duration}` |
| `EventGoldComplete` | GoldSystem | SplashSystem | `*GoldCompletionPayload{SequenceID}` |
| `EventGoldTimeout` | GoldSystem | SplashSystem | `*GoldCompletionPayload{SequenceID}` |
| `EventGoldDestroyed` | DrainSystem | SplashSystem | `*GoldCompletionPayload{SequenceID}` |

**Producer Pattern (modes package):**
```go
import "github.com/lixenwraith/vi-fighter/events"

payload := &events.CharacterTypedPayload{Char: r, X: x, Y: y}
h.ctx.PushEvent(events.EventCharacterTyped, payload, h.ctx.PausableClock.Now())
```

**Consumer Pattern (systems in engine package):**
```go
import (
    "github.com/lixenwraith/vi-fighter/engine"
    "github.com/lixenwraith/vi-fighter/events"
)

// Generic Handler interface implemented by systems
func (s *EnergySystem) EventTypes() []events.EventType {
    return []events.EventType{events.EventCharacterTyped, events.EventEnergyTransaction}
}

func (s *EnergySystem) HandleEvent(world *engine.World, event events.GameEvent) {
    switch event.Type {
    case events.EventCharacterTyped:
        if payload, ok := event.Payload.(*events.CharacterTypedPayload); ok {
            s.handleCharacterTyping(world, payload.X, payload.Y, payload.Char)
        }
    }
}
```

**Generic Routing Architecture:**

The `Router[T]` uses generics to avoid circular dependencies:

```go
// events package (no dependency on engine)
type Handler[T any] interface {
    HandleEvent(ctx T, event GameEvent)
    EventTypes() []EventType
}

type Router[T any] struct {
    handlers map[EventType][]Handler[T]
}

// engine package instantiates with concrete type
router := events.NewRouter[*engine.World]()
router.Register(energySystem)  // energySystem implements Handler[*engine.World]
router.DispatchAll(world, eventQueue)
```

**Data Flow:**

1. **Production**: `modes` package detects keystroke → constructs payload → calls `ctx.PushEvent()` → event written to lock-free `EventQueue`
2. **Synchronization**: `ClockScheduler` wakes (50ms tick) → acquires `World` lock for thread safety
3. **Consumption & Dispatch**: `Router` drains queue in FIFO order → looks up registered `Handler[*engine.World]` → calls `HandleEvent(world, event)`

**Key Benefits:**

1. **Decoupling**: Input handling (`modes`) depends only on lightweight event definitions, not heavy engine logic
2. **Testability**: Routing logic and queues testable in isolation with mock contexts
3. **Concurrency Safety**: Generic design keeps Event System "dumb" about data, while `engine` enforces thread safety via World lock during dispatch
4. **No Circular Dependencies**: `events` package is standalone, `modes` and `engine` both depend on it but not on each other

### State Ownership Model

**GameState** (`engine/game_state.go`) centralizes game state with clear ownership boundaries:

#### Real-Time State (Lock-Free Atomics)
- **Heat**, **Energy** (`atomic.Int64`): Current values
- **Cursor Position**: Managed in ECS, cached in GameContext
- **Boost State** (`atomic.Bool`, `atomic.Int64`): Enabled, EndTime, Color
- **Shield State** (`atomic.Bool`): ShieldActive
- **Shield Color Tracking** (`atomic.Int32`): LastTypedSeqType, LastTypedSeqLevel
- **Drain State** (`atomic.Bool`, `atomic.Uint64`, `atomic.Int32`): Active, EntityID, X, Y
- **Sequence ID** (`atomic.Int64`): Thread-safe ID generation
- **Runtime Metrics** (`atomic.Uint64`): GameTicks, CurrentAPM, PendingActions

#### Clock-Tick State (Mutex Protected)
- **Spawn Timing** (`sync.RWMutex`): LastTime, NextTime, RateMultiplier
- **Screen Density**: EntityCount, ScreenDensity, SpawnEnabled
- **Game Phase State**: CurrentPhase, PhaseStartTime
- **Gold Sequence State**: GoldActive, GoldSequenceID, GoldStartTime, GoldTimeoutTime
- **Decay Timer State**: DecayTimerActive, DecayNextTime
- **Decay Animation State**: DecayAnimating, DecayStartTime
- **APM History** (`sync.RWMutex`): apmHistory[60], apmHistoryIndex

#### State Initialization (Unified Pattern)

The game uses `GameState.initState()` for both app start and :new command:

```go
func (gs *GameState) initState(now time.Time) {
    gs.Energy.Store(0)
    gs.Heat.Store(0)
    gs.BoostEnabled.Store(false)
    // ... all atomic and mutex-protected state
}

func NewGameState(maxEntities int, now time.Time) *GameState {
    gs := &GameState{MaxEntities: maxEntities}
    gs.initState(now)
    return gs
}

func (gs *GameState) Reset(now time.Time) {
    gs.mu.Lock()
    defer gs.mu.Unlock()
    gs.initState(now)
}
```

#### Snapshot Pattern

**Snapshot pattern** provides safe multi-field state reads:

```go
// Spawn State
type SpawnStateSnapshot struct {
    LastTime, NextTime time.Time
    RateMultiplier float64
    Enabled bool
    EntityCount, MaxEntities int
    ScreenDensity float64
}
snapshot := ctx.State.ReadSpawnState()

// Boost State (atomic fields)
type BoostSnapshot struct {
    Enabled bool
    EndTime time.Time
    Color int32
    Remaining time.Duration
}
snapshot := ctx.State.ReadBoostState()

// Gold State
snapshot := ctx.State.ReadGoldState()

// Decay State
snapshot := ctx.State.ReadDecayState()

// Phase State
snapshot := ctx.State.ReadPhaseState()
```

### Clock Scheduler and Time Management

**Dual-clock system** with frame/game synchronization:
- **Frame Clock** (16ms, ~60 FPS): Rendering, UI updates, input handling
- **Game Clock** (50ms): Game logic via ClockScheduler

#### PausableClock - Game Time vs Real Time

**Dual Time System**:
- **Game Time**: Pausable clock for all game logic (spawning, decay, gold timeouts)
  - Stops during COMMAND mode
  - Accessed via `ctx.TimeProvider.Now()`
- **Real Time**: Wall clock for UI elements (cursor blink)
  - Continues during pause
  - Accessed via `ctx.GetRealTime()`

**Pause Mechanism:**
```go
ctx.SetPaused(true)          // Sets IsPaused atomic flag
ctx.PausableClock.Pause()    // Stops game time
// gameTime = realTime - totalPausedTime
```

**Resume with Drift Protection:**
```go
func (cs *ClockScheduler) HandlePauseResume(pauseDuration time.Duration) {
    cs.nextTickDeadline = cs.nextTickDeadline.Add(pauseDuration)
}
```

#### GamePhase State Machine

```go
const (
    PhaseNormal         // Regular gameplay, content spawning
    PhaseGoldActive     // Gold sequence active
    PhaseGoldComplete   // Gold completed (transient)
    PhaseDecayWait      // Waiting for decay timer
    PhaseDecayAnimation // Decay animation running
)
```

**Phase transitions handled on 50ms clock tick:**
- `PhaseNormal`: Default state, content spawning active
- `PhaseGoldActive`: Check timeout → GoldSystem handles timeout
- `PhaseGoldComplete`: Start decay timer → transition to PhaseDecayWait
- `PhaseDecayWait`: Check decay ready → start animation
- `PhaseDecayAnimation`: DecaySystem manages → returns to PhaseNormal

**Cleaners run in parallel** via event system (non-blocking, independent of phases)

#### ClockScheduler

**Infrastructure:**
- 50ms tick interval in dedicated goroutine
- Adaptive sleep respecting pause state
- Frame synchronization via channels (`frameReady`, `updateDone`)
- Tick counter for metrics
- Pause resume callback for drift correction

**Runtime Metrics:**
- Increments GameTicks every tick
- Updates APM every 20 ticks (~1 second)
- APM = sum of 60-second rolling window

### Spatial Indexing with SpatialGrid

Dense 2D grid for O(1) spatial queries with multi-entity support.

**Architecture** (`engine/spatial_grid.go`):
- Fixed-size cells (128 bytes = 2 cache lines)
- Stores up to 15 entities per cell
- 1D contiguous array indexed as `cells[y*width + x]`
- Soft clipping when full (no allocation spikes)

**PositionStore Integration:**
- Wraps `SpatialGrid` with thread-safe operations
- Maintains bidirectional mapping
- Stores `*World` reference for z-index lookups

**Access Patterns:**
```go
entities := world.Positions.GetAllAt(x, y)

var buf [engine.MaxEntitiesPerCell]engine.Entity
count := world.Positions.GetAllAtInto(x, y, buf[:])
entitiesAtPos := buf[:count]

if world.Positions.HasAny(x, y) {
    // At least one entity present
}

entity := world.Positions.GetTopEntityFiltered(x, y, world, func(e engine.Entity) bool {
    return engine.IsInteractable(world, e)
})
```

### High-Precision Entity Architecture: Sync & Overlay Pattern

High-precision entities (Decay, Cleaner, Materialize) use a **dual-state model** for unified spatial queries while maintaining sub-pixel physics:

**Pattern Overview:**
- **Primary (Logic)**: `PositionStore` holds authoritative integer grid location for collision/queries
- **Overlay (Physics/Render)**: Domain components retain float precision (`PreciseX/Y`)
- Systems update float state, then sync integer grid position via `Positions.Add()`

**Spawn Protocol:**
```go
entity := world.CreateEntity()
world.Positions.Add(entity, components.PositionComponent{X: gridX, Y: gridY})  // Grid registration
world.Decays.Add(entity, components.DecayComponent{
    PreciseX: float64(gridX),  // Float overlay
    PreciseY: float64(gridY),
    // ...
})
```

**Grid Sync Protocol:**
Systems update float position, then sync grid if integer position changed:
```go
// Update physics (float)
decay.PreciseX += velocity * dt
decay.PreciseY += velocity * dt

// Sync grid position if cell changed
newGridX := int(decay.PreciseX)
newGridY := int(decay.PreciseY)
if newGridX != oldPos.X || newGridY != oldPos.Y {
    world.Positions.Add(entity, components.PositionComponent{X: newGridX, Y: newGridY})
}
```

**Self-Exclusion Requirement:**
Spatial queries return the querying entity. Collision loops must filter:
```go
entitiesAtPos := world.Positions.GetAllAt(x, y)
for _, candidate := range entitiesAtPos {
    if candidate == selfEntity {
        continue  // Self-exclusion
    }
    // Process collision...
}
```

**Affected Systems:**
- **DecaySystem** (`systems/decay.go`): Swept traversal with self-exclusion, grid sync on cell change
- **CleanerSystem** (`systems/cleaner.go`): Vector physics with trail tracking, grid sync on movement
- **DrainSystem** (`systems/drain.go`): Materializers use pattern for spawn animation

**Benefits:**
- Unified cleanup via single `PositionStore` iteration
- Spatial queries include all entities (no special cases)
- Sub-pixel physics without sacrificing collision accuracy
- Consistent entity lifecycle (all entities visible to global queries)

### Z-Index System

**Z-Index System** (`engine/z-index.go`) provides priority-based entity selection.

**Z-Index Constants** (Higher = On Top):
```go
const (
    ZIndexBackground = 0
    ZIndexSpawnChar  = 100
    ZIndexNugget     = 200
    ZIndexDecay      = 300
    ZIndexDrain      = 400
    ZIndexShield     = 500
    ZIndexCursor     = 1000
)
```

**Core Functions:**
- `GetZIndex(world, entity)`: Returns z-index based on components
- `SelectTopEntity(entities, world)`: Selects highest z-index
- `SelectTopEntityFiltered(entities, world, filter)`: Top entity passing filter
- `IsInteractable(world, entity)`: Returns true for typed entities

**Integration Points:**
- **PositionStore**: `GetTopEntityFiltered(x, y, world, filter)`
- **EnergySystem**: Find interactable entity at cursor
- **CursorRenderer**: Determine which character to display

## Component Hierarchy

```
Component (marker interface)
├── PositionComponent {X, Y int}
├── CharacterComponent {Rune rune, Color ColorClass, Style TextStyle, SeqType, SeqLevel}
├── SequenceComponent {ID, Index int, Type SequenceType, Level SequenceLevel}
├── GoldSequenceComponent {Active bool, SequenceID int, StartTimeNano int64, CharSequence []rune, CurrentIndex int}
├── DecayComponent {PreciseX, PreciseY float64, Speed float64, Char rune, LastChangeRow, LastIntX, LastIntY int, PrevPreciseX, PrevPreciseY float64}
├── CleanerComponent {PreciseX/Y float64, VelocityX/Y float64, TargetX/Y float64, TrailRing [Length]Point, TrailHead, TrailLen int, Char rune}
├── MaterializeComponent {PreciseX/Y float64, VelocityX/Y float64, TargetX, TargetY int, TrailRing [Length]Point, TrailHead, TrailLen int, Direction MaterializeDirection, Char rune, Arrived bool}
├── FlashComponent {X, Y int, Char rune, StartTime time.Time, Duration time.Duration}
├── NuggetComponent {ID int, SpawnTime time.Time}
├── DrainComponent {LastMoveTime, LastDrainTime time.Time, IsOnCursor bool, SpawnOrder int64}
├── CursorComponent {ErrorFlashEnd int64, HeatDisplay int}
├── ProtectionComponent {Mask ProtectionFlags, ExpiresAt int64}
├── ShieldComponent {Energy int64, RadiusX, RadiusY float64, OverrideColor ColorClass, MaxOpacity float64}
└── SplashComponent {Content [8]rune, Length int, Color SplashColor, AnchorX, AnchorY int, Mode SplashMode, StartNano int64, Duration int64, SequenceID int}
```

**Sequence Types:**
- **Green**: Positive scoring, spawned by SpawnSystem
- **Blue**: Positive scoring, spawned by SpawnSystem
- **Red**: Penalty, ONLY created through decay
- **Gold**: 10-character bonus sequence

## Rendering Architecture

The game uses **direct terminal rendering** via a custom `terminal` package and `RenderOrchestrator` pattern.

### Terminal Integration

Vi-fighter uses a custom **terminal package** for direct ANSI terminal control, replacing tcell. The terminal package is designed to be independently usable and provides true color rendering, double-buffered output with cell-level diffing, and raw stdin parsing.

**Integration Points:**

- **RenderBuffer → Terminal**: Zero-copy cell export via type alias (`render.Cell` = `terminal.Cell`)
- **Input Handling**: `InputHandler` consumes `terminal.Event` from `PollEvent()`
- **Lifecycle**: Main loop coordinates `Init()`, event polling, and `Fini()`
- **Resize Events**: Terminal dimension changes propagate via `EventResize`

**Terminal Cell Structure:**
```go
type Cell struct {
    Rune  rune    // Character to display
    Fg, Bg RGB    // 24-bit colors
    Attrs Attr    // Style attributes (bold, dim, etc.)
}
```

The render package uses `terminal.Cell` directly in `RenderBuffer`, enabling zero-copy transfer via `FlushToTerminal()`. This tight coupling optimizes the render pipeline while keeping the terminal package independently usable.

**For complete terminal package documentation**, see [terminal.md](./terminal.md).

### Render Package

**Core Types:**
- `RenderOrchestrator` (`render/orchestrator.go`): Coordinates render pipeline with priority ordering
- `RenderBuffer` (`render/buffer.go`): Dense grid compositor with blend modes and stencil masking
- `SystemRenderer` (`render/interface.go`): Interface for renderers
- `RenderPriority` (`render/priority.go`): Render order constants (lower first)
- `RenderContext` (`render/context.go`): Frame data passed by value
- `RGB` (`render/rgb.go`): Explicit 8-bit color with blend operations (Blend, Add, Scale, Grayscale, Lerp)
- `Cell`: Type alias to `terminal.Cell` (zero-copy coupling)
- `RenderMask` (`render/mask.go`): Bitmask constants for selective post-processing (MaskGrid, MaskEntity, MaskShield, MaskEffect, MaskUI)

**Color Management** (`render/colors.go`):
- 60+ named RGB color variables
- Centralized color definitions: sequences, entities, UI, effects, modals
- Resolution functions: `GetFgForSequence(seqType, level)`, `GetHeatMeterColor(progress)`
- Systems use semantic `ColorClass` enum, renderer resolves to RGB

**Blend Modes** (`render/blender.go`):
- `BlendReplace`: Opaque overwrite
- `BlendAlpha`: src*α + dst*(1-α)
- `BlendAdd`: clamp(dst+src, 255) for light accumulation
- `BlendMax`: per-channel maximum
- `BlendFgOnly`: Update text only, preserve background
- `BlendSoftLight`: Gentle overlay with lookup tables

**Render Flow:**
1. `RenderOrchestrator.RenderFrame()` locks World and clears buffer
2. Content renderers execute in priority order (skip if `VisibilityToggle.IsVisible()` returns false)
3. Each renderer sets write mask via `SetWriteMask()` and writes to buffer: `Set()`, `SetFgOnly()`, `SetWithBg()`
4. Post-processing renderers execute (DimRenderer, GrayoutRenderer) applying effects via `MutateDim()` and `MutateGrayscale()`
5. `RenderBuffer.FlushToTerminal()` passes `[]terminal.Cell` to terminal (zero-copy)

**Priority Layers:**
- **PriorityBackground (0)**: Base layer
- **PriorityGrid (100)**: Ping highlights
- **PrioritySplash (150)**: Large block-character visual feedback
- **PriorityEntities (200)**: Characters
- **PriorityEffects (300)**: Shields, decay, cleaners, flashes
- **PriorityDrain (350)**: Drain entity
- **PriorityPostProcessing (390-395)**: Post-processors (GrayoutRenderer, DimRenderer)
- **PriorityUI (400)**: Heat meter, line numbers, status bar, cursor
- **PriorityOverlay (500)**: Modal windows
- **PriorityDebug (1000)**: Debug overlays

**Individual Renderers** (`render/renderers/`):
- `PingGridRenderer`: Row/column highlights (writes MaskGrid)
- `SplashRenderer`: Large block-character feedback and gold timer (writes MaskEffect, linear opacity fade, bitmap font rendering)
- `CharactersRenderer`: All character entities (writes MaskEntity)
- `ShieldRenderer`: Protective field with gradient (writes MaskShield, queries GameState.ShieldActive from RenderContext, color derived from LastTypedSeqType/Level)
- `EffectsRenderer`: Decay, cleaners, flashes, materializers (writes MaskEffect)
- `DrainRenderer`: Drain entities (writes MaskEffect)
- `HeatMeterRenderer`, `LineNumbersRenderer`, `ColumnIndicatorsRenderer`: UI elements (write MaskUI)
- `StatusBarRenderer`: Mode, commands, metrics, FPS (writes MaskUI)
- `CursorRenderer`: Cursor rendering (writes MaskUI)
- `OverlayRenderer`: Modal windows (writes MaskUI)
- **Post-Processors**:
  - `GrayoutRenderer`: Desaturation effect for entities when cleaners trigger with no targets (priority 390)
  - `DimRenderer`: Brightness reduction for non-UI content during pause (priority 395)

**RenderBuffer Methods:**
- `SetWriteMask(mask uint8)`: Set mask for subsequent draw operations
- `Set(x, y, rune, fg, bg, mode, alpha, attrs)`: Full compositing (marks touched, writes currentMask)
- `SetFgOnly(x, y, rune, fg, attrs)`: Text overlay (does NOT mark touched, writes currentMask)
- `SetBgOnly(x, y, bg)`: Background update (marks touched, writes currentMask)
- `SetWithBg(x, y, rune, fg, bg)`: Opaque replacement (marks touched, writes currentMask)
- `MutateDim(factor, targetMask)`: Reduce brightness for cells matching mask
- `MutateGrayscale(intensity, targetMask)`: Desaturate cells matching mask
- `Clear()`: Exponential copy algorithm
- `FlushToTerminal(term)`: Zero-copy pass to terminal

**Dirty Tracking:**
- Each cell has `touched` flag
- Cells start with black (0,0,0) background after Clear()
- Background-modifying blends mark cells as touched
- BlendFgOnly preserves background state
- At Flush(), untouched cells receive Tokyo Night background (26,27,38)

**Performance Optimizations:**
- Zero-alloc buffer after init
- Exponential copy for Clear()
- Pre-built gradients in renderers
- Ring buffers in trail systems
- Zero-copy export to terminal (uses `terminal.Cell` directly)

**Adding New Visual Elements:**
1. Create struct implementing `SystemRenderer` in `render/renderers/`
2. Choose appropriate `RenderPriority` constant
3. Implement `Render(ctx RenderContext, world *World, buf *RenderBuffer)`
4. Use semantic color resolution (ColorClass → RGB via render/colors.go)
5. Register with orchestrator: `orchestrator.Register(renderer, priority)`
6. Optionally implement `VisibilityToggle` for conditional rendering

### Pause State Visual Feedback

When paused (COMMAND mode):
- Game time stops (all timers freeze)
- UI time continues (cursor blinks)
- Non-UI content dimmed to 50% brightness via `DimRenderer` post-processor (applies to MaskAll ^ MaskUI)
- Frame updates continue

### Stencil-Based Post-Processing

The render pipeline uses a **stencil mask system** for selective visual effects:

**Architecture:**
```
Content Renderers → SetWriteMask() → RenderBuffer (cells[] + masks[])
                                           ↓
                              Post-Processors (GrayoutRenderer, DimRenderer)
                                           ↓
                              FlushToTerminal
```

**Mask Categories** (`render/mask.go`):
- `MaskGrid` (0x01): Background grid, ping overlay
- `MaskEntity` (0x02): Characters, nuggets, spawned content
- `MaskShield` (0x04): Cursor shield effect
- `MaskEffect` (0x08): Decay, cleaners, flashes, materializers, drains
- `MaskUI` (0x10): Heat meter, status bar, line numbers, cursor, overlay
- `MaskAll` (0xFF): All content

**Post-Processing Effects:**
- **DimRenderer** (priority 395): Applies brightness reduction during pause to all non-UI content (`MaskAll ^ MaskUI`)
- **GrayoutRenderer** (priority 390): Applies desaturation effect to entities when cleaners trigger with no targets (phantom cleaners)

## System Coordination and Event Flow

### Complete Game Cycle

```
PhaseNormal → PhaseGoldActive → PhaseGoldComplete → PhaseDecayWait → PhaseDecayAnimation → PhaseNormal

Parallel (Event-Driven):
  EventCleanerRequest → Cleaner Animation → EventCleanerFinished
```

**Key Transitions:**
- Game starts in PhaseNormal (instant spawn)
- Gold spawns after decay → PhaseGoldActive
- Gold completion/timeout → PhaseGoldComplete → PhaseDecayWait
- Decay timer expires → PhaseDecayAnimation
- Animation completes → PhaseNormal
- :new command resets to PhaseNormal
- Cleaners run in parallel, don't affect phases

### Event Sequencing

**Gold Phase:**
- Duration: 10 seconds
- Completion: typed correctly or times out
- Next: PhaseGoldComplete → decay timer starts

**Decay Timer Phase:**
- Duration: 60-10 seconds based on heat percentage
- Formula: `60s - (50s * (CurrentHeat / MaxHeat))`
- Next: Triggers animation

**Decay Animation Phase:**
- Duration: Based on falling speed (4.8-1.6 seconds)
- Effects: Spawns falling entities, decays characters
- Character decay: Bright → Normal → Dark, Blue→Green→Red
- Next: Returns to PhaseNormal

### Energy System Integration

**Gold Typing:**
1. Verify character matches
2. If incorrect: Flash error, DON'T reset heat
3. If correct: Destroy character, move cursor
4. If last character:
   - Check if heat at maximum
   - If yes: Trigger cleaners immediately
   - Fill heat to maximum
   - Mark gold complete

**Key Behavior:**
- Gold typing NEVER resets heat
- Cleaners trigger BEFORE heat fill
- Gold timeout uses pausable clock

### Concurrency Guarantees

**Mutex Protection:**
- DecaySystem: `sync.RWMutex` protects animation state
- GoldSystem: `sync.RWMutex` protects sequence state

**Atomic Operations:**
- CleanerSystem: `pendingSpawn` flag (lock-free activation)
- All atomic state in GameState

### State Transition Rules

**Phase Transitions:**
- **Game Start** → **PhaseNormal**: Instant start
- **PhaseNormal** → **PhaseGoldActive**: Gold spawns
- **PhaseGoldActive** → **PhaseGoldComplete**: Gold ends
- **PhaseGoldComplete** → **PhaseDecayWait**: Timer starts
- **PhaseDecayWait** → **PhaseDecayAnimation**: Timer expires
- **PhaseDecayAnimation** → **PhaseNormal**: Animation complete

**Cleaner Event Flow:**
- `EventCleanerRequest` → spawn entities (or phantom if no Red)
- Animation runs in parallel
- `EventCleanerFinished` on completion
- No phase transitions blocked

## Input State Machine

### Game Mode State Machine

```
NORMAL ─[i]→ INSERT
NORMAL ─[/]→ SEARCH
INSERT / SEARCH ─[ESC]→ NORMAL
NORMAL ─[:]→ COMMAND (paused) ─[ESC/ENTER]→ NORMAL
COMMAND ─[:debug/:help]→ OVERLAY (modal) ─[ESC/ENTER]→ NORMAL
```

**ESC Key Priority:**
1. Search Mode: Clear text → NORMAL
2. Command Mode: Clear text, unpause → NORMAL
3. Insert Mode: → NORMAL
4. Overlay Mode: Close overlay, unpause → NORMAL
5. Normal Mode: Activate ping grid (1 second)

### Input Handling Architecture

The input system (`modes/` package) is fully decoupled from game logic through **event-driven architecture** via the dedicated `events` package. The `modes` package depends only on `events` for message definitions, with zero dependencies on `engine` or `systems` packages.

**Package Location:** `modes/` (InputHandler, InputMachine, BindingTable, Motion functions, Operators, Commands, Search)
**Event Package:** `events/` (EventType, GameEvent, Payloads, EventQueue, Router[T])
**Dependency Graph:** `modes` → `events` ← `engine` (no circular dependencies)

#### Core Components

**1. InputHandler** (`modes/input.go`)

Central coordinator for all input processing:
- Receives terminal events from main game loop
- Dispatches to mode-specific handlers (Normal, Insert, Search, Command, Overlay)
- Manages mode transitions and state
- Emits events instead of directly modifying game state
- Coordinates with InputMachine for Normal mode vi command parsing

**Key Methods:**
```go
func (h *InputHandler) HandleEvent(ev terminal.Event) bool  // Main entry point
func (h *InputHandler) handleNormalMode()   // Vi command parsing
func (h *InputHandler) handleInsertMode()   // Character typing
func (h *InputHandler) handleSearchMode()   // Search text input
func (h *InputHandler) handleCommandMode()  // Colon command input
func (h *InputHandler) handleOverlayMode()  // Modal window interaction
```

**2. InputMachine** (`modes/machine.go`)

Sophisticated state machine for parsing vi-style commands in Normal mode:

**7 States:**
- `StateIdle` - Awaiting input
- `StateCount` - Accumulating count (e.g., "2" in "2dw")
- `StateCharWait` - Waiting for character target (f, F, t, T)
- `StateOperatorWait` - Waiting for motion after operator (d)
- `StateOperatorCharWait` - Operator + character motion (df, dt)
- `StatePrefixG` - Handling 'g' prefix commands (gg, go)
- `StateOperatorPrefixG` - Operator + 'g' prefix (dgg)

**Capabilities:**
- Count accumulation with multiplication (e.g., `2d3w` = delete 6 words)
- Command buffer for display feedback
- Returns `ProcessResult` with action closures and mode changes

**State Machine Flow** (typing `2d3w` - delete 6 words):
```
'2' → StateIdle → StateCount (count1=2)
'd' → StateCount → StateOperatorWait (operator='d')
'3' → StateOperatorWait → StateOperatorWait (count2=3)
'w' → Execute OpDelete with MotionWordForward × 6 → StateIdle
```

**3. BindingTable** (`modes/bindings.go`)

Maps keys to actions using three lookup tables:
- `normal` - Standard Normal mode bindings (h, j, k, l, w, b, i, /, :, etc.)
- `operatorMotions` - Motions valid after operators (w, b, $, gg, etc.)
- `prefixG` - Commands following 'g' prefix (gg, go)

**Binding Structure:**
```go
type Binding struct {
    Action     ActionType      // Motion, CharWait, Operator, ModeSwitch, etc.
    Target     rune            // Canonical identifier
    Motion     MotionFunc      // For standard motions
    CharMotion CharMotionFunc  // For f/F/t/T
}
```

**ActionTypes:**
- `ActionMotion` - Immediate cursor movement (h, j, k, l, w, b, $, gg)
- `ActionCharWait` - Requires target character (f, F, t, T)
- `ActionOperator` - Requires motion (d)
- `ActionPrefix` - Requires second key (g)
- `ActionModeSwitch` - Changes mode (i, /, :)
- `ActionSpecial` - Special handling (x, D, n, N, ;, ,)

**4. Motion Functions** (`modes/motions.go`, `modes/motions_helpers.go`)

Pure computation functions that calculate target positions:

**Function Signatures:**
```go
type MotionFunc func(ctx *GameContext, startX, startY, count int) MotionResult
type CharMotionFunc func(ctx *GameContext, startX, startY, count int, char rune) MotionResult

type MotionResult struct {
    StartX, StartY int
    EndX, EndY     int
    Type           RangeType   // Char or Line
    Style          MotionStyle // Inclusive or Exclusive
    Valid          bool
}
```

**Examples:** `MotionLeft`, `MotionRight`, `MotionWordForward`, `MotionLineEnd`, `MotionFindForward`

**Key Property:** Motions are stateless calculations that return target coordinates without mutating game state.

**5. Operators** (`modes/operators.go`)

Functions that apply motions to the game state by emitting events:

- `OpMove(ctx, result, cmd)` - Updates cursor position in ECS
- `OpDelete(ctx, result)` - **Emits `EventDeleteRequest` event** (decoupled from EnergySystem)

**Key Property:** Operators translate motion results into events, eliminating direct dependencies on game systems.

**6. Commands** (`modes/commands.go`)

Colon command execution system:
- `ExecuteCommand(ctx, command)` - Parses and dispatches commands
- Supports: `:q`, `:new`, `:energy`, `:heat`, `:boost`, `:spawn`, `:debug`, `:help`
- Directly manipulates GameState for debug commands
- Can trigger overlay mode for help/debug

**7. Search Functions** (`modes/search.go`)

Search functionality implementation:
- `PerformSearch(ctx, text, forward)` - Finds matches and moves cursor
- `RepeatSearch(ctx, forward)` - Repeats last search (n, N)

#### Input Event Flow

```
Terminal Event (tcell)
    ↓
InputHandler.HandleEvent()
    ↓
[Mode Router]
    ├─→ handleNormalMode() ──→ InputMachine.Process()
    │        ↓                        ↓
    │   BindingTable            Motion calculation
    │        ↓                        ↓
    │   Action closure          OpMove / OpDelete
    │                                 ↓
    ├─→ handleInsertMode() ────→ PushEvent(EventCharacterTyped)
    ├─→ handleSearchMode() ────→ PerformSearch()
    ├─→ handleCommandMode() ───→ ExecuteCommand()
    └─→ handleOverlayMode() ───→ Scroll/Close overlay
         ↓
EventQueue (lock-free, events pkg)
         ↓
ClockScheduler.DispatchEventsImmediately()
         ↓
EventRouter[*engine.World] (events pkg)
         ↓
Handler[*engine.World] implementations (systems pkg)
         ↓
Systems (EnergySystem, NuggetSystem, CleanerSystem, etc.)
```

#### Mode-Specific Behavior

**Normal Mode:**
- Full vi command parsing via InputMachine
- Arrow keys and special keys wrapped in `World.RunSafe()`
- Rune keys processed through binding table
- Actions executed as closures within `World.RunSafe()`
- Tab emits `EventNuggetJumpRequest` (10 Energy cost)
- Enter emits `EventDirectionalCleanerRequest` (heat ≥ 10, costs 10)
- ESC activates ping grid (1 second)

**Insert Mode:**
- Movement keys (arrows, home, end) work normally
- Typing emits `EventCharacterTyped` events
- Space and backspace handled directly
- Tab emits `EventNuggetJumpRequest`

**Search Mode:**
- Builds search text buffer
- Enter triggers `PerformSearch()` which directly manipulates ECS
- ESC clears buffer and returns to NORMAL

**Command Mode:**
- Builds command text buffer
- Sets pause state via `ctx.SetPaused(true)`
- Enter executes command via `ExecuteCommand()`
- Commands can mutate GameState directly (debug mode)
- ESC clears buffer, unpauses, returns to NORMAL

**Overlay Mode:**
- Modal display for help/debug info
- Supports scrolling (k/j, up/down)
- ESC/Enter closes and unpauses

#### Decoupling Patterns

**1. Event-Driven Communication via Dedicated Package**
- **Package Separation**: `modes` package depends only on `events` package for message definitions
- **No Engine Dependency**: InputHandler emits events without knowing about `engine` or `systems` packages
- **Generic Routing**: Systems subscribe to event types via `Router[*engine.World]` (generic over context type)
- **Zero Coupling**: `events` package contains only data structures, no game logic
- **Import Graph**: `modes` → `events` ← `engine` (no circular dependencies)

**2. Action Closures**
- InputMachine returns action closures in `ProcessResult`
- Closures capture context and are executed later
- Allows pure computation in state machine

**3. Motion as Pure Functions**
- Motion functions are stateless calculations
- Return `MotionResult` describing target, not mutating state
- Operators interpret results and emit events to `events.EventQueue`

**4. Binding Table Configuration**
- Key mappings externalized from state machine logic
- Easy to extend with new bindings
- Supports multiple binding contexts (normal, operator, prefix)

**5. Mode Isolation**
- Each mode has dedicated handler function
- Mode transitions managed centrally in InputHandler
- Modes don't know about each other

### Commands

**`:new` - New Game:**
- Clears ECS World and resets game state
- Phase: Transitions to PhaseNormal
- Event Queue: Drains all pending events
- **Critical**: Restore Cursor Entity after `World.Clear()`:
  ```go
  world.Clear()
  ctx.ResetEventQueue()
  ctx.State.Reset(ctx.PausableClock.Now())

  cursorEntity := world.CreateEntity()
  world.Positions.Add(cursorEntity, components.PositionComponent{
      X: ctx.GameWidth / 2,
      Y: ctx.GameHeight / 2,
  })
  world.Cursors.Add(cursorEntity, components.CursorComponent{})
  world.Protections.Add(cursorEntity, components.ProtectionComponent{
      Mask: components.ProtectAll,
      ExpiresAt: 0,
  })
  ctx.CursorEntity = cursorEntity
  ```

### Special Keys (NORMAL Mode)

**Tab**: Jumps to active Nugget via `EventNuggetJumpRequest` (Cost: 10 Energy)
**ESC**: Activates ping grid for 1 second
**Enter**: 4-directional cleaners via `EventDirectionalCleanerRequest` (heat ≥ 10, costs 10)
**Arrow keys**: Function like h/j/k/l (wrapped in `World.RunSafe()`)

### Supported Vi Motions

**Basic**: h, j, k, l, Space
**Line**: 0 (start), ^ (first non-space), $ (end)
**Word**: w, b, e (word), W, B, E (WORD)
**Screen**: gg (top), G (bottom), go (top-left), H, M, L
**Paragraph**: { (prev empty), } (next empty), % (matching bracket)
**Find/Till**: f<char>, F<char>, t<char>, T<char>, ; (repeat), , (reverse)
**Search**: / (search), n/N (next/prev match)
**Delete**: x (char), dd (line), d<motion>, D (to end)

## Concurrency Model

### Main Architecture
- **Main loop**: Single-threaded ECS updates (16ms frame tick)
- **Input events**: Goroutine → channel → main loop
- **Clock scheduler**: Separate goroutine (50ms tick)
- **All systems**: Run synchronously in main loop

### Shared State Synchronization
- **Color Census**: Per-frame entity iteration (no shared counters)
- **GameState**: `sync.RWMutex` for phase/timing
- **World**: Thread-safe per store (internal locking)

### Crash Handling and Panic Recovery

The game uses a **centralized crash handling system** to ensure terminal cleanup on panic:

**Architecture:**
- `GameContext.crashHandler`: Function called when background goroutines panic
- `GameContext.Go(fn)`: Wrapper for safe goroutine execution with panic recovery
- Dependency injection from `main` package to avoid terminal package coupling

**Implementation Pattern:**
```go
// Main package sets crash handler (has terminal dependency)
ctx.SetCrashHandler(func(r any) {
    terminal.EmergencyReset(os.Stdout)
    fmt.Fprintf(os.Stderr, "\r\n\x1b[31mGAME CRASHED: %v\x1b[0m\r\n", r)
    fmt.Fprintf(os.Stderr, "Stack Trace:\r\n%s\r\n", debug.Stack())
    os.Exit(1)
})

// Engine package uses wrapper (terminal-independent)
ctx.Go(func() {
    // Game logic that might panic
})
```

**Direct Panic Handling:**
- **Main goroutine** (`main.go`): `defer recover()` restores terminal on crash
- **Input poller goroutine** (`main.go`): `defer recover()` for event polling
- **Render goroutine**: Runs in main loop, protected by main's defer

**Usage:**
- ClockScheduler uses `ctx.Go()` for safe tick loop execution
- Any system spawning goroutines should use `ctx.Go()` wrapper
- Terminal-dependent code uses direct `defer recover()` blocks

## Race Condition Prevention

### Design Principles
1. Single-threaded ECS
2. No autonomous goroutines
3. Explicit synchronization (atomics/mutexes)
4. Frame-coherent snapshots
5. Minimal lock scope

### CleanerSystem Concurrency
- Pure ECS pattern (state in `CleanerComponent`)
- Synchronous updates in main loop
- Event-driven activation
- Component-based physics
- Snapshot rendering (deep-copy trails)
- Zero state duplication

### Frame Coherence Strategy
1. Renderer queries World for all `CleanerComponent` entities
2. Deep-copies trail slice per cleaner
3. Renderer uses trail copy (no shared references)
4. Main loop updates via ECS synchronized methods
5. No data races

## Performance Guidelines

### Hot Path Optimizations
1. Generics-based queries (zero-allocation)
2. `GetAllAtInto()` with stack buffers
3. SpatialGrid's cache-friendly layout (128-byte cells)
4. Batch similar operations
5. Reuse allocated slices
6. Synchronous CleanerSystem updates
7. Pre-calculated rendering gradients

### Memory Management
- Pool temporary slices
- Clear references before destroying entities
- Limit total entity count (MAX_CHARACTERS = 200)

## Game Systems

### System Priorities

Systems execute in priority order (lower = earlier):
1. **BoostSystem (5)**: Boost timer expiration
2. **EnergySystem (10)**: Process input, update energy
3. **SpawnSystem (15)**: Generate Blue/Green sequences
4. **NuggetSystem (18)**: Nugget spawning/collection
5. **GoldSystem (20)**: Gold lifecycle
6. **CleanerSystem (22)**: Cleaner physics/collision
7. **DrainSystem (25)**: Drain movement/logic
8. **DecaySystem (30)**: Sequence degradation
9. **FlashSystem (35)**: Flash effect lifecycle
10. **SplashSystem (800)**: Splash lifecycle and gold timer updates (after game logic, before rendering)

### System Communication Patterns

**Event-Driven Systems** (fully decoupled via events):
- **SplashSystem**: Subscribes to 5 events, no direct system calls
- **ShieldSystem**: Subscribes to 3 events, no direct system calls
- **FlashSystem**: Pure functional helper, no coupling
- **CleanerSystem**: Subscribes to 2 events, emits 1 event
- **EnergySystem**: Subscribes to 3 events, emits 4 events
- **GoldSystem**: Subscribes to 1 event, emits 2 events
- **NuggetSystem**: Subscribes to 1 event, emits 1 event

**Clock-Tick Systems** (update-based):
- **BoostSystem**: Pure state management, no events
- **SpawnSystem**: Content spawning only, no event communication
- **DrainSystem**: Hostile entity management, emits 1 event
- **DecaySystem**: Sequence degradation, direct scheduler integration

**GameContext Coupling** (by design):
- **AudioEngine** access for sound effects (EnergySystem, GoldSystem, NuggetSystem, CleanerSystem)
- **CursorEntity** access for spatial reference (EnergySystem, GoldSystem, NuggetSystem, DrainSystem, SpawnSystem)
- **GameState** access for centralized state management (all systems)

### Content Management System
- **ContentManager** (`content/manager.go`): Manages content files
- Auto-discovery: Scans `data/` for `.txt` files
- Validation: Pre-validates at startup
- Block Selection: Random blocks (3-15 lines)
- Location: Auto-locates project root via `go.mod`

### Spawn System

Clock-tick system managing content spawning and difficulty scaling.

**Communication:** Pure update-based, no event handlers or emission

**Content Source:**
- Loads from `.txt` files in `data/` directory via ContentManager
- Block generation: 3-15 consecutive lines, trimmed
- Background pre-fetch using `ctx.Go()` for performance

**Spawn Rate:**
- Base: 2 seconds
- Adaptive: 1-4 seconds based on screen density
- Controlled via GameState spawn timing

**6-Color Limit:**
- Tracks Blue×3 + Green×3 combinations (18 possible states)
- Census-based: per-frame entity iteration via `runCensus(world)`
- Only spawns when <6 combinations present
- Red/Gold excluded from limit

**Placement Strategy:**
- Random positions with 3 attempts per line
- Collision detection via PositionStore
- Cursor exclusion zone (5 horizontal, 3 vertical)
- Failed attempts discarded

**Generates:** Only Blue and Green sequences (Red comes from decay)

### Decay System

Clock-tick system managing sequence degradation through falling entity animation.

**Communication:** Direct scheduler integration, no event handlers or emission

**Phase Integration:**
- Triggered by ClockScheduler when decay timer expires (60-10 seconds based on heat)
- Manages PhaseDecayAnimation state via GameState
- Returns to PhaseNormal when animation completes

**Decay Mechanics:**
- **Brightness**: Bright → Normal → Dark
- **Color Chain**:
  - Blue (Dark) → Green (Bright)
  - Green (Dark) → Red (Bright) ← **ONLY source of Red sequences**
  - Red (Dark) → Destroyed
- **Timing**: 60-10 seconds based on heat percentage
- **Formula**: `60s - (50s * (CurrentHeat / MaxHeat))`

**Falling Animation:**
- One decay entity per column (`DecayComponent` in `World.Decays`)
- Speed: Random 5.0-15.0 rows/second
- Physics: `PreciseY += Speed × dt.Seconds()`
- Matrix effect: Random character changes during fall
- Auto-destroy when entity exits bottom of screen

**Collision Detection:**
- **Swept traversal**: Checks all rows between prev/current position (anti-tunneling)
- **Coordinate latching**: `LastIntX`, `LastIntY` prevent re-processing same cell
- **Frame deduplication**: `processedGridCells` map prevents double-hits
- **Entity deduplication**: `decayedThisFrame` map prevents repeat decay of same entity

**Thread Safety:**
- `sync.RWMutex` protects animation state
- Falling entities stored in `World.Decays` (pure ECS)
- Deduplication maps are system-local state

### Energy System

Event-driven system for character interaction and energy/heat management.

**Event Handling:**
- Consumes: `EventCharacterTyped`, `EventEnergyTransaction`, `EventDeleteRequest`
- Emits: `EventSplashRequest`, `EventShieldActivate`, `EventShieldDeactivate`, `EventDirectionalCleanerRequest`, `EventCleanerRequest`, `EventGoldComplete`

**Responsibilities:**
- Character typing validation and sequence destruction
- Heat accumulation (with boost multiplier)
- Energy management and shield state control
- Error handling (resets heat, plays error sound)
- Cleaner triggering (gold/nugget completion at max heat)

**Hit Detection:**
```go
var entityBuf [engine.MaxEntitiesPerCell]engine.Entity
count := world.Positions.GetAllAtInto(cursorX, cursorY, entityBuf[:])
entitiesAtCursor := entityBuf[:count]

for _, entity := range entitiesAtCursor {
    if entity == s.ctx.CursorEntity {
        continue
    }
    // Process typing...
}
```

### Boost System

Clock-tick system managing boost timer and heat multiplier state.

**Communication:** Pure state management, no event handlers or emission

**Activation:**
- Automatically activates when heat reaches 100
- Managed via GameState atomic fields (`BoostEnabled`, `BoostEndTime`, `BoostColor`)

**Timing:**
- **Initial Duration**: 500ms
- **Extension**: +500ms per matching color/level sequence typed
- **Color Binding**: Tied to triggering sequence color/level
- **Timer Check**: Runs every Update() tick, deactivates on expiration

**Effects:**
- 2× heat multiplier while active
- Visual feedback: Pink "Boost: X.Xs" in status bar

### Gold System

Event-driven system managing gold sequence lifecycle and phase transitions.

**Event Handling:**
- Consumes: `EventGoldComplete`
- Emits: `EventGoldSpawned`, `EventGoldTimeout`

**Lifecycle:**
- **Spawn**: After decay animation completes
- **Position**: Random location avoiding cursor
- **Length**: 10 alphanumeric characters
- **Duration**: 10 seconds (pausable, tracked via GameState)
- **Completion**: Via `EventGoldComplete` from EnergySystem
- **Timeout**: Self-triggered via `EventGoldTimeout` after 10 seconds

**Reward:**
- Fills heat to max
- Triggers cleaners if heat already at max (via EnergySystem)

### Nugget System

Event-driven system managing nugget spawning and collection mechanics.

**Event Handling:**
- Consumes: `EventNuggetJumpRequest` (from Tab key)
- Emits: `EventEnergyTransaction` (jump energy cost)

**Behavior:**
- Spawns every 5 seconds if no active nugget exists
- Orange alphanumeric character displayed on screen

**Collection Methods:**
- **Typing**: Type the nugget character at its position
- **Jump (Tab)**: Instant cursor teleport (costs 10 Energy via `EventEnergyTransaction`)

**Rewards:**
- +10% max heat capacity
- Triggers 4-directional cleaners if heat at max (via EnergySystem)

### Drain System

Clock-tick system managing hostile entities with shield interaction.

**Communication:** Update-based, emits `EventShieldDrain` for shield interactions

**Purpose:**
- Hostile entities that scale with heat level
- Pressure mechanic forcing energy/shield management

**Spawn Mechanics:**
- **Trigger**: Heat ≥ 10
- **Count**: `floor(Heat/10)`, maximum 10 drains
- **Position**: Random offset (±10) from cursor
- **Staggered**: 4 ticks between spawns (prevents spawn storms)
- **LIFO**: SpawnOrder tracks despawn priority
- **Materialize**: 1-second cyan block animation from screen edges

**Movement:**
- Moves toward cursor every 1 second
- Uses PositionStore for spatial queries
- Path-finding: Direct line toward cursor position

**Despawn Conditions:**
- Energy ≤ 0 AND Shield inactive
- Excess drain count (LIFO order)
- Drain-Drain collisions (mutual destruction)
- Cursor collision without shield (-10 Heat, drain despawns)

**Shield Interaction:**
- **Zone Check**: Drains inside shield radius emit `EventShieldDrain`
- **Energy Drain**: 100 energy/tick per drain (via ShieldSystem event handler)
- **No Heat Loss**: Drains persist but don't damage heat when shield active
- **Shield Query**: Checks `GameState.GetShieldActive()` for protection state

### Cleaner System

Event-driven system managing Red sequence cleanup animations.

**Event Handling:**
- Consumes: `EventCleanerRequest`, `EventDirectionalCleanerRequest`
- Emits: `EventCleanerFinished` (decorative, no consumers)

**Horizontal Row Cleaners:**
- **Trigger**: `EventCleanerRequest` when gold completed at max heat
- **Behavior**: Sweeps rows containing Red characters
- **Phantom Mode**: No spawn if no Red exists (still emits `EventCleanerFinished`)
- **Direction**: Alternating L→R / R→L per row
- **Selectivity**: Only destroys Red sequences

**Directional Cleaners (4-Way):**
- **Triggers**:
  - Nugget collected at max heat (via EnergySystem)
  - Enter key in NORMAL mode (requires heat ≥ 10, costs 10 heat)
- **Behavior**: 4 cleaners from cursor origin (right, left, down, up)
- **Position Lock**: Row/column locked at spawn (horizontal/vertical movement only)
- **Selectivity**: Only destroys Red sequences

**Common Architecture:**
- Pure ECS (state in `CleanerComponent`)
- Vector physics: `position += velocity × dt`
- Trail rendering: Ring buffer (10 positions, FIFO)
- Lifecycle: Spawn off-screen → traverse to opposite edge → auto-destroy
- Collision: Swept segment detection (anti-tunneling)
- Visual: Pre-calculated gradients, opacity falloff
- Thread Safety: Event-driven activation, lock-free queue, synchronous ECS updates

### Flash System

Pure functional system providing visual feedback for entity destruction.

**Communication:** No event handlers, provides functional helper for other systems

**Architecture:**
- Pure ECS (`FlashComponent`)
- No coupling to other systems
- Functional helper: `SpawnDestructionFlash(world, x, y, char, now)`

**Usage:**
- Called by: CleanerSystem, DrainSystem, DecaySystem
- Creates flash entity at destruction site
- Default duration: 300ms

**Lifecycle:**
- Auto-cleanup after duration expires
- Update() checks `GameTime` against `StartTime + Duration`

### Splash System

The Splash System provides large block-character visual feedback for player actions and displays the gold countdown timer.

**Architecture:**
- **Pure ECS**: Multiple concurrent splash entities, no singletons
- **Event-Driven**: Subscribes to `EventSplashRequest`, `EventGoldSpawned`, `EventGoldComplete`, `EventGoldTimeout`, `EventGoldDestroyed`
- **Component-Based**: `SplashComponent` stores content, color, position, lifecycle mode, and timing data
- **No Position Component**: Splash entities don't use PositionStore (positioned via AnchorX/AnchorY fields)

**Lifecycle Modes:**
- **`SplashModeTransient`**: Auto-expire after duration (typing feedback, commands, nuggets)
  - Enforces uniqueness: Only one transient splash active at a time
  - Duration: 1 second fade-out
- **`SplashModePersistent`**: Event-driven lifecycle (gold countdown timer)
  - Created by `EventGoldSpawned` with gold `SequenceID`
  - Destroyed by `EventGoldComplete`, `EventGoldTimeout`, or `EventGoldDestroyed`
  - Updates every frame to display remaining time (9 → 0)
  - Anchored to gold sequence position

**Event Triggers:**
- `EventSplashRequest`: Character typed (EnergySystem), nugget collected (EnergySystem), commands executed (InputHandler) → creates transient splash
- `EventGoldSpawned`: Gold sequence created (GoldSystem) → creates persistent timer splash
- `EventGoldComplete`: Gold typed correctly (GoldSystem) → destroys timer splash
- `EventGoldTimeout`: Gold sequence timed out (GoldSystem) → destroys timer splash
- `EventGoldDestroyed`: Gold destroyed by external event (DrainSystem) → destroys timer splash

**Smart Layout (Transient Splashes):**
- Quadrant-based placement avoiding cursor and gold sequences
- Scoring system: Opposite quadrant preferred (-1000 for cursor quadrant, -50 per gold char)
- Boundary clamping to keep splash within game area
- Origin provided by event payload (usually cursor position)

**Gold Timer Anchoring (Persistent Splashes):**
- Horizontally centered over gold sequence
- Positioned 2 rows above sequence (fallback: below if top clipped)
- Tracks gold SequenceID for lifecycle management

**Rendering** (`SplashRenderer`):
- Bitmap font from `assets/splash_font.go` (16×12 per character, MSB-first)
- Background-only effect via `SetBgOnly()` with `MaskEffect` write mask
- Linear fade-out animation (opacity: 1.0 → 0.0)
- Color resolution: `SplashColor` enum → `render.RGB`
- Max 8 characters, 1-pixel spacing between characters

**Color Coding** (`SplashColor` enum):
- `SplashColorGreen/Blue/Red`: Sequence colors (normal brightness)
- `SplashColorGold`: Bright yellow (gold sequences and timer)
- `SplashColorNugget`: Orange (nugget collection)
- `SplashColorNormal`: Dark orange (commands in Normal mode)
- `SplashColorInsert`: Specific color for Insert mode feedback

**Update Loop** (`SplashSystem.Update()`):
- Transient: Check expiry, destroy if duration elapsed
- Persistent: Update countdown digit (9→8→...→0)
- Frame-accurate timing using `TimeResource.GameTime`
- No orphan detection needed (event-driven lifecycle ensures cleanup)

**Integration Points:**
- EnergySystem: Character/nugget typing → `EventSplashRequest`
- InputHandler: Command execution → `EventSplashRequest`
- GoldSystem: Sequence spawn → `EventGoldSpawned`, completion → `EventGoldComplete/Timeout`

**Thread Safety:**
- Pure ECS (state in component)
- Event-driven (lock-free queue)
- No shared state between systems

### Shield System

Event-driven energy-powered protective field that activates when energy is available.

**Activation Model:**
- **Trigger**: Energy > 0 (pure energy-gated)
- **State Ownership**: `GameState.ShieldActive` atomic bool
- **Event-Driven Control**: All activation/deactivation via events

**Event Flow:**
```
EnergySystem.Update()
    └─► if energy > 0 && !shieldActive → PushEvent(EventShieldActivate)
    └─► if energy <= 0 && shieldActive → PushEvent(EventShieldDeactivate)

DrainSystem.handleDrainInteractions()
    └─► if drain inside shield zone → PushEvent(EventShieldDrain, amount)

ClockScheduler.processTick()
    └─► eventRouter.DispatchAll() → ShieldSystem.HandleEvent()
    └─► world.Update() → ShieldSystem.Update() (passive drain)
```

**ShieldSystem Responsibilities:**
- **Event Handling**: Consumes `EventShieldActivate`, `EventShieldDeactivate`, `EventShieldDrain`
- **Activation**: Sets `ShieldActive` atomic flag, updates component state
- **Deactivation**: Clears `ShieldActive` atomic flag, cleans up component state
- **Passive Drain**: Drains 1 energy/second while active (in `Update()`)
- **External Drain**: Processes `EventShieldDrain` events from DrainSystem

**Energy Costs:**
- **Passive**: 1 energy/second (handled by ShieldSystem.Update)
- **Shield Zone**: 100 energy/tick per drain (via EventShieldDrain from DrainSystem)

**Defense Mechanism:**
- DrainSystem queries `GameState.GetShieldActive()` to check protection
- Drains inside shield zone push `EventShieldDrain` instead of draining heat
- Shield deactivates when energy depleted (EventShieldDeactivate)

**Visual Rendering:**
- **ShieldRenderer** queries `ctx.ShieldActive` from RenderContext
- **Elliptical field** with linear gradient
- **Color**: Derived from `GameState.LastTypedSeqType/Level` (set by EnergySystem)
- **Visibility**: Only renders when `ShieldActive` is true

**Component**: `{Energy int64, RadiusX, RadiusY float64, OverrideColor ColorClass, MaxOpacity float64}`

**Key Design Decisions:**
- **Decoupled from BoostSystem**: Boost no longer manages shield state
- **Centralized State**: Single source of truth (`GameState.ShieldActive`)
- **Event-Driven**: All state transitions via events (no direct system coupling)
- **Pure Energy-Gated**: Activation depends only on energy availability

### Audio System

Vi-fighter uses a **pure Go audio system** with zero CGO dependencies. PCM waveforms are synthesized in memory and piped to system audio tools.

**Integration Points:**

- **Sound Effects**:
  - `SoundError` - Typing errors (EnergySystem)
  - `SoundBell` - Nugget collection (NuggetSystem)
  - `SoundWhoosh` - Cleaner activation (CleanerSystem)
  - `SoundCoin` - Gold sequence completion (GoldSystem)

- **Game Systems**: EnergySystem, GoldSystem, NuggetSystem, CleanerSystem call `ctx.AudioEngine.Play(soundType)` to trigger sounds

- **User Controls**: Ctrl+S toggles mute (starts muted by default)

- **Pause Behavior**: Entering COMMAND mode stops audio and drains queues

- **Fallback**: Gracefully enters silent mode if no audio backend available (game continues normally)

**Platform Support:**
- Linux (PulseAudio, PipeWire, ALSA)
- FreeBSD (PulseAudio or OSS)
- macOS not supported (no testable backend)

**Audio Package Location:** `audio/` directory

**For complete audio package implementation details**, see [audio.md](./audio.md).

## Extension Points

### Adding New Components
1. Define data struct implementing `Component`
2. Register in relevant systems
3. Ensure proper `PositionStore` integration if position-related

### Adding New Systems
1. Implement `System` interface
2. Define `Priority()` for execution order
3. Register in `main.go`

### Adding New Visual Effects
1. Create component for effect data
2. Create `SystemRenderer` in `render/renderers/`
3. Register with orchestrator

## Invariants to Maintain

1. **Multi-Entity Cells**: Max 15 entities per cell
2. **Component Consistency**: SequenceComponent entities MUST have Position and Character
3. **Cursor Bounds**: `0 <= CursorX < GameWidth && 0 <= CursorY < GameHeight`
4. **Heat Range**: 0-100 (normalized, max always 100)
5. **Boost Mechanic**: Heat reaches 100 → boost activates, matching color extends timer
6. **Red Spawn Invariant**: Red NEVER spawned directly, only via decay
7. **Gold Randomness**: Random positions
8. **6-Color Limit**: Max 6 Blue/Green color/level combinations
9. **Census Accuracy**: Entity iteration provides exact counts

## Known Constraints and Limitations

### SpatialGrid Cell Capacity

Max 15 entities per cell:
- **Soft Clipping**: Full cells ignore additional `Add()` calls
- **Spawn Validation**: Cursor exclusion, collision detection, batch operations
- **Cursor Protection**: MUST have `ProtectionComponent` with `Mask: ProtectAll`
- **Rationale**: Fixed capacity enables value-type cells (cache-friendly, 128-byte = 2 cache lines)

## Content Files

### data/ directory
- **Purpose**: `.txt` files with game content
- **Format**: Plain text (code blocks, prose)
- **Location**: Project root, auto-located via `go.mod`
- **Discovery**: ContentManager scans all `.txt` (excludes hidden)
- **Processing**: Pre-validated, cached at init, lines trimmed, min 10 valid lines
- **Block Grouping**: 3-15 lines based on indent/brace depth
