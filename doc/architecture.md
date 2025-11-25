# Vi-Fighter Architecture

## Core Paradigms

### Entity-Component-System (ECS)
**Strict Rules:**
- Entities are ONLY identifiers (uint64)
- Components contain ONLY data, NO logic
- Systems contain ALL logic, operate on component sets
- World is the single source of truth for all game state

### Resource System (Phase 2 Architecture)
**Global Data Pattern:**
- **Resources** store shared, global data (Time, Configuration, Input State)
- **ResourceStore** (`World.Resources`) provides thread-safe, generic access
- **Systems fetch dependencies** at start of `Update()` via `engine.MustGetResource[T](world.Resources)`
- **Decouples systems** from "God Object" GameContext for basic data needs
- **Separation of Concerns**: GameContext handles orchestration, Resources handle data

### Generic ECS Architecture

Vi-fighter uses a **compile-time generics-based ECS** (Go 1.18+) that eliminates reflection from the hot path, providing type safety and improved performance.

**Core Components:**

1. **Generic Stores** (`Store[T]`):
   - Typed component storage with compile-time type checking
   - Operations: `Add(entity, component)`, `Get(entity)`, `Remove(entity)`, `All()`
   - Thread-safe via internal `sync.RWMutex`
   - Zero allocations for component access (no type assertions needed)

2. **PositionStore** (Specialized):
   - Extends `Store[PositionComponent]` with spatial indexing capabilities
   - Internal spatial hash map for O(1) position-based queries
   - Operations: `GetEntityAt(x, y)`, `Move(...)`, `BeginBatch()`
   - Batch operations ensure atomicity for multi-entity spawning with collision detection

3. **Query System** (`QueryBuilder`):
   - Type-safe component intersection queries
   - Uses sparse set intersection starting with smallest store for optimal performance
   - Example: `world.Query().With(world.Positions).With(world.Characters).Execute()`
   - Returns entity slice for iteration

4. **Entity Builder**:
   - Transactional entity creation pattern
   - Example: `world.NewEntity().With(world.Positions, pos).With(world.Characters, char).Build()`
   - Reserves entity ID upfront, commits components atomically on `Build()`

**World Structure:**
```go
type World struct {
    // Global resource store (Phase 2: Resource System)
    Resources      *ResourceStore  // Thread-safe global data (Time, Config, Input)

    // Explicit typed stores (public for system access)
    Positions      *PositionStore
    Characters     *Store[CharacterComponent]
    Sequences      *Store[SequenceComponent]
    GoldSequences  *Store[GoldSequenceComponent]
    Cleaners       *Store[CleanerComponent]
    FallingDecays  *Store[FallingDecayComponent]
    RemovalFlashes *Store[RemovalFlashComponent]
    Nuggets        *Store[NuggetComponent]
    Drains         *Store[DrainComponent]

    // Lifecycle management
    allStores []AnyStore  // For bulk operations like DestroyEntity
}
```

### Resource System (Phase 2: Global Data Decoupling)

The Resource System provides a generic, thread-safe mechanism for systems to access global shared data without coupling to `GameContext`. Resources are stored in `World.Resources` and accessed via type-safe generics.

**Architecture Goals:**
- **Decouple Systems from GameContext**: Systems no longer depend on "God Object" context fields
- **Type-Safe Access**: Generic resource retrieval eliminates type assertions
- **Thread-Safe by Default**: Internal RWMutex ensures safe concurrent access
- **Centralized Updates**: Resources updated at frame/tick boundaries for consistency

**Core Resources:**

1. **`TimeResource`** - Time data for all systems:
   - `GameTime` (time.Time): Current game time (pausable, stops in COMMAND mode)
   - `RealTime` (time.Time): Wall clock time (always advances)
   - `DeltaTime` (time.Duration): Time since last update
   - `FrameNumber` (int64): Current frame count

2. **`ConfigResource`** - Immutable configuration data:
   - `GameWidth`, `GameHeight`: Game area dimensions
   - `ScreenWidth`, `ScreenHeight`: Terminal screen dimensions
   - `GameX`, `GameY`: Game area offset

3. **`InputResource`** - Current input state:
   - `GameMode` (int): Current mode (Normal, Insert, Search, Command)
   - `CommandText` (string): Command buffer text
   - `SearchText` (string): Search buffer text
   - `IsPaused` (bool): Pause state

**Resource Access Pattern:**
```go
// ✅ CORRECT: Fetch resources at start of Update()
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    // Fetch dependencies
    config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

    // Use directly in logic
    width := config.GameWidth
    height := config.GameHeight
    now := timeRes.GameTime

    // ... system logic using resources ...
}

// ❌ INCORRECT: Accessing via GameContext (legacy pattern)
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    width := s.ctx.GameWidth   // DEPRECATED - use ConfigResource instead
    now := s.ctx.TimeProvider.Now()  // DEPRECATED - use TimeResource instead
}
```

**Resource Update Cycle:**
Resources are updated at the start of each frame/tick by the main game loop and ClockScheduler:

1. **Frame Start** (main.go): Update `TimeResource` with current frame time
2. **Input Processing**: Update `InputResource` with current mode and command text
3. **System Updates**: Systems read from Resources via `MustGetResource`
4. **Tick Updates** (ClockScheduler): Update `TimeResource` for scheduled systems

**Migration Status:**
The Resource System is being gradually adopted across all systems (Phase 2.5):
- ✅ **Migrated**: DecaySystem, CleanerSystem, DrainSystem, SpawnSystem, NuggetSystem, ScoreSystem, BoostSystem, GoldSystem
- 🔄 **In Progress**: (All core systems migrated as of Phase 2.5)
- **Legacy Fields**: `GameContext.GameWidth`, `GameContext.GameHeight`, `GameContext.TimeProvider` remain for backward compatibility but are deprecated

**Thread Safety:**
- **ResourceStore**: Protected by internal `sync.RWMutex`
- **Concurrent Reads**: Multiple systems can read resources simultaneously
- **Write Coordination**: Only main loop/scheduler updates resources (single writer pattern)

**Data Flow:**
```
┌─────────────────────────────────────────────────────────────┐
│ Main Game Loop (cmd/vi-fighter/main.go)                    │
│                                                             │
│ 1. Frame Start                                              │
│    └─> Update TimeResource (GameTime, RealTime, DeltaTime) │
│    └─> Update InputResource (GameMode, IsPaused)           │
│                                                             │
│ 2. System Updates (via World.Update())                     │
│    └─> SpawnSystem.Update()                                │
│        ├─> config := MustGetResource[*ConfigResource]()    │
│        ├─> timeRes := MustGetResource[*TimeResource]()     │
│        └─> Use config.GameWidth, timeRes.GameTime          │
│    └─> ScoreSystem.Update()                                │
│        ├─> config := MustGetResource[*ConfigResource]()    │
│        └─> Access ctx.State for Heat/Score                 │
│    └─> [Other Systems...]                                  │
│                                                             │
│ 3. Clock Scheduler (50ms tick, separate goroutine)         │
│    └─> Update TimeResource for scheduled systems           │
│    └─> GoldSystem, DecaySystem, CleanerSystem updates      │
│                                                             │
│ 4. Render                                                   │
│    └─> Read Resources via snapshots (frame-coherent)       │
└─────────────────────────────────────────────────────────────┘
```

Systems **read** from Resources, never write to them. All Resource updates happen in the main loop or ClockScheduler before system updates run, ensuring frame-coherent data access.

### Event-Driven Communication
Systems communicate through `GameContext.EventQueue`, a lock-free ring buffer that decouples producers and consumers.

**Core Principles:**
- **Decoupling**: Systems push events instead of calling methods on other systems
- **Lock-Free**: Ring buffer with atomic CAS operations (no mutexes)
- **Single Consumer**: Game loop consumes events each frame
- **Frame Deduplication**: Events include frame number to prevent duplicate processing

**Event Types**:
- `EventCleanerRequest`: Trigger cleaner spawn (gold completed at max heat)
- `EventCleanerFinished`: Cleaner animation completed (testing/debugging)
- `EventGoldSpawned`: Gold sequence created (testing/debugging)
- `EventGoldComplete`: Gold sequence typed successfully (testing/debugging)

**Producer Pattern**:
```go
// ScoreSystem pushes event when gold completed at max heat
if heatAtMaxBeforeGoldComplete {
    ctx.PushEvent(engine.EventCleanerRequest, nil)
}
```

