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
- `EventQueue` access via `ctx.PushEvent()`
- `AudioEngine` access via `ctx.AudioEngine.PlaySound()`
- Input handling methods (mode transitions, motion commands)
- `CursorX`, `CursorY` fields cache ECS position for motion handlers
  - MUST sync FROM ECS before use: `pos, _ := ctx.World.Positions.Get(ctx.CursorEntity); ctx.CursorX = pos.X`
  - MUST sync TO ECS after modification

### Event-Driven Communication

**EventRouter** pattern for decoupled system communication:

**Architecture Flow:**
```
Producer → EventQueue → ClockScheduler → EventRouter → EventHandler → Systems
(InputHandler)  (lock-free)   (tick loop)    (dispatch)   (consume)
```

**Core Principles:**
- Systems never call each other's methods directly
- Lock-free queue with atomic CAS operations
- Centralized dispatch before World.Update()
- Frame deduplication to prevent duplicate processing

**Event Types:**

| Event | Producer | Consumer | Payload |
|-------|----------|----------|---------|
| `EventCharacterTyped` | InputHandler | EnergySystem | `*CharacterTypedPayload{Char, X, Y}` |
| `EventEnergyTransaction` | InputHandler | EnergySystem | `*EnergyTransactionPayload{Amount, Source}` |
| `EventCleanerRequest` | EnergySystem | CleanerSystem | `nil` |
| `EventDirectionalCleanerRequest` | InputHandler, EnergySystem | CleanerSystem | `*DirectionalCleanerPayload{OriginX, OriginY}` |
| `EventCleanerFinished` | CleanerSystem | (observers) | `nil` |
| `EventGoldComplete` | EnergySystem | (observers) | `nil` |
| `EventSplashRequest` | EnergySystem, InputHandler | SplashSystem | `*SplashRequestPayload{Text, Color, OriginX, OriginY}` |
| `EventGoldSpawned` | GoldSystem | SplashSystem | `*GoldSpawnedPayload{SequenceID, OriginX, OriginY, Length, Duration}` |
| `EventGoldTimeout` | GoldSystem | SplashSystem | `*GoldCompletionPayload{SequenceID}` |
| `EventGoldDestroyed` | GoldSystem | SplashSystem | `*GoldCompletionPayload{SequenceID}` |

**Producer Pattern:**
```go
payload := &engine.CharacterTypedPayload{Char: r, X: x, Y: y}
h.ctx.PushEvent(engine.EventCharacterTyped, payload, h.ctx.PausableClock.Now())
```

**Consumer Pattern:**
```go
func (s *EnergySystem) EventTypes() []engine.EventType {
    return []engine.EventType{engine.EventCharacterTyped, engine.EventEnergyTransaction}
}

func (s *EnergySystem) HandleEvent(world *engine.World, event engine.GameEvent) {
    switch event.Type {
    case engine.EventCharacterTyped:
        if payload, ok := event.Payload.(*engine.CharacterTypedPayload); ok {
            s.handleCharacterTyping(world, payload.X, payload.Y, payload.Char)
        }
    }
}
```

### State Ownership Model

**GameState** (`engine/game_state.go`) centralizes game state with clear ownership boundaries:

#### Real-Time State (Lock-Free Atomics)
- **Heat**, **Energy** (`atomic.Int64`): Current values
- **Cursor Position**: Managed in ECS, cached in GameContext
- **Boost State** (`atomic.Bool`, `atomic.Int64`): Enabled, EndTime, Color
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

**Migration Changes:**

1. **Component Structure:**
   - `DecayComponent`: Now has `PreciseX`, `PreciseY` (full 2D float) instead of `Column`, `YPosition`
   - `CleanerComponent`: Grid position removed; now sourced from `PositionComponent`
   - `MaterializeComponent`: Grid position removed; now sourced from `PositionComponent`

2. **PositionStore Integration:**
   - All gameplay entities now register in `PositionStore` for unified spatial queries
   - Enables single iteration cleanup in `cleanupOutOfBoundsEntities()`
   - Simplifies collision detection (no custom bypass logic needed)

3. **Spawn Protocol:**
   ```go
   entity := world.CreateEntity()
   world.Positions.Add(entity, components.PositionComponent{X: gridX, Y: gridY})  // Grid registration
   world.Decays.Add(entity, components.DecayComponent{
       PreciseX: float64(gridX),  // Float overlay
       PreciseY: float64(gridY),
       // ...
   })
   ```

4. **Grid Sync Protocol:**
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

5. **Self-Exclusion Requirement:**
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
├── ShieldComponent {Sources uint8, RadiusX, RadiusY float64, OverrideColor ColorClass, MaxOpacity float64, LastDrainTime time.Time}
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
- `ShieldRenderer`: Protective field with gradient (writes MaskShield, derived from GameState.LastTypedSeqType/Level)
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

### Input Dispatch Architecture (NORMAL Mode)

**State machine with binding table** replaces enum+switch pattern.