**Consumer Pattern**:
```go
// CleanerSystem polls events each frame
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
    events := cs.ctx.ConsumeEvents()
    for _, event := range events {
        if event.Type == engine.EventCleanerRequest {
            if !cs.spawned[event.Frame] {  // Frame deduplication
                cs.spawnCleaners(world)
                cs.spawned[event.Frame] = true
            }
        }
    }
    // ... rest of update logic
}
```

**Implementation** (`engine/events.go`):
- `EventQueue`: Fixed-size ring buffer (size defined in `constants.EventQueueSize`)
- `Push()`: Lock-free CAS (Compare-And-Swap) for concurrent producers using fast bitwise masking (`constants.EventBufferMask`)
- `Consume()`: Atomically claims and returns all pending events
- `Peek()`: Read-only inspection for debugging/testing

### State Ownership Model

**GameState** (`engine/game_state.go`) centralizes game state with clear ownership boundaries:

#### Real-Time State (Lock-Free Atomics)
Updated immediately on user input/spawn events, read by all systems:
- **Heat** (`atomic.Int64`): Current heat value
- **Score** (`atomic.Int64`): Player score
- **Cursor Position**: NOW IN ECS as cursor entity with PositionComponent and CursorComponent
  - Legacy atomics (CursorX, CursorY) still exist in GameState for backward compatibility
  - GameContext has non-atomic cache fields (CursorX, CursorY) synced with ECS
- **Color Tracking**: NOW CENSUS-BASED via per-frame O(n) entity iteration
  - Legacy atomic counters removed from GameState (eliminating drift)
- **Boost State** (`atomic.Bool`, `atomic.Int64`): Enabled, EndTime, Color
- **Visual Feedback**: CursorError (via CursorComponent.ErrorFlashEnd), ScoreBlink, PingGrid (atomic)
- **Drain State** (`atomic.Bool`, `atomic.Uint64`, `atomic.Int32`): Active, EntityID, X, Y
- **Sequence ID** (`atomic.Int64`): Thread-safe ID generation

Atomics are used for high-frequency access (every frame and keystroke) to avoid lock contention while ensuring immediate consistency and race-free updates.

#### Clock-Tick State (Mutex Protected)
Updated during scheduled game logic ticks, read by all systems:
- **Spawn Timing** (`sync.RWMutex`): LastTime, NextTime, RateMultiplier
- **Screen Density**: EntityCount, ScreenDensity, SpawnEnabled
- **6-Color Limit**: NOW ENFORCED VIA CENSUS (per-frame entity iteration, no atomic drift)
- **Game Phase State**: CurrentPhase, PhaseStartTime
- **Gold Sequence State**: GoldActive, GoldSequenceID, GoldStartTime, GoldTimeoutTime
- **Decay Timer State**: DecayTimerActive, DecayNextTime
- **Decay Animation State**: DecayAnimating, DecayStartTime

Mutexes protect infrequently-changed state that requires consistent multi-field reads and atomic state transitions (spawn timing, phase changes). Blocking is acceptable since these are not on the hot path.

#### State Access Patterns

**Phase 2: Resource-Based Access (Current Pattern):**
Systems fetch global data (Time, Config, Input) from `World.Resources`, and game state from `GameContext.State`:

```go
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    // ✅ PHASE 2: Fetch resources at start of Update()
    config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

    // Access dimensions and time from resources
    width := config.GameWidth
    height := config.GameHeight
    now := timeRes.GameTime

    // Access game state through GameContext (real-time state)
    s.ctx.State.AddHeat(1)                       // Atomic increment
    snapshot := s.ctx.State.ReadSpawnState()     // Mutex-protected snapshot

    // Access event queue through GameContext
    s.ctx.PushEvent(engine.EventCleanerRequest, nil)

    // Access audio engine through GameContext
    s.ctx.AudioEngine.PlaySound(audio.SoundTypeError)
}
```

**Legacy Pattern (Deprecated - Phase 1):**
```go
// ❌ DEPRECATED: Accessing dimensions/time via GameContext
width := s.ctx.GameWidth              // Use ConfigResource instead
height := s.ctx.GameHeight            // Use ConfigResource instead
now := s.ctx.TimeProvider.Now()       // Use TimeResource instead
```

**GameContext Role (Post-Migration):**
- **Primary Purpose**: OS/Window management, Event routing, State orchestration
- **Retained Responsibilities**:
  - `GameState` access via `ctx.State` (Heat, Score, Boost, Phase, etc.)
  - `EventQueue` access via `ctx.PushEvent()` / `ctx.ConsumeEvents()`
  - `AudioEngine` access via `ctx.AudioEngine.PlaySound()`
  - Input handling methods (mode transitions, motion commands)
  - `World` reference (ECS access)
- **Deprecated Fields** (Use Resources instead):
  - ~~`ctx.GameWidth`~~ → Use `ConfigResource.GameWidth`
  - ~~`ctx.GameHeight`~~ → Use `ConfigResource.GameHeight`
  - ~~`ctx.TimeProvider.Now()`~~ → Use `TimeResource.GameTime`
- **Cursor Position Cache**: `CursorX`, `CursorY` fields cache ECS position for motion handlers
  - MUST be synced FROM ECS before use: `pos, _ := ctx.World.Positions.Get(ctx.CursorEntity); ctx.CursorX = pos.X`
  - MUST be synced TO ECS after modification: `ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY})`

#### Snapshot Pattern

The **snapshot pattern** is the primary mechanism for safely reading multi-field state across concurrent goroutines. All mutex-protected state is accessed through immutable snapshot structures that guarantee internally consistent views.

**Core Principles:**
1. **Immutability**: Snapshots are value copies, never references
2. **Atomicity**: All fields in a snapshot come from the same moment in time
3. **No Partial Reads**: Readers never see half-updated state
4. **Lock-Free After Capture**: Once a snapshot is taken, no locks are held

**Available Snapshot Types:**

All systems use snapshots to read GameState. The following snapshot types are available:

```go
// Spawn State (mutex-protected)
type SpawnStateSnapshot struct {
    LastTime       time.Time
    NextTime       time.Time
    RateMultiplier float64
    Enabled        bool
    EntityCount    int
    MaxEntities    int
    ScreenDensity  float64
}
snapshot := ctx.State.ReadSpawnState() // Used by: SpawnSystem, Renderer

// Color Counter State (atomic fields)
type ColorCountSnapshot struct {
    BlueBright  int64
    BlueNormal  int64
    BlueDark    int64
    GreenBright int64
    GreenNormal int64
    GreenDark   int64
}
snapshot := ctx.State.ReadColorCounts() // Used by: SpawnSystem, DecaySystem, Renderer

// Cursor Position (ECS-based, legacy atomics deprecated)
// PRIMARY SOURCE: ctx.World.Positions.Get(ctx.CursorEntity)
// DEPRECATED: GameState.CursorX/Y atomics (kept for backward compatibility)
type CursorSnapshot struct {
    X int
    Y int
}
// PREFERRED: Read directly from ECS
pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
// LEGACY: snapshot := ctx.State.ReadCursorPosition() // Used by: Renderer (legacy)

// Boost State (atomic fields)
type BoostSnapshot struct {
    Enabled   bool
    EndTime   time.Time
    Color     int32
    Remaining time.Duration
}
snapshot := ctx.State.ReadBoostState() // Used by: ScoreSystem, Renderer

// Phase State (mutex-protected)
type PhaseSnapshot struct {
    Phase     GamePhase
    StartTime time.Time
    Duration  time.Duration
}
snapshot := ctx.State.ReadPhaseState() // Used by: ClockScheduler, all game systems

// Gold Sequence State (mutex-protected)
type GoldSnapshot struct {
    Active      bool
    SequenceID  int
    StartTime   time.Time
    TimeoutTime time.Time
    Elapsed     time.Duration
    Remaining   time.Duration
}
snapshot := ctx.State.ReadGoldState() // Used by: GoldSequenceSystem, ScoreSystem, Renderer

// Decay State (mutex-protected)
type DecaySnapshot struct {
    TimerActive bool
    NextTime    time.Time
    Animating   bool
    StartTime   time.Time
    TimeUntil   float64
}
snapshot := ctx.State.ReadDecayState() // Used by: DecaySystem, Renderer

// Atomic pairs for related fields
heat, score := ctx.State.ReadHeatAndScore() // Used by: ScoreSystem, Renderer
```

**Usage Examples:**

**Correct: Snapshot Pattern**
```go
// ✅ GOOD: Read once, use multiple times
snapshot := ctx.State.ReadSpawnState()
if snapshot.Enabled && snapshot.EntityCount < snapshot.MaxEntities {
    density := snapshot.ScreenDensity
    rate := snapshot.RateMultiplier
    // All fields guaranteed consistent
}
```

**Incorrect: Multiple Individual Reads**
```go
// ❌ BAD: Race condition - fields may change between reads
if ctx.State.ShouldSpawn() {                    // RLock #1
    count := ctx.State.ReadSpawnState().EntityCount  // RLock #2
    density := ctx.State.ReadSpawnState().ScreenDensity  // RLock #3
    // EntityCount and ScreenDensity may be from different updates!
}
```

**Correct: Atomic Snapshots**
```go
// ✅ GOOD: Atomic fields read together
heat, score := ctx.State.ReadHeatAndScore()
if heat > 0 && score > 0 {
    // heat and score are consistent
}
```

**Incorrect: Separate Atomic Reads**
```go
// ❌ BAD: heat and score may be from different moments
heat := ctx.State.GetHeat()   // Atomic read #1
score := ctx.State.GetScore() // Atomic read #2
// If another goroutine updates both, we might see heat=new, score=old
```

**System Usage Map:**

| System | Snapshot Types Used | Purpose |
|--------|-------------------|---------|
| SpawnSystem | SpawnState, Census (entity iteration), ECS Cursor | Check spawn conditions, cursor exclusion zone |
| ScoreSystem | BoostState, GoldState, HeatAndScore | Process typing, update heat/score |
| GoldSequenceSystem | GoldState, PhaseState | Manage gold sequence lifecycle |
| DecaySystem | DecayState, PhaseState | Manage decay timer and animation |
| CleanerSystem | EventQueue | Event-driven cleaner lifecycle via EventCleanerRequest/Finished |
| Renderer | All snapshot types | Render game state without blocking game loop |
| ClockScheduler | PhaseState, GoldState, DecayState | Manage phase transitions |

**Concurrency Guarantees:**

1. **Mutex-Protected Snapshots** (SpawnState, PhaseState, GoldState, DecayState):
   - Use `RLock` to read state atomically
   - All fields copied before returning
   - Multiple concurrent readers allowed
   - Writers block only during actual state modification

2. **Atomic Field Snapshots** (BoostState, HeatAndScore):
   - No locks required
   - Multiple atomic loads in sequence
   - Still provides consistent view (atomic loads are sequentially consistent)
   - Trade-off: Very rare possibility of seeing mixed state between loads (acceptable for these use cases)

3. **ECS-Based State** (Cursor Position, Color Tracking):
   - Cursor position: Read via `ctx.World.Positions.Get(ctx.CursorEntity)`
   - Color tracking: Per-frame census via entity iteration (no atomic drift)
   - Thread-safe via PositionStore's internal RWMutex

4. **Immutability After Capture:**
   - All snapshots are value types (structs)
   - Modifying snapshot fields doesn't affect GameState
   - Safe to pass snapshots across goroutine boundaries
   - No memory aliasing issues

### Clock Scheduler and Time Management

**Architecture**: Dual-clock system with frame/game synchronization:
- **Frame Clock** (16ms, ~60 FPS): Rendering, UI updates (cursor blink), input handling
- **Game Clock** (50ms): Game logic via ClockScheduler - phase transitions, system updates

**Purpose**: Separates visual updates (frame) from game logic (game) for deterministic gameplay and smooth rendering. The ClockScheduler centralizes phase transitions on a predictable 50ms tick, preventing race conditions in inter-dependent mechanics (Gold→Decay→Cleaner flow).

#### PausableClock - Game Time vs Real Time

**Dual Time System**:
- **Game Time**: Pausable clock used for all game logic and game state feedback
  - Used for: spawning, decay, gold timeouts, score blink, cursor error flash
  - Stops advancing when paused (COMMAND mode) - visual feedback freezes during pause
  - Accessed via `ctx.TimeProvider.Now()` (returns pausable time)
- **Real Time**: Wall clock time for purely visual UI elements
  - Currently not used (reserved for future UI animations like "PAUSED" text blinker)
  - Would continue during pause if implemented
  - Accessed via `ctx.GetRealTime()` (returns wall clock)

**Pause Mechanism** (`engine/pausable_clock.go`):
```go
// Entering COMMAND mode
ctx.SetPaused(true)          // Sets IsPaused atomic flag
ctx.PausableClock.Pause()    // Stops game time advancement

// Game time calculation (when not paused)
gameTime = realTime - totalPausedTime

// When paused, Now() returns frozen time at pause point
```

**Resume with Drift Protection**:
When resuming from pause, the ClockScheduler adjusts its next tick deadline to maintain the 50ms rhythm:
```go
// On resume, ClockScheduler.HandlePauseResume() is called
func (cs *ClockScheduler) HandlePauseResume(pauseDuration time.Duration) {
    // Shift deadline forward by pause duration
    cs.nextTickDeadline = cs.nextTickDeadline.Add(pauseDuration)
}
```

This ensures no clock drift accumulates from pausing/resuming.

**Frame/Game Synchronization**:
The main loop coordinates frame rendering with game updates using channels:
- `frameReady` channel: Main loop signals when frame is ready for next update
- `updateDone` channel: ClockScheduler signals when game update is complete
- Rendering waits for game update to complete before drawing (prevents tearing)
- If update takes too long (>2 tick intervals), scheduler catches up to prevent spiral

#### GamePhase State Machine

**Phase Enum** (`engine/clock_scheduler.go`):
```go
type GamePhase int

const (
    PhaseNormal         // Regular gameplay, content spawning
    PhaseGoldActive     // Gold sequence active with timeout tracking
    PhaseGoldComplete   // Gold completed, ready for next phase (transient)
    PhaseDecayWait      // Waiting for decay timer (heat-based interval)
    PhaseDecayAnimation // Decay animation running (falling entities)
)
```

**Phase State** (in `GameState`):
- `CurrentPhase` (`GamePhase`): Current game phase (mutex protected)
- `PhaseStartTime` (`time.Time`): When current phase started
- **Gold state**: `GoldActive`, `GoldSequenceID`, `GoldStartTime`, `GoldTimeoutTime`
- **Decay timer state**: `DecayTimerActive`, `DecayNextTime`
- **Decay animation state**: `DecayAnimating`, `DecayStartTime`

**Note**: Cleaners are triggered via `EventCleanerRequest` and run in parallel with the main phase cycle (non-blocking, event-driven).

**Phase Access Pattern**:
```go
// Read current phase (thread-safe)
phase := ctx.State.GetPhase()

// Transition to new phase (validated, resets start time)
success := ctx.State.TransitionPhase(PhaseGoldActive)

// Get phase duration
duration := ctx.State.GetPhaseDuration()

// Consistent snapshot
snapshot := ctx.State.ReadPhaseState()
```

#### ClockScheduler (`engine/clock_scheduler.go`)

**Infrastructure**:
- 50ms tick interval running in dedicated goroutine
- Adaptive sleep that respects pause state (sleeps longer during pause to avoid busy-wait)
- Frame synchronization via channels (`frameReady`, `updateDone`)
- Thread-safe start/stop with idempotent Stop()
- Tick counter for debugging and metrics
- Graceful shutdown on game exit
- Pause resume callback registration for drift correction

**Behavior**:
- Ticks every 50ms (game time) when not paused
- Skips all game logic updates when paused (defensive check in processTick)
- Waits for frame ready signal before processing tick (prevents update/render conflicts)
- **Phase transitions handled on clock tick** (via `processTick()`):
  - `PhaseGoldActive`: Check gold timeout (pausable clock) → timeout via GoldSystem
  - `PhaseGoldComplete`: Start decay timer
  - `PhaseDecayWait`: Check decay ready (pausable clock) → start decay animation
  - `PhaseDecayAnimation`: Handled by DecaySystem → returns to PhaseNormal when complete
  - `PhaseNormal`: Gold spawning handled by GoldSystem's Update() method
- **Cleaner Animation**: Triggered via `EventCleanerRequest` (runs in parallel with main phase cycle)
- **Drift Protection**:
  - Advances deadline by exactly one interval (prevents cumulative drift)
  - If severely behind (>2 intervals), resets to current time + interval
  - On pause resume, adjusts deadline by pause duration
- **Critical**: All timers use pausable clock (game time), so they freeze during pause