**Core Components:**

1. **InputMachine** (`modes/machine.go`):
   - 7-state machine managing input parsing
   - Tracks count accumulation (count1/count2 for `2d3w`)
   - Dispatches via binding table lookup

2. **BindingTable** (`modes/bindings.go`):
   - Maps keys to action types
   - Tables: `normal[]`, `operatorMotions[]`, `prefixG[]`

   ```go
   type Binding struct {
       Action       ActionType
       Target       rune
       AcceptsCount bool
       Executor     func(*engine.GameContext, int)
   }
   ```

3. **ActionType Enum:**
   - `ActionMotion`: Immediate (h,j,k,l,w,b)
   - `ActionCharWait`: Wait for target (f,F,t,T)
   - `ActionOperator`: Wait for motion (d)
   - `ActionPrefix`: Wait for second key (g)
   - `ActionModeSwitch`: Change mode (i,/,:)
   - `ActionSpecial`: Immediate with special handling (x,D,n,N)

**State Machine States:**

| State | Description | Transitions |
|-------|-------------|-------------|
| `StateIdle` | Awaiting input | Digit→StateCount, Operator→StateOperatorWait, Motion→execute |
| `StateCount` | Accumulating count | Continue accumulating or execute with count |
| `StateOperatorWait` | Operator pending | Motion→execute operator+motion |
| `StateOperatorCharWait` | Operator + char wait | Char→execute |
| `StateCharWait` | Awaiting target char | Char→execute find/till |
| `StatePrefixG` | Prefix 'g' pending | Second char→execute |
| `StateOperatorPrefixG` | Operator + 'g' | Second char→execute |

**Example Flow** (typing `2d3w` - delete 6 words):
1. `'2'` → StateIdle → StateCount, count1=2
2. `'d'` → StateCount → StateOperatorWait, operator='d'
3. `'3'` → StateOperatorWait → stay, count2=3
4. `'w'` → Execute `DeleteMotion(ctx, 'w', 6)` → StateIdle

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

### Motion Commands (NORMAL Mode)

**Single character**: h, j, k, l, w, b, e, etc.
**Prefix commands**: `gg`, `go`, `dd`, `dw`, `d$`, `f<char>`, `F<char>`
**Count prefix**: `5j`, `10l`, `3w`, `2fa`, `3Fb`
**Consecutive move penalty**: h/j/k/l >3 times resets heat
**Arrow keys**: Function like h/j/k/l but always reset heat
**Tab**: Jumps to active Nugget (Cost: 10 Energy)
**ESC**: Ping grid for 1 second
**Enter**: 4-directional cleaners from cursor (heat ≥ 10, costs 10)

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

### Content Management System
- **ContentManager** (`content/manager.go`): Manages content files
- Auto-discovery: Scans `data/` for `.txt` files
- Validation: Pre-validates at startup
- Block Selection: Random blocks (3-15 lines)
- Location: Auto-locates project root via `go.mod`

### Spawn System
- **Content Source**: `.txt` files in `data/`
- **Block Generation**: 3-15 consecutive lines, trimmed
- **6-Color Limit**: Tracks Blue×3 + Green×3 combinations
  - Census-based: per-frame entity iteration
  - SpawnSystem runs `runCensus(world)` returning `ColorCensus`
  - Only spawns when <6 combinations present
  - Red/Gold excluded from limit
- **Placement**: Random locations, 3 attempts per line
  - Collision detection
  - Cursor exclusion zone (5H, 3V)
  - Failed attempts discarded
- **Rate**: 2s base, adaptive (1-4s)
- **Generates**: Only Blue and Green

### Decay System

Applies degradation through falling entity animation.

**Decay Mechanics:**
- **Brightness**: Bright → Normal → Dark
- **Color Chain**:
  - Blue (Dark) → Green (Bright)
  - Green (Dark) → Red (Bright) ← ONLY source of Red
  - Red (Dark) → Destroyed
- **Timing**: 10-60s based on heat
- **Formula**: `60s - (50s * heatPercentage)`

**Falling Animation:**
- One entity per column (`DecayComponent` in `World.Decays`)
- Speed: Random 5.0-15.0 rows/sec
- Physics: `YPosition += Speed × dt.Seconds()`
- Matrix effect: Random character changes
- Cleanup: Auto-destroy when done

**Collision Detection:**
- **Swept traversal**: Checks all rows between prev/current position (anti-tunneling)
- **Coordinate latching**: `LastIntX`, `LastIntY` prevent re-processing (anti-green artifacts)
- **Frame deduplication**: `processedGridCells` map prevents double-hits
- **Entity deduplication**: `decayedThisFrame` map prevents repeat decay

**Thread Safety:**
- `sync.RWMutex` protects system state
- Falling entities queried from `World.Decays` (no internal tracking)
- Deduplication maps are system-internal state