**Integration** (`cmd/vi-fighter/main.go`):
```go
// Constants for dual-clock system
const (
    frameUpdateDT = 16 * time.Millisecond // ~60 FPS (frame rate for rendering)
    gameUpdateDT  = 50 * time.Millisecond // game logic tick
)

// Create frame synchronization channel
frameReady := make(chan struct{}, 1)

// Create clock scheduler with frame synchronization
clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, gameUpdateDT, frameReady)

// Signal initial frame ready
frameReady <- struct{}{}

clockScheduler.SetSystems(goldSystem, decaySystem, cleanerSystem)
clockScheduler.Start()
defer clockScheduler.Stop()

// Main game loop with frame ticker
frameTicker := time.NewTicker(frameUpdateDT)
defer frameTicker.Stop()

for {
    select {
    case <-frameTicker.C:
        // Always update UI elements (use real time, works during pause)
        updateUIElements(ctx)

        // During pause: skip game updates but still render
        if ctx.IsPaused.Load() {
            renderer.RenderFrame(...)
            continue
        }

        // Wait for update complete, then render
        <-gameUpdateDone
        renderer.RenderFrame(...)

        // Signal ready for next update
        frameReady <- struct{}{}
    }
}
```

#### Architecture Benefits

**Separation of Concerns**:
- **Frame layer** (16ms): Rendering, UI updates (cursor blink), input handling - always responsive
- **Game logic layer** (50ms): Phase transitions, system updates, spawn decisions - deterministic and pausable
- **UI elements use real time**: Cursor continues blinking during pause for visual feedback
- **Game logic uses game time**: All timers (decay, gold timeout) freeze during pause

**Pause-Aware Design**:
- **COMMAND mode pauses game**: Entering `:` stops game time but keeps UI active
- **No drift accumulation**: Resume callback adjusts scheduler deadline by pause duration
- **Visual feedback during pause**: Characters dimmed to 70% brightness, cursor still blinks
- **Frame updates continue**: Rendering still happens during pause for visual feedback

**Race Condition Prevention**:
- Phase transitions happen atomically on clock tick
- Frame synchronization prevents update/render conflicts (channels coordinate timing)
- Heat snapshots taken at specific moments (not cached)
- State ownership model eliminates conflicting writes
- Adaptive sleep during pause avoids busy-wait

**Testability**:
- Clock tick is deterministic with `MockTimeProvider`
- Pause/resume can be tested independently
- Phase transitions can be unit tested independently
- Integration tests can advance time precisely
- Frame/game sync can be verified with channels

**Performance**:
- Frame updates remain responsive (60 FPS independent of game logic)
- Game logic only runs 3× per frame (50ms vs 16ms) - reduced CPU load
- Mutex contention minimized (scheduler thread vs main thread)
- Adaptive sleep during pause reduces CPU usage to near-zero
- Frame/game synchronization prevents wasted rendering (no tearing)

## Audio System

### Overview
The audio system provides sound effects for game events using a dual-queue architecture with thread-safe playback management.

### Components
- **AudioEngine**: Core playback engine with two priority queues
  - `realTimeQueue` (size 5): High-priority sounds like typing errors
  - `stateQueue` (size 10): Game state sounds (coins, bells, whooshes)
- **Sound Effects**: Synthesized using beep library
  - Error: Short harsh buzz (100Hz saw wave)
  - Bell: Melodic ding for nuggets (880Hz + 1760Hz sine)
  - Whoosh: Noise burst for cleaners
  - Coin: Two-note chime for gold completion

### Integration Points
- **ScoreSystem**: Sends error sounds via realTimeQueue
- **GoldSystem**: Sends coin sounds via stateQueue
- **NuggetSystem**: Sends bell sounds via stateQueue (Tab jump)
- **CleanerSystem**: Sends whoosh sounds via stateQueue

### Pause Behavior
- Entering COMMAND mode immediately stops current sound
- Queues are drained to prevent delayed playback
- No resume on unpause - sounds don't continue

### Controls
- **Ctrl+S**: Toggle mute (starts muted by default)
- Status bar shows [MUTED] indicator when muted

### Thread Safety
- Atomic operations for state flags (running, muted, initialized)
- Mutex protection for playback state and config access
- Non-blocking queue sends with overflow handling
- Generation counter prevents stale command playback

### System Priorities
Systems execute in priority order (lower = earlier):
1. **BoostSystem (5)**: Handle boost timer expiration (highest priority)
2. **ScoreSystem (10)**: Process user input, update score
3. **SpawnSystem (15)**: Generate new sequences (Blue and Green only)
4. **NuggetSystem (18)**: Manage collectible nugget spawning and collection
5. **GoldSystem (20)**: Manage gold sequence lifecycle and random placement
6. **CleanerSystem (22)**: Process cleaner physics, collision, and visual effects
7. **DrainSystem (25)**: Manage score-draining entity movement and logic
8. **DecaySystem (30)**: Apply sequence degradation and color transitions (lowest priority)

**Important**: All priorities must be unique to ensure deterministic execution order. The priority values define the exact order in which systems process game state each frame.

### Spatial Indexing
- Primary index: Encapsulated within `PositionStore` (Internal `spatialIndex`)
- Access: `world.Positions.GetEntityAt(x, y)`
- Updates: `world.Positions.Move(...)` or `world.Positions.Add(...)`
- Batching: `world.Positions.BeginBatch()` for atomic multi-entity spawning
- **Limitation**: Single entity per cell - only one entity can occupy a given (x, y) position
  - When Cursor Entity is at a position, it effectively "masks" other entities in the spatial index
  - Systems requiring collision detection with masked entities must use Query Pattern instead of spatial lookups

## Component Hierarchy
```
Component (marker interface)
├── PositionComponent {X, Y}
├── CharacterComponent {Rune, Style}
├── SequenceComponent {ID, Index, Type, Level}
├── GoldSequenceComponent {Active, SequenceID, StartTimeNano, CharSequence, CurrentIndex}
├── FallingDecayComponent {Column, YPosition, Speed, Char, LastChangeRow, LastIntX, LastIntY, PrevPreciseX, PrevPreciseY}
├── CleanerComponent {PreciseX, PreciseY, VelocityX, VelocityY, TargetX, TargetY, GridX, GridY, Trail, Char}
├── RemovalFlashComponent {X, Y, Char, StartTime, Duration}
├── NuggetComponent {ID, SpawnTime}
└── DrainComponent {X, Y, LastMoveTime, LastDrainTime, IsOnCursor}
```

**Note**: GoldSequenceComponent is the full type name used in code (defined in `components/character.go`).

### Sequence Types
- **Green**: Positive scoring, spawned by SpawnSystem, decays Blue→Green→Red
- **Blue**: Positive scoring, spawned by SpawnSystem, decays to Green when Dark level reached
- **Red**: Negative scoring (penalty), ONLY created through decay (not spawned directly)
- **Gold**: Bonus sequence (10 characters), spawned by GoldSystem after decay animation completes

## Rendering Pipeline

1. Clear dirty regions (when implemented)
2. Draw static UI (heat meter, line numbers)
3. Draw game entities (characters)
   - Apply pause dimming effect when `ctx.IsPaused.Load()` is true (70% brightness)
4. Draw overlays (ping, decay animation)
5. Draw cleaners with gradient trail effects
6. Draw removal flash effects
7. Draw cursor (topmost layer)

### Pause State Visual Feedback

When the game is paused by entering COMMAND mode (`:` key), the rendering system provides visual feedback:

- **Trigger**: `ctx.IsPaused.Load()` returns true (set when entering COMMAND mode)
- **Game Time**: Stops advancing - all timers (decay, gold timeout, boost) freeze
- **UI Time**: Continues - cursor still blinks using real time for visual feedback
- **Visual Dimming**: All character foreground colors are multiplied by 0.7 (70% brightness)
- **Purpose**: Clearly indicates paused state while preserving game visibility
- **Implementation**: Applied in `drawCharacters()` after ping highlighting, before final rendering
- **Frame Updates**: Continue during pause to show dimmed characters and cursor blink

## System Coordination and Event Flow

### Complete Game Cycle
The game operates in a continuous cycle managed by the phase system:

```
PhaseNormal → PhaseGoldActive → PhaseGoldComplete → PhaseDecayWait → PhaseDecayAnimation → PhaseNormal
    ↑                                                                         ↓
    └─────────────────────────────────────────────────────────────────────────┘

Parallel (Event-Driven):
  EventCleanerRequest → Cleaner Spawn → Cleaner Animation → EventCleanerFinished
  (triggered when gold completed at max heat, runs independently of phase cycle)
```

**Key State Transitions:**
- Gold spawns after decay animation completes → PhaseGoldActive
- Gold completion/timeout → PhaseGoldComplete → PhaseDecayWait (starts decay timer)
- Decay timer expires → PhaseDecayAnimation (falling entities decay characters)
- Decay animation completes → PhaseNormal (ready for next gold)
- Cleaners run in parallel via event system, do NOT affect phase transitions

### Event Sequencing

#### 1. Gold Phase
- **Activation**: Gold spawns after decay animation completes
- **Duration**: 10 seconds (constants.GoldSequenceDuration)
- **Completion**: Either typed correctly or times out
- **Next Action**: Transitions to `PhaseGoldComplete`, then starts decay timer

**State Validation**:
- Gold can only spawn when NOT active
- Gold entities have unique sequence IDs
- Position conflicts with existing entities are avoided

#### 2. Decay Timer Phase
- **Activation**: Started when Gold ends (completion or timeout)
- **Duration**: 60-10 seconds (based on heat percentage at Gold end time)
  - Formula: `60s - (50s * (CurrentHeat / MaxHeat))`
  - Higher heat = faster decay
- **Purpose**: Creates breathing room between gold spawns
- **Next Action**: Triggers decay animation when timer expires

**State Validation**:
- Timer only starts if not already running
- Timer calculation is atomic (based on heat at specific moment)
- Timer does not restart during active animation
- Timer uses pausable clock (freezes during COMMAND mode)

#### 3. Decay Animation Phase
- **Activation**: Triggered when decay timer expires
- **Duration**: Based on falling entity speed (4.8-1.6 seconds)
  - Slowest entity: 24 rows / 5.0 rows/sec = 4.8s
  - Fastest entity: 24 rows / 15.0 rows/sec = 1.6s
- **Effects**:
  - Spawns falling entities (one per column)
  - Entities decay characters they pass over
  - Characters decay one level: Bright → Normal → Dark
  - Dark level triggers color change: Blue→Green, Green→Red
- **Next Action**: Returns to PhaseNormal (Gold can spawn again)

**State Validation**:
- Animation cannot start if already animating
- Each character decayed at most once per animation
- Falling entities properly cleaned up on completion
- Animation uses pausable clock (freezes during COMMAND mode)

### Score System Integration

#### Gold Typing
When user types during active gold:

```
ScoreSystem.handleGoldSequenceTyping():
  1. Verify character matches expected gold character
  2. If incorrect: Flash error, DO NOT reset heat
  3. If correct:
     - Destroy character entity
     - Move cursor right
  4. If last character:
     - Check if heat is at maximum
     - If yes: Trigger cleaners immediately
     - Fill heat to maximum (if not already)
     - Mark gold sequence as complete
```

**Key Behavior**:
- Gold typing NEVER resets heat (unlike incorrect regular typing)
- Cleaners trigger BEFORE heat is filled (to check pre-fill state)
- Heat is guaranteed to be at max after gold completion
- Gold timeout uses pausable clock (10 seconds of game time, not real time)

### Concurrency Guarantees

#### Mutex Protection (DecaySystem)
All DecaySystem state is protected by `sync.RWMutex`:
- `animating`: Animation active state
- `timerStarted`: Whether decay timer has been initialized
- `fallingEntities`: List of active falling entities
- `decayedThisFrame`: Map tracking which entities were decayed
- `startTime`, `nextDecayTime`: Timing information
- `gameWidth`, `gameHeight`: Dimension information

**Lock Patterns**:
- RLock for reads: Allows concurrent readers
- Lock for writes: Exclusive access for modifications
- Locks released before calling into other systems (prevents deadlock)

#### Atomic Operations (CleanerSystem)
CleanerSystem uses minimal atomic state:
- `pendingSpawn`: Activation signal flag (atomic.Bool)
  - Set by `ActivateCleaners()` from any thread
  - Cleared by `Update()` in main loop via CAS operation
  - Ensures spawn happens exactly once per activation

**Benefits**:
- Single atomic flag eliminates complex state tracking
- Lock-free activation from any thread (e.g., ScoreSystem)
- All other state managed through ECS components
- Component queries handled by World's internal synchronization

#### Gold System Synchronization
GoldSequenceSystem uses `sync.RWMutex` for all state:
- `active`: Gold sequence active state
- `sequenceID`: Current sequence identifier
- `startTime`: When sequence was spawned
- Cleaner trigger function reference

### State Transition Rules

#### Phase Transitions (Main Game Cycle)
- **PhaseNormal** → **PhaseGoldActive**: When gold sequence spawns
- **PhaseGoldActive** → **PhaseGoldComplete**: When gold typed or timeout
- **PhaseGoldComplete** → **PhaseDecayWait**: Starts decay timer
- **PhaseDecayWait** → **PhaseDecayAnimation**: When decay timer expires
- **PhaseDecayAnimation** → **PhaseNormal**: When falling animation completes

#### Cleaner Event Flow (Parallel to Phase Cycle)
- **EventCleanerRequest** pushed when: Gold completed at max heat
- **CleanerSystem** consumes event → spawns cleaner entities (or phantom if no Red characters)
- **Cleaner animation** runs: Entities move across screen, destroy Red characters
- **EventCleanerFinished** pushed when: All cleaner entities destroyed
- **No phase transitions**: Cleaners run independently, do not block or modify phase state

#### Invalid Transitions (Prevented)
- Gold spawning while Gold already active → Ignored
- Decay animation starting while already animating → Ignored
- Decay timer restarting during active animation → Blocked
- Phase transitions during cleaner animation → **Allowed** (cleaners are non-blocking)

#### Valid Transitions
- Gold End → Decay Timer Start: Always allowed (independent of cleaners)
- Decay Timer Expire → Animation Start: Atomic transition (independent of cleaners)
- Animation Complete → Gold Spawn: Automatic, immediate (independent of cleaners)
- Cleaner Request → Spawn: Event-driven, any time

### Debugging Support

All major systems provide `GetSystemState()` for debugging:

```go
decaySystem.GetSystemState()
// Returns: "Decay[animating=true, elapsed=2.30s, fallingEntities=80]"
// or: "Decay[timer=active, timeUntil=45.20s, nextDecay=...]"
// or: "Decay[inactive]"

goldSystem.GetSystemState()
// Returns: "Gold[active=true, sequenceID=123, timeRemaining=7.50s]"
// or: "Gold[inactive]"

cleanerSystem.GetSystemState()
// Returns: "Cleaner[active=true, count=5, elapsed=1.20s]"
// or: "Cleaner[inactive]"
```

**Usage**: Call during test failures or production debugging to understand system state.

## Input State Machine
```
NORMAL ─[i]→ INSERT
NORMAL ─[/]→ SEARCH
INSERT / SEARCH ─[ESC]→ NORMAL
NORMAL -[:]→ COMMAND (game paused) -[ESC/ENTER]→ NORMAL
```

### Commands

**`:new` - New Game**
- **Behavior**: Clears the ECS World and resets game state for a fresh game
- **Critical Requirement**: Cursor Entity Restoration
  - `World.Clear()` is destructive and removes ALL entities including the Cursor Entity
  - After clear, the Cursor Entity's components must be explicitly restored:
    - `CursorComponent`: Tracks cursor-specific state (error flash timing, etc.)
    - `ProtectionComponent`: Marks cursor as indestructible (Mask: `ProtectAll`)
    - `PositionComponent`: Sets initial cursor position (typically 0, 0)
  - **Failure to restore causes "Zombie Cursor"**: Rendering system expects cursor entity to exist
  - **Implementation Pattern**:
    ```go
    // Clear world (destroys all entities including cursor)
    world.Clear()

    // MUST restore cursor entity with all required components
    cursorEntity := world.NewEntity()
    world.Positions.Add(cursorEntity, components.PositionComponent{X: 0, Y: 0})
    world.Cursors.Add(cursorEntity, components.CursorComponent{})
    world.Protections.Add(cursorEntity, components.ProtectionComponent{
        Mask: components.ProtectAll,
    })

    // Update context reference
    ctx.CursorEntity = cursorEntity
    ```

### Motion Commands (NORMAL Mode)
- **Single character**: Direct execution (h, j, k, l, w, b, e, etc.)
- **Prefix commands**: Build state (`g`, `d`, `f`, `F`) then wait for completion
  - `gg` - Jump to top
  - `go` - Jump to top-left corner
  - `dd` - Delete line
  - `dw`, `d$`, `d<motion>` - Delete with motion
  - `f<char>` - Find character forward on line (count-aware: `2fa` finds 2nd 'a')
  - `F<char>` - Find character backward on line (count-aware: `2Fa` finds 2nd 'a' backward)