### Energy System
- Character typing in insert mode
- Heat updates (with boost multiplier)
- Error handling (resets heat)
- Census impact: destruction affects next spawn

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
- **Activation**: Heat reaches 100
- **Duration**: 500ms initial
- **Color Binding**: Tied to triggering color
- **Extension**: +500ms per matching color
- **Effects**: 2× heat multiplier, shield activation
- **Visual**: Pink "Boost: X.Xs" in status bar

### Gold System
- **Trigger**: Spawns after decay animation
- **Position**: Random location avoiding cursor
- **Length**: 10 alphanumeric characters
- **Duration**: 10 seconds (pausable)
- **Reward**: Fills heat to max
- **Cleaner Trigger**: If heat already max when completed

### Nugget System
- **Behavior**: Spawns every 5 seconds if no active nugget
- **Collection**:
  - Typing: Type character on nugget
  - Jump (Tab): Instant jump to nugget (10 Energy cost)
- **Reward**: +10% max heat
- **Bonus**: 4 cleaners if heat at max when collected

### Drain System
- **Purpose**: Hostile entities scaling with heat
- **Spawn**: Heat ≥ 10, count = `floor(Heat/10)`, max 10
- **Position**: Random offset (±10) from cursor
- **Staggered**: 4 ticks between spawns
- **LIFO**: SpawnOrder tracks despawn priority
- **Despawn**: Energy ≤ 0 AND Shield inactive, OR excess drains, OR collisions
- **Materialize**: 1s cyan block animation from edges
- **Movement**: Toward cursor every 1s
- **Collisions**:
  - Drain-Drain: Mutual destruction
  - Cursor (No Shield): -10 Heat, drain despawns
  - Cursor (Shield): Energy drain, no heat loss, drain persists
  - Shield Zone: 100 energy/tick drain

### Cleaner System

**Horizontal Row Cleaners:**
- Trigger: `EventCleanerRequest` when gold at max heat
- Behavior: Sweeps rows with Red characters
- Phantom: No spawn if no Red (still pushes EventCleanerFinished)
- Direction: Alternating L→R / R→L
- Selectivity: Only destroys Red

**Directional Cleaners (4-Way):**
- Trigger: `EventDirectionalCleanerRequest`
  - Nugget at max heat: EnergySystem
  - Enter key (heat ≥ 10): Input handler, costs 10 heat
- Behavior: 4 cleaners from origin (right, left, down, up)
- Position Lock: Row/column locked at spawn
- Selectivity: Only destroys Red

**Common Architecture:**
- Pure ECS (state in `CleanerComponent`)
- Vector physics: `position += velocity × dt`
- Trail: Ring buffer (10 positions, FIFO)
- Lifecycle: Spawn off-screen → target opposite side → destroy
- Collision: Swept segment (anti-tunneling)
- Visual: Pre-calculated gradients, opacity falloff
- Thread Safety: Event-driven, lock-free queue, ECS sync

### Flash System
- **Purpose**: Visual flash for entity destruction
- **Architecture**: Pure ECS (`FlashComponent`)
- **Duration**: 300ms default
- **Spawn**: `SpawnDestructionFlash(world, x, y, char, now)`
- **Usage**: CleanerSystem, DrainSystem, DecaySystem
- **Lifecycle**: Automatic cleanup after duration

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
- **`SplashModePersistent`**: Persistent until explicitly destroyed (gold countdown timer)
  - Anchored to gold sequence position
  - Updates every frame to display remaining time (9 → 0)
  - Orphan detection: Auto-cleanup if gold sequence destroyed

**Event Triggers:**
- `EventSplashRequest`: Character typed (EnergySystem), nugget collected (EnergySystem), commands executed (InputHandler)
- `EventGoldSpawned`: Gold sequence created (GoldSystem) → creates persistent timer splash
- `EventGoldComplete/Timeout/Destroyed`: Gold finished → destroys timer splash

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
- Persistent: Update countdown digit (9→8→...→0), orphan detection
- Frame-accurate timing using `TimeResource.GameTime`

**Integration Points:**
- EnergySystem: Character/nugget typing → `EventSplashRequest`
- InputHandler: Command execution → `EventSplashRequest`
- GoldSystem: Sequence spawn → `EventGoldSpawned`, completion → `EventGoldComplete/Timeout`

**Thread Safety:**
- Pure ECS (state in component)
- Event-driven (lock-free queue)
- No shared state between systems

### Shield System
- **Purpose**: Energy-powered protective field
- **Activation**: Sources != 0 AND Energy > 0
- **Energy Costs**:
  - Passive: 1/second
  - Shield Zone: 100/tick per drain
- **Defense**: Drains drain energy (not heat) while active
- **Visual**:
  - Elliptical field with linear gradient
  - Color derived from GameState.LastTypedSeqType/Level
  - Only renders when `IsShieldActive()` true
- **Component**: `{Sources uint8, RadiusX, RadiusY float64, OverrideColor ColorClass, MaxOpacity float64, LastDrainTime time.Time}`

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