- **Count prefix**: Accumulate digits until motion (e.g., `5j`, `10l`, `3w`, `2fa`, `3Fb`)
- **Count-aware multi-keystroke commands**: Commands like `f` and `F` preserve count through phases
  - `MotionCount` → `PendingCount` when entering multi-keystroke state
  - `PendingCount` used when completing the command
  - Both cleared after execution
- **CommandCapabilities system**: Systematic mapping of which commands accept counts
  - Flags: `AcceptsCount`, `MultiKeystroke`, `RequiresMotion`
  - See `modes/capabilities.go` for full mapping
- **Consecutive move penalty**: Using h/j/k/l more than 3 times consecutively resets heat
- **Arrow keys**: Function like h/j/k/l but always reset heat
- **Tab**: Jumps cursor directly to active Nugget (Cost: 10 Score, requires Score >= 10)

### Supported Vi Motions
**Basic**: h, j, k, l, Space (as l)
**Line**: 0 (start), ^ (first non-space), $ (end)
**Word**: w, b, e (word), W, B, E (WORD)
**Screen**: gg (top), G (bottom), go (top-left), H (high), M (middle), L (low)
**Paragraph**: { (prev empty), } (next empty), % (matching bracket - supports (), {}, [], <>)
**Find/Till**: f<char> (find forward), F<char> (find backward), t<char> (till forward), T<char> (till backward), ; (repeat find/till), , (reverse find/till)
**Search**: / (search), n/N (next/prev match)
**Delete**: x (char), dd (line), d<motion> (delete with motion), D (to end of line)

## Concurrency Model

### Main Architecture
- **Main game loop**: Single-threaded ECS updates (16ms frame tick)
- **Input events**: Goroutine → channel → main loop
- **Clock scheduler**: Separate goroutine for phase transitions (50ms tick)
- **All systems**: Run synchronously in main game loop, no autonomous goroutines

### Shared State Synchronization
- **Color Counters**: `atomic.Int64` for lock-free updates
  - `SpawnSystem`: Increments counters when blocks placed
  - `ScoreSystem`: Decrements counters when characters typed
  - `DecaySystem`: Updates counters during decay transitions
  - All counter operations are race-free and thread-safe
- **GameState**: Uses `sync.RWMutex` for phase state and timing
- **World**: Thread-safe entity/component access (internal locking)

## Race Condition Prevention

### Design Principles
1. **Single-Threaded ECS**: All entity/component modifications in main game loop
2. **No Autonomous Goroutines**: Systems never spawn independent update loops
3. **Explicit Synchronization**: All cross-thread access uses atomics or mutexes
4. **Frame-Coherent Snapshots**: Renderer reads immutable snapshots, never live state
5. **Lock Granularity**: Minimize lock scope - protect data structures, not operations

### CleanerSystem Concurrency Model
The CleanerSystem uses a pure ECS pattern with careful synchronization:

- **Pure ECS Pattern**: All state in `CleanerComponent`, no external maps or tracking
- **Synchronous Updates**: Main loop `Update()` method with delta time integration
- **Event-Driven Activation**: `EventCleanerRequest` triggers spawning via event queue
- **Component-Based Physics**: Sub-pixel position, velocity, and trail stored in component
- **Snapshot Rendering**: Renderer queries World directly and deep-copies trail positions for thread safety
- **ECS Synchronization**: Leverages World's internal locking for component access
- **Zero State Duplication**: Component is the single source of truth

### Frame Coherence Strategy
**Rendering Thread Safety**:
1. Renderer queries World directly for all `CleanerComponent` entities each frame
2. Deep-copies trail slice for each cleaner to prevent data races with Update thread
3. Renderer uses trail copy (no shared references to component state)
4. Main loop updates components via ECS World's synchronized methods
5. No data races: trail copies are fully independent from component state

**Implementation**:
```go
// Renderer (terminal_renderer.go)
func (r *TerminalRenderer) drawCleaners(world *engine.World, defaultStyle tcell.Style) {
    // Direct store access
    entities := world.Cleaners.All()

    for _, entity := range entities {
        cleaner, ok := world.Cleaners.Get(entity)
        if !ok { continue }

        // Deep copy trail to avoid race conditions during rendering
        trailCopy := make([]core.Point, len(cleaner.Trail))
        copy(trailCopy, cleaner.Trail)

        // Render using trail copy...
    }
}

// CleanerSystem Update (cleaner_system.go)
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
    // Update component directly via World (internally synchronized)
    c.PreciseX += c.VelocityX * dt.Seconds()

    // Strict copy-on-write for Trail to prevent race conditions with Renderer
    newTrail := make([]core.Point, newLen)
    newTrail[0] = newPoint
    copy(newTrail[1:], c.Trail[:copyLen])
    c.Trail = newTrail  // Atomic reference replacement

    world.AddComponent(entity, c)  // ECS handles synchronization
}
```

## Performance Guidelines

### Hot Path Optimizations
1. Use generics-based queries for type-safe, zero-allocation component access
2. Use spatial index for position lookups via PositionStore
3. Batch similar operations (e.g., all destroys at end)
4. Reuse allocated slices where possible (e.g., trail slices grow/shrink in-place)
5. CleanerSystem updates synchronously with frame-accurate delta time
6. Pre-calculate rendering gradients once at initialization (zero per-frame color math)

### Memory Management
- Pool temporary slices (coordinate lists, entity batches)
- Clear references before destroying entities
- Limit total entity count (MAX_CHARACTERS = 200)

## Extension Points

### Adding New Components
1. Define data struct implementing `Component`
2. Register type in relevant systems
3. Update spatial index if position-related

### Adding New Systems
1. Implement `System` interface
2. Define `Priority()` for execution order
3. Register in `main.go` after context creation

### Adding New Visual Effects
1. Create component for effect data
2. Add rendering logic to `TerminalRenderer`
3. Ensure proper layer ordering

## Invariants to Maintain

1. **One Entity Per Position**: `spatialIndex[y][x]` holds at most one entity
2. **Component Consistency**: Entity with SequenceComponent MUST have Position and Character
3. **Cursor Bounds**: `0 <= CursorX < GameWidth && 0 <= CursorY < GameHeight`
4. **Heat Mechanics**: Heat is normalized to a fixed range (0-100). Max Heat is always 100
5. **Boost Mechanic**: When heat reaches maximum (100), boost activates with color-matching (Blue or Green) providing x2 heat multiplier. Typing the matching color extends boost duration by 500ms per character, while typing a different color deactivates boost
6. **Red Spawn Invariant**: Red sequences are NEVER spawned directly, only through decay
7. **Gold Randomness**: Gold sequences spawn at random positions
8. **6-Color Limit**: At most 6 Blue/Green color/level combinations present simultaneously
9. **Counter Accuracy**: Color counters must match actual on-screen character counts
10. **Atomic Operations**: All color counter updates use atomic operations for thread safety

## Known Constraints and Limitations

### PositionStore Single-Entity Limitation

The `PositionStore` spatial index enforces a **single entity per cell** constraint:

**Constraint**: `spatialIndex[y][x]` can only hold one entity at position (x, y)

**Implications**:
1.  **Cursor Masking**: The spatial index (GetEntityAt) returns the topmost entity. The Cursor masks characters.
   - **Systems must use Query()** to inspect characters under the cursor, never GetEntityAt()
   - Other entities at that position become "masked" and unreachable via `GetEntityAt(x, y)`
   - The masked entities still exist in the World and other component stores
   - Only the spatial index reference is overwritten

2. **Hit Detection Workaround**: Systems requiring collision detection at cursor position must use Query Pattern
   - **Spatial Lookup** (`GetEntityAt`): Fast O(1) but returns only the topmost entity (cursor)
   - **Query Pattern** (`Query().With(...).Execute()`): Slower O(n) but finds all entities including masked ones
   - See "Score System > Hit Detection Strategy" for implementation details

3. **Entity Spawning**: SpawnSystem must check spatial index before placement to avoid conflicts
   - Cursor exclusion zone prevents spawning near cursor (5 horizontal, 3 vertical)
   - Collision detection ensures characters don't spawn on occupied cells

4. **Cursor Entity Protection**: Cursor must have `ProtectionComponent` with `Mask: ProtectAll`
   - Prevents accidental destruction by systems that clean up entities
   - Critical after `World.Clear()` operations (see `:new` command requirements)

**Design Rationale**:
- Simplifies rendering (no z-ordering required for most entities)
- Efficient O(1) position lookups for spawning and movement
- Trade-off: Requires Query Pattern for collision detection at cursor position

## Game Mechanics Details

### Content Management System
- **ContentManager** (`content/content_manager.go`): Manages Go source file discovery and validation
- **Auto-discovery**: Scans project directory for `.go` files at initialization
- **Validation**: Pre-validates all content at startup for performance
- **Block Selection**: Random 100-500 line blocks, grouped by structure
- **Refresh Strategy**: Pre-fetches new content at 80% consumption threshold

### Spawn System
- **Content Source**: Loads Go source code from `assets/` directory at initialization (automatically located at project root)
- **Block Generation**:
  - Selects random 3-15 consecutive lines from file per spawn (grouped by indent level and structure)
  - Lines are trimmed of whitespace before placement
  - Line order within block doesn't need to be preserved
- **6-Color Limit**:
  - Tracks 6 color/level combinations: Blue×3 (Bright, Normal, Dark) + Green×3 (Bright, Normal, Dark)
  - Uses atomic counters (`atomic.Int64`) for race-free character tracking
  - Only spawns new blocks when fewer than 6 colors are present on screen
  - When all characters of a color/level are cleared, that slot becomes available
  - Atomic counters track each color/level combination (Blue×3 + Green×3)
  - SpawnSystem checks counters before spawning: `if count == 0 { spawn enabled }`
  - ScoreSystem decrements on character typing
  - DecaySystem updates during transitions
  - Red sequences explicitly excluded from tracking
- **Intelligent Placement**:
  - Each line attempts placement up to 3 times
  - Random row and column selection per attempt
  - Collision detection with existing characters
  - Cursor exclusion zone (5 horizontal, 3 vertical)
  - Lines that fail placement after 3 attempts are discarded
- **Position**: Random locations across screen avoiding collisions and cursor
- **Rate**: 2 seconds base, adaptive based on screen fill (1-4 seconds)
- **Generates**: Only Blue and Green sequences (never Red)

### Decay System

The decay system applies character degradation through a falling entity animation with swept collision detection. The system uses a **stateless architecture** where falling entities are queried from the `World.FallingDecays` store each frame rather than being tracked internally. This design allows for future extensions such as orbital and magnetic effects on decay entities.

#### Decay Mechanics
- **Brightness Decay**: Bright → Normal → Dark (reduces score multiplier)
  - Updates color counters atomically: decrements old level, increments new level
- **Color Decay Chain**:
  - Blue (Dark) → Green (Bright)
  - Green (Dark) → Red (Bright) ← **Only source of Red sequences**
  - Red (Dark) → Destroyed
  - Counter updates during color transitions (Blue→Green, Green→Red)
  - Red sequences are not tracked in color counters
- **Timing**: 10-60 seconds interval based on heat level (higher heat = faster decay)
  - Calculated when Gold sequence ends: `60s - (50s * heatPercentage)`
  - Timer uses pausable clock (freezes during COMMAND mode)
- **Counter Management**: Decrements counters when characters destroyed at Red (Dark) level

#### Falling Entity Animation
- **Spawn**: One falling entity per column stored as `FallingDecayComponent` in `World.FallingDecays` store (ensuring complete screen coverage)
- **Stateless Update**: System queries all falling entities via `world.FallingDecays.All()` each frame (no internal entity tracking)
- **Speed**: Random per entity, between 5.0-15.0 rows/second (FallingDecayMinSpeed/MaxSpeed)
- **Physics Integration**: Position updated via delta time: `YPosition += Speed × dt.Seconds()`
- **Character**: Random alphanumeric character per entity
- **Matrix Effect**: Characters randomly change as they fall (controlled by FallingDecayChangeChance)
- **Duration**: Based on slowest entity reaching bottom (gameHeight / FallingDecayMinSpeed)
- **Cleanup**: All falling entities automatically destroyed when animation completes (`world.FallingDecays.Count() == 0`)

#### Swept Collision Detection (Anti-Tunneling)

To prevent fast-moving entities from "tunneling" through characters without detecting collisions, the decay system uses swept segment traversal:

**Physics History Tracking** (`FallingDecayComponent`):
- `PrevPreciseY`: Y position from previous frame
- `YPosition`: Current Y position (updated by `YPosition += Speed * elapsed`)

**Swept Traversal Logic**:
1. Calculate integer row range: `startRow = int(PrevPreciseY)`, `endRow = int(YPosition)`
2. Sort coordinates to handle bidirectional movement: `if startRow > endRow { swap }`
3. Clamp to screen bounds: `[0, gameHeight-1]`
4. Iterate through ALL rows in range: `for row := startRow; row <= endRow`
5. Check collision at each integer grid cell in the path

**Example**:
- Entity at Y=4.8 in frame N-1, moves to Y=7.3 in frame N
- Swept traversal checks rows [4, 5, 6, 7]
- Guarantees collision detection even if entity moves >1 row per frame

#### Coordinate Latching (Anti-Green Artifacts)

Coordinate latching prevents re-processing the same grid cell when an entity lingers:

**Latch State** (`FallingDecayComponent`):
- `LastIntX`, `LastIntY`: Last processed integer grid coordinates
- Initialized to `(-1, -1)` to force first-frame processing

**Latch Check Logic**:
```go
if col == fall.LastIntX && row == fall.LastIntY {
    continue // Skip - already processed this cell
}
```

**Update After Interaction**:
- Latch is updated AFTER processing each grid cell
- Blocks re-processing even if `SpawnSystem` places new entity in same frame

**Result**:
- Eliminates "Green Artifacts" (lingering collision state)
- Each grid cell processed exactly once per falling entity
- New entities spawned at same location will not be hit by latched entity

#### Frame Deduplication (Anti-Double Hits)

Prevents multiple falling entities from hitting the same position in a single frame:

**Deduplication Map** (`DecaySystem.processedGridCells`):
- Type: `map[int]bool` with integer keys `(row * gameWidth) + col`
- Scope: Single frame (cleared at start of each `updateFallingEntities` call)
- Purpose: Track which grid cells have been hit this frame
- **Zero-Allocation Design**: Map is a persistent field in DecaySystem struct, cleared via `delete()` instead of reallocating

**Pattern**:
1. Clear map at frame start: `for k := range processedGridCells { delete(processedGridCells, k) }`
2. Check before processing: `if processedGridCells[flatIdx] { continue }`
3. Mark after hit: `processedGridCells[flatIdx] = true`

**Performance**:
- Reuses same map across frames (no allocations in hot path)
- Integer keys via flat indexing (no `fmt.Sprintf` overhead)
- O(1) lookup per collision check

#### Entity-Level Deduplication

Prevents the same target entity from being hit multiple times:

**Entity Tracking** (`DecaySystem.decayedThisFrame`):
- Type: `map[engine.Entity]bool`
- Scope: Entire animation (cleared when animation starts)
- Purpose: Track which entities have been decayed

**Pattern**:
1. Initialize at animation start: `decayedThisFrame = make(map[engine.Entity]bool)`
2. Check before decay: `if decayedThisFrame[targetEntity] { continue }`
3. Mark after decay: `decayedThisFrame[targetEntity] = true`

#### Collision Interaction Logic

When a falling entity encounters a target at `(col, row)`:

1. **Latch Check**: Skip if `(col, row) == (LastIntX, LastIntY)`
2. **Bounds Check**: Skip if outside screen
3. **Frame Deduplication**: Skip if `processedGridCells[flatIdx]` is true
4. **Spatial Lookup**: Get entity at position via `world.Positions.GetEntityAt(col, row)`
5. **Entity Deduplication**: Skip if `decayedThisFrame[targetEntity]` is true
6. **Process Hit**:
   - If Nugget: Destroy entity, clear active nugget reference
   - If Character: Apply decay logic (level/color transition)
   - Mark: `decayedThisFrame[targetEntity] = true`, `processedGridCells[flatIdx] = true`
7. **Update Latch**: `LastIntX = col`, `LastIntY = row`
8. **Matrix Effect**: Randomly change character on row transition

#### Thread Safety
- **Mutex Protection**: DecaySystem state protected by `sync.RWMutex`
  - `currentRow`: Current decay row for display purposes
  - `decayedThisFrame`: Entity-level deduplication map
  - `processedGridCells`: Frame-level spatial deduplication map
- **Stateless Entity Access**: All falling entities queried from `World.FallingDecays` store (no internal tracking)
- **Component Access**: World's internal synchronization for Get/Add operations

#### Pause Behavior
- **Timer**: Freezes during COMMAND mode (uses pausable clock)
- **Animation**: Elapsed time calculation based on `decaySnapshot.StartTime` (pausable)
- **Visual**: Falling entities dim with rest of game (70% brightness)

### Score System
- **Character Typing**: Processes user input in insert mode
- **Counter Management**:
  - Atomically decrements color counters when Blue/Green characters typed
  - Red and Gold characters do not affect color counters
- **Heat Updates**: Typing correct characters increases heat (with boost multiplier if active)
- **Error Handling**: Incorrect typing resets heat and triggers error cursor

#### Hit Detection Strategy

Due to `PositionStore` limitations (single entity per cell), the Cursor Entity effectively "masks" characters at the same position in the spatial index. Therefore, hit detection for typing uses a **Query Pattern** rather than simple spatial lookups:

**Implementation Pattern**:
```go
// ❌ INCORRECT: Spatial lookup fails when cursor masks character
entity, ok := world.Positions.GetEntityAt(cursorX, cursorY)
// Returns cursor entity, not the character underneath

// ✅ CORRECT: Query pattern iterates all Position+Character entities
entities := world.Query().
    With(world.Positions).
    With(world.Characters).
    Execute()

for _, entity := range entities {
    pos, _ := world.Positions.Get(entity)
    char, _ := world.Characters.Get(entity)
    if pos.X == cursorX && pos.Y == cursorY {
        // Found character at cursor position
        // Process typing logic...
    }
}
```

**Trade-offs**:
- **Spatial Lookup**: O(1) but fails when cursor masks character
- **Query Pattern**: O(n) but reliable for all entities at cursor position
- Query pattern is used for typing interactions where correctness is critical

### Boost System
- **Activation Condition**: Heat reaches maximum value (screen width)
- **Initial Duration**: 500ms (BoostExtensionDuration constant)
- **Color Binding**: Boost is tied to the color (Blue or Green) of the character that triggered max heat
- **Extension Mechanic**:
  - Typing matching color: Extends boost timer by 500ms per character
  - Typing different color: Deactivates boost immediately (heat remains at max)
  - Typing red or incorrect: Deactivates boost and resets heat to 0
- **Effect**: Heat gain multiplier of 2× (+2 heat per character instead of +1)
- **Visual Indicator**: Pink background "Boost: X.Xs" in status bar
- **Implementation**: Managed within ScoreSystem (not a separate system)
  - Atomic state: BoostEnabled (Bool), BoostEndTime (Int64), BoostColor (Int32)
  - Timer checked each frame via `UpdateBoostTimerAtomic()` (CAS pattern)
  - Color matching: Typing same color extends by 500ms via atomic update
  - No separate boost entities - pure state management

### Gold System
- **Trigger**: Spawns when decay animation completes (transitions to PhaseNormal)
- **Position**: Random location avoiding cursor (NOT fixed center-top)
- **Length**: Fixed 10 alphanumeric characters (randomly generated)
- **Duration**: 10 seconds (game time via pausable clock) before timeout
- **Reward**: Fills heat meter to maximum on completion
- **Cleaner Trigger**: If heat is already at maximum when gold completed, triggers Cleaner animation
- **Behavior**: Typing gold chars does not affect heat/score directly
- **Pause Behavior**: Timeout freezes during COMMAND mode (game time stops)

### Nugget System
- **Purpose**: Collectible bonus items that spawn randomly.
- **Behavior**: Spawns every 5 seconds if no nugget is active.
- **Collection**:
    - **Typing**: Typing the character displayed on the nugget collects it (handled by ScoreSystem).
    - **Jump (Tab)**: Pressing `Tab` instantly jumps the cursor to the nugget (requires Score >= 10).
- **Reward**: Increases Heat by 10% of max heat.
- **Cost**: Jumping via `Tab` costs 10 Score points.

### Drain System
- **Purpose**: A hostile entity that drains score if the player is idle or positioned on it.
- **Trigger**: Spawns when Score > 0. Despawns when Score <= 0.
- **Movement**: Moves toward the cursor every 1 second (independent of frame rate).
- **Effect**: If positioned on top of the cursor, drains 10 points every 1 second.
- **Visual**: Rendered as '╬' (Light Cyan).

### Cleaner System
- **Trigger**: Event-driven via `EventCleanerRequest` when gold completed at maximum heat
  - ScoreSystem pushes event: `ctx.PushEvent(engine.EventCleanerRequest, nil)`
  - CleanerSystem consumes event in Update() method (event polling pattern)
  - Frame deduplication: Tracks spawned frames to prevent duplicate activations
- **Update Model**: **Synchronous** - runs in main game loop via ECS Update() method
- **Architecture**: Pure ECS implementation using vector physics
  - All state stored in `CleanerComponent` (no external state tracking)
  - Physics-based movement with sub-pixel precision (`PreciseX`, `PreciseY`)
  - Velocity-driven updates: `position += velocity × deltaTime`
  - Frame-rate independent animation via delta time
- **Configuration**: Direct constants in `constants/cleaners.go`
  - **CleanerAnimationDuration**: Time to traverse screen (1 second)
  - **CleanerTrailLength**: Number of trail positions tracked (10)
  - **CleanerTrailFadeTime**: Trail fade duration (0.3 seconds)
  - **CleanerChar**: Unicode block character ('█')
  - **CleanerRemovalFlashDuration**: Flash effect duration (150ms)
- **Behavior**: Sweeps across rows containing Red characters, removing them on contact
- **Phantom Cleaners**: If no Red characters exist when event consumed, no entities spawn
  - Still pushes `EventCleanerFinished` (marks completion for testing/debugging)
  - Phase cycle continues independently (cleaners are non-blocking)
- **Direction**: Alternating - odd rows sweep L→R, even rows sweep R→L
- **Selectivity**: Only destroys Red characters, leaves Blue/Green untouched
- **Lifecycle**:
  - Spawn off-screen (±`CleanerTrailLength` from edges)
  - Target off-screen opposite side
  - Destroy when `PreciseX` passes `TargetX`
  - Ensures trail fully clears screen before entity removal
- **Physics System**:
  - **Velocity Calculation**: `baseSpeed = gameWidth / animationDuration`
  - **Movement Update**: `PreciseX += VelocityX × dt.Seconds()`
  - **Trail Recording**: New trail point added when cleaner enters new grid cell
  - **Trail Truncation**: Limited to `CleanerTrailLength` positions (FIFO queue)
- **Collision Detection** (Swept Segment):
  - Checks ALL integer positions between previous and current `PreciseX`
  - Prevents tunneling when cleaner moves >1 char/frame
  - Uses `math.Min/Max` for bidirectional range (L→R and R→L)
  - Range clamped to screen bounds before checking
  - Example: Movement from 8.2→10.7 checks positions [8, 9, 10]
- **Visual Effects**:
  - Pre-calculated gradient in renderer (built once at initialization)
  - Trail rendered with opacity falloff: 100% at head → 0% at tail
  - Removal flash spawns as separate `RemovalFlashComponent` entity
  - Flash cleanup: Automatic removal after `CleanerRemovalFlashDuration`
- **Thread Safety**:
  - Event-driven activation via lock-free EventQueue
  - Frame deduplication map prevents duplicate spawns (`spawned[event.Frame]`)
  - Component data protected by ECS World's internal synchronization
  - Renderer deep-copies trail data directly from components (no snapshots needed)
  - No external mutexes or state maps required (pure ECS + events)
- **Performance**:
  - Zero goroutine overhead (pure synchronous ECS)
  - No mutex contention (lock-free event queue + ECS synchronization)
  - Single component query per update
  - Minimal allocations (trail slice grows/shrinks in-place)
  - Pre-calculated rendering gradients (zero per-frame color math)
  - Typical update time: < 0.5ms for 24 cleaners

## Data Files

### assets/ directory
- **Purpose**: Contains `.txt` files with game content (code blocks)
- **Format**: Plain text files containing source code
- **Location**: Automatically located at project root by searching for `go.mod`
- **Content**: Source code files (e.g., Go standard library code like crypto/md5)
- **Discovery**: ContentManager scans for all `.txt` files (excluding hidden files starting with `.`)
- **Processing**:
  - All valid files are pre-validated and cached at initialization
  - Lines trimmed of whitespace
  - Empty lines and comments ignored
  - Files must have at least 10 valid lines after processing
  - Content blocks are selected randomly from validated cache