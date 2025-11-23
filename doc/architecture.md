# Vi-Fighter Architecture

## Core Paradigms

### Entity-Component-System (ECS)
**Strict Rules:**
- Entities are ONLY identifiers (uint64)
- Components contain ONLY data, NO logic
- Systems contain ALL logic, operate on component sets
- World is the single source of truth for all game state

### Event-Driven Communication
**Architecture**: Systems decouple via `GameContext.EventQueue` for inter-system communication.

**Core Principles:**
- **Decoupling**: Systems push events instead of calling methods on other systems
- **Lock-Free**: Ring buffer with atomic operations (no mutexes)
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

**Why Events Instead of Direct Calls:**
```go
// ❌ WRONG: Direct method calls create tight coupling
cleanerSystem.ActivateCleaners()  // ScoreSystem must hold CleanerSystem reference

// ❌ WRONG: Shared boolean flags create race conditions
ctx.State.SetCleanerPending(true)  // Requires mutex, hard to test

// ✅ CORRECT: Events decouple systems
ctx.PushEvent(engine.EventCleanerRequest, nil)  // Lock-free, testable, observable
```

**Implementation** (`engine/events.go`):
- `EventQueue`: Fixed-size ring buffer (256 events)
- `Push()`: Lock-free CAS (Compare-And-Swap) for concurrent producers
- `Consume()`: Atomically claims and returns all pending events
- `Peek()`: Read-only inspection for debugging/testing

**Benefits**:
- **Testability**: Events are observable (can assert event was pushed)
- **Debuggability**: Event log shows all inter-system communication
- **Thread-Safety**: Lock-free ring buffer, no contention
- **Flexibility**: Easy to add new consumers for existing events

**Trade-offs**:
- **Latency**: Events processed next frame (not immediate)
- **Indirection**: Event flow less obvious than direct method calls
- **Deduplication**: Consumers must track processed events by frame

### State Ownership Model

**GameState** (`engine/game_state.go`) centralizes game state with clear ownership boundaries:

#### Real-Time State (Lock-Free Atomics)
Updated immediately on user input/spawn events, read by all systems:
- **Heat** (`atomic.Int64`): Current heat value
- **Score** (`atomic.Int64`): Player score
- **Cursor Position** (`atomic.Int32`): CursorX, CursorY for spawn exclusion zone
- **Color Counters** (6× `atomic.Int64`): Blue/Green × Bright/Normal/Dark tracking
- **Boost State** (`atomic.Bool`, `atomic.Int64`): Enabled, EndTime, Color
- **Visual Feedback**: CursorError, ScoreBlink, PingGrid (atomic)
- **Drain State** (`atomic.Bool`, `atomic.Uint64`, `atomic.Int32`): Active, EntityID, X, Y
- **Sequence ID** (`atomic.Int64`): Thread-safe ID generation

**Why Atomic**: These values are accessed on every frame and every keystroke. Atomics provide:
- Lock-free reads (no contention on render or input threads)
- Immediate consistency (typing feedback feels instant)
- Race-free updates without blocking

#### Clock-Tick State (Mutex Protected)
Updated during scheduled game logic ticks, read by all systems:
- **Spawn Timing** (`sync.RWMutex`): LastTime, NextTime, RateMultiplier
- **Screen Density**: EntityCount, ScreenDensity, SpawnEnabled
- **6-Color Limit**: Enforced via atomic color counter checks
- **Game Phase State**: CurrentPhase, PhaseStartTime
- **Gold Sequence State**: GoldActive, GoldSequenceID, GoldStartTime, GoldTimeoutTime
- **Decay Timer State**: DecayTimerActive, DecayNextTime
- **Decay Animation State**: DecayAnimating, DecayStartTime

**Why Mutex**: These values change infrequently (every 2 seconds for spawn, or on game events) and require:
- Consistent multi-field reads (spawn timing snapshot, phase state)
- Atomic state transitions (spawn rate adaptation, phase changes)
- Blocking is acceptable (not on hot path)

#### State Access Patterns

**Systems access GameState through GameContext:**
All systems hold a reference to `*engine.GameContext` and access state via `ctx.State`:

```go
// Real-time (typing): Direct atomic access through GameContext
ctx.State.AddHeat(1)                    // No lock, instant
ctx.State.AddColorCount(Blue, Bright, 1) // Atomic increment

// Clock-tick (spawn): Snapshot pattern through GameContext
snapshot := ctx.State.ReadSpawnState()  // RLock, consistent view
if ctx.State.ShouldSpawn() {            // RLock, check timing
    // ... spawn logic ...
    ctx.State.UpdateSpawnTiming(now, next) // Lock, update state
}

// Render: Safe concurrent reads through GameContext
heat := ctx.State.GetHeat()             // Atomic load
snapshot := ctx.State.ReadSpawnState()  // RLock, no blocking

// Dimensions: Read directly from GameContext (no caching)
width := ctx.GameWidth   // Always current
height := ctx.GameHeight // Always current
```

**GameContext Role:**
- GameContext is **NOT** a delegation layer for State
- It provides direct access to GameState via the `State` field
- Systems read dimensions directly from `ctx.GameWidth` and `ctx.GameHeight`
- No local dimension caching - always read current values
- Input-specific methods (cursor position, mode, motion commands) remain on GameContext

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

// Cursor Position (atomic fields)
type CursorSnapshot struct {
    X int
    Y int
}
snapshot := ctx.State.ReadCursorPosition() // Used by: SpawnSystem (exclusion zone), Renderer

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
| SpawnSystem | SpawnState, ColorCounts, CursorPosition | Check spawn conditions, cursor exclusion zone |
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

2. **Atomic Field Snapshots** (ColorCounts, CursorPosition, BoostState, HeatAndScore):
   - No locks required
   - Multiple atomic loads in sequence
   - Still provides consistent view (atomic loads are sequentially consistent)
   - Trade-off: Very rare possibility of seeing mixed state between loads (acceptable for these use cases)

3. **Immutability After Capture:**
   - All snapshots are value types (structs)
   - Modifying snapshot fields doesn't affect GameState
   - Safe to pass snapshots across goroutine boundaries
   - No memory aliasing issues

#### Testing
- `engine/game_state_test.go`: Unit tests for atomic operations, state snapshots, spawn timing
  - `TestSnapshotConsistency`: Verifies snapshots remain consistent under rapid state changes (10 concurrent readers)
  - `TestNoPartialReads`: Ensures snapshots never show partial state updates (5 concurrent readers)
  - `TestSnapshotImmutability`: Confirms snapshots are immutable value copies
  - `TestAllSnapshotTypesConcurrent`: Tests all snapshot types under concurrent access (5 concurrent readers)
  - `TestAtomicSnapshotConsistency`: Verifies atomic field snapshots (10 concurrent readers, 1000 rapid updates)
- `systems/race_condition_comprehensive_test.go`: Snapshot pattern integration tests
  - `TestSnapshotConsistencyUnderRapidChanges`: Multi-writer (3) + multi-reader (10) snapshot consistency
  - `TestSnapshotImmutabilityWithSystemUpdates`: Snapshot immutability during active system modifications
  - `TestNoPartialSnapshotReads`: Verifies no partial reads during rapid updates (8 concurrent readers)
  - `TestPhaseSnapshotConsistency`: Phase snapshot consistency during rapid transitions (5 concurrent readers)
  - `TestMultiSnapshotAtomicity`: Multiple snapshot types taken in rapid succession (10 concurrent readers)
- All tests pass with `-race` flag (no data races detected)

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

#### Testing
- `engine/clock_scheduler_test.go`: Scheduler tick tests, phase transition tests
  - `TestClockSchedulerBasicTicking`: Tick count increment verification
  - `TestClockSchedulerConcurrentTicking`: Concurrent goroutine safety
  - `TestClockSchedulerStopIdempotent`: Multiple Stop() calls safety
  - `TestClockSchedulerTickRate`: 50ms tick rate verification
  - `TestPhaseTransitions`: Phase transition logic validation
  - `TestConcurrentPhaseReads`: Concurrent phase access safety
- `engine/integration_test.go`: Comprehensive cycle and phase tests
  - `TestCompleteGameCycle`: Full Normal→Gold→DecayWait→DecayAnim→Normal cycle
  - `TestGoldCompletionBeforeTimeout`: Early gold completion handling
  - `TestConcurrentPhaseReadsDuringTransitions`: 20 readers × concurrent access test
  - `TestPhaseTimestampConsistency`: Timestamp accuracy verification
  - `TestPhaseDurationCalculation`: Duration calculation accuracy
  - `TestCleanerTrailCollisionLogic`: Swept segment collision detection verification
  - `TestNoSkippedCharacters`: Verification that truncation logic doesn't skip characters
  - `TestRapidPhaseTransitions`: Rapid phase transition stress test
  - `TestGoldSequenceIDIncrement`: Sequential ID generation
- All tests pass with `-race` flag
- Clock runs in goroutine, no memory leaks detected
- Concurrent phase reads/writes tested (20 goroutines)

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
- Primary index: `World.spatialIndex[y][x] -> Entity`
- Secondary index: `World.componentsByType[Type] -> []Entity`
- ALWAYS use spatial transactions for position changes (tx.Move, tx.Spawn, tx.Destroy)
- SafeDestroyEntity handles spatial index cleanup atomically

## Component Hierarchy
```
Component (marker interface)
├── PositionComponent {X, Y}
├── CharacterComponent {Rune, Style}
├── SequenceComponent {ID, Index, Type, Level}
├── GoldSequenceComponent {Active, SequenceID, StartTimeNano, CharSequence, CurrentIndex}
├── FallingDecayComponent {Column, YPosition, Speed, Char, LastChangeRow}
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
  - Formula: `60s - (50s * heatPercentage)`
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

### CleanerSystem Synchronous Model
- **Update Pattern**: Called from main loop's `Update()` method with delta time
- **No Goroutine**: Eliminates race conditions from concurrent entity access
- **Atomic Activation**: Single `atomic.Bool` flag (`pendingSpawn`) for spawn triggering
- **Pure ECS State**: All cleaner state stored in `CleanerComponent` instances
  - No external state maps or tracking structures
  - Component data includes physics state, trail history, and rendering info
  - ECS World provides internal synchronization for component access
- **Frame-Coherent Rendering**: Renderer queries World directly and deep-copies trail data for thread-safe rendering
- **Zero External Locks**: No mutexes required (atomic flag + ECS internal synchronization)

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

### CleanerSystem Race Prevention Strategy
**Problem**: Early implementations had complex state tracking with potential race conditions

**Solution (Current Pure ECS Implementation)**:
- ✅ **Pure ECS Pattern**: All state in `CleanerComponent`, no external maps or tracking
- ✅ **Synchronous Updates**: Main loop `Update()` method with delta time integration
- ✅ **Event-Driven Activation**: `EventCleanerRequest` triggers spawning via event queue
- ✅ **Component-Based Physics**: Sub-pixel position, velocity, and trail stored in component
- ✅ **Snapshot Rendering**: Renderer queries World directly and deep-copies trail positions for thread safety
- ✅ **ECS Synchronization**: Leverages World's internal locking for component access
- ✅ **Zero State Duplication**: Component is the single source of truth

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
    // Query World directly for cleaner components
    cleanerType := reflect.TypeOf(components.CleanerComponent{})
    entities := world.GetEntitiesWith(cleanerType)

    for _, entity := range entities {
        compRaw, ok := world.GetComponent(entity, cleanerType)
        if !ok {
            continue
        }
        cleaner := compRaw.(components.CleanerComponent)

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
    c.Trail = append([]core.Point{newPoint}, c.Trail...)
    world.AddComponent(entity, c)  // ECS handles synchronization
}
```

### Testing for Race Conditions
- All tests must pass with `go test -race`
- Dedicated race tests in `systems/cleaner_race_test.go`
- Integration tests verify concurrent scenarios
- Benchmarks validate performance impact of synchronization

## Performance Guidelines

### Hot Path Optimizations
1. Cache entity queries per frame
2. Use spatial index for position lookups
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
4. **Heat Non-Negativity**: Heat (typing momentum) is always >= 0
5. **Boost Mechanic**: When heat reaches maximum, boost activates with color-matching (Blue or Green) providing x2 heat multiplier. Typing the matching color extends boost duration by 500ms per character, while typing a different color deactivates boost
6. **Red Spawn Invariant**: Red sequences are NEVER spawned directly, only through decay
7. **Gold Randomness**: Gold sequences spawn at random positions
8. **6-Color Limit**: At most 6 Blue/Green color/level combinations present simultaneously
9. **Counter Accuracy**: Color counters must match actual on-screen character counts
10. **Atomic Operations**: All color counter updates use atomic operations for thread safety

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
- **Brightness Decay**: Bright → Normal → Dark (reduces score multiplier)
  - Updates color counters atomically: decrements old level, increments new level
- **Color Decay Chain**:
  - Blue (Dark) → Green (Bright)
  - Green (Dark) → Red (Bright) ← **Only source of Red sequences**
  - Red (Dark) → Destroyed
  - Counter updates during color transitions (Blue→Green, Green→Red)
  - Red sequences are not tracked in color counters
- **Timing**: 10-60 seconds interval based on heat level (higher heat = faster decay)
- **Animation**: Row-by-row sweep from top to bottom
- **Counter Management**: Decrements counters when characters destroyed at Red (Dark) level

### Score System
- **Character Typing**: Processes user input in insert mode
- **Counter Management**:
  - Atomically decrements color counters when Blue/Green characters typed
  - Red and Gold characters do not affect color counters
- **Heat Updates**: Typing correct characters increases heat (with boost multiplier if active)
- **Error Handling**: Incorrect typing resets heat and triggers error cursor

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

## Error Handling Strategy

- **User Input**: Flash error cursor, reset heat
- **System Errors**: Log warning, continue with degraded functionality
- **Missing Data File**: Graceful degradation (no file-based spawns)
- **Fatal Errors**: Clean shutdown with screen.Fini()

## Testing Strategy

### Test Suite Organization

The test suite has been reorganized into smaller, focused files for better maintainability and clarity. Large test files (>30KB) have been split into logical groups.

### Unit Tests
- **Component Tests**: Validate individual component data structures
- **System Tests**: Test system logic in isolation
- **Helper Tests**: Verify utility functions and time providers
- **Motion Tests**: Validate vi motion commands and find operations

#### Heat Display Tests
Located in `render/heat_display_test.go`:
- **TestHeatPercentageMapping**: Verifies heat display correctly maps percentage to 0-10 range
  - Tests edge cases: 0%, 25%, 50%, 75%, 100%
  - Tests different terminal widths (40, 80, 100, 120, 200)
  - Validates formula: `displayHeat = int(float64(heat) / float64(maxHeat) * 10.0)`
- **TestHeatDisplayBounds**: Verifies display heat is always within 0-10 range
  - Tests negative heat, zero heat, normal heat, max heat, and over-max heat
  - Ensures bounds checking (0 ≤ displayHeat ≤ 10)
- **TestHeatDisplayEdgeCases**: Verifies boundary conditions in heat calculation
  - Tests exact percentage boundaries (10%, 20%, etc.)
  - Tests just below and just above boundaries
  - Tests clamping behavior for over-max heat
- **TestHeatDisplayGranularity**: Verifies correct granularity at different heat levels
  - Tests all 11 heat ranges (0-9%, 10-19%, ..., 90-99%, 100%)
  - Validates each 10% increment transitions to next segment

#### Motion and Command Tests
The motion tests have been reorganized from a single large file into focused test files:

**Located in `modes/`:**
- **motions_screen_test.go**: Screen position motions (H/M/L, ^)
- **motions_word_test.go**: Vim word motions (w, b, e) - basic navigation, transitions, boundaries
- **motions_bigword_test.go**: WORD motions (W, B, E) - space-delimited word navigation
- **motions_paragraph_test.go**: Paragraph motions ({, })
- **motions_simple_test.go**: Simple directional motions (space, h/j/k/l with counts)
- **motions_count_test.go**: Motion count handling and edge cases
- **motions_gaps_test.go**: File content and position gap handling
- **motions_brackets_test.go**: Bracket matching (%, parentheses, braces, square brackets)
- **motions_angles_test.go**: Angle bracket matching (<, >)
- **motions_helpers_test.go**: Helper functions and character type detection
- **find_motion_test.go**: Find/till motions (f, F, t, T, ;, ,)
  - Forward find (`f`) with count support: `fa`, `2fa`, `5fx`
  - Backward find (`F`) with count support: `Fa`, `2Fa`, `3Fb`
  - Edge cases: no match, count exceeds matches, boundary conditions
  - Unicode character support
  - Delete integration: `dfa`, `d2fa`, `dFx`, `d3Fx`
- **till_motion_test.go**: Till motion tests
- **regression_test.go**: Ensures no regressions in existing vi commands
  - Basic motions: h, j, k, l, w, b, e, W, B, E
  - Count prefixes: `5h`, `3j`, `10w`
  - Special commands: `gg`, `G`, `go`, `dd`, `d$`, `%`
  - Cursor validation and boundary checks
  - Race condition tests for motion execution
- **count_aware_commands_test.go**: Count-aware command state management
- **input_test.go**: Input handling and mode switching
- **delete_operator_test.go**: Delete operation tests

#### Cleaner System Tests
The cleaner system tests have been reorganized into focused files after a major refactoring that simplified the system by ~2500 lines of code. The new pure ECS implementation eliminates complex state tracking, resulting in cleaner, more maintainable tests:

**Located in `systems/`:**
- **cleaner_activation_test.go**: Cleaner trigger conditions and activation logic
  - TestCleanersTriggerConditions: Heat-based cleaner triggering
  - TestCleanerActivationWithoutRed: Phantom cleaner activation when no Red characters exist
  - TestCleanersDuplicateTriggerIgnored: Duplicate trigger prevention
- **cleaner_movement_test.go**: Cleaner movement and direction logic
  - TestCleanersDirectionAlternation: Odd/even row direction verification
  - TestCleanersMovementSpeed: Movement speed validation
  - TestCleanersMultipleRows: Multi-row cleaner behavior
- **cleaner_collision_test.go**: Collision detection and character removal
  - TestCleanersRemoveOnlyRedCharacters: Selectivity tests (Red only)
  - TestCleanersNoRedCharacters: Phantom cleaner behavior
  - TestCleanerCollisionCoverage: Verifies no position gaps in collision detection
  - TestCleanerCollisionReverseDirection: Gap coverage for R→L cleaners
  - TestCleanerCollisionLongTrail: Coverage across entire trail
- **cleaner_lifecycle_test.go**: Animation lifecycle and resource management
  - TestCleanersAnimationCompletion: Animation lifecycle validation
  - TestCleanersTrailTracking: Trail position management
  - TestCleanersPoolReuse: Memory pool efficiency validation
- **cleaner_flash_test.go**: Visual flash effects
  - TestCleanersRemovalFlashEffect: Flash effect creation
  - TestCleanersFlashCleanup: Flash effect cleanup
  - TestCleanersNoFlashForBlueGreen: Flash selectivity (Red only)
  - TestCleanersMultipleFlashEffects: Multiple concurrent flashes
- **cleaner_test_helpers.go**: Shared helper functions for cleaner tests

#### Spawn System Tests
The spawn system tests have been reorganized into focused files:

**Located in `systems/`:**
- **spawn_content_test.go**: Content management and filtering
  - TestContentManagerIntegration: ContentManager initialization
  - TestCommentFiltering: Full-line comment filtering
  - TestEmptyBlockHandling: Empty code block handling
  - TestBlockGroupingWithShortLines: Block size filtering
- **spawn_colors_test.go**: Color counter and availability
  - TestColorCounters: Atomic color counter operations
  - TestColorCountersConcurrency: Thread-safe counter updates
  - TestGetAvailableColors: Color availability tracking (max 6)
  - TestSpawnWithNoAvailableColors: Spawning when all colors in use
- **spawn_placement_test.go**: Line placement and positioning
  - TestPlaceLine: Basic line placement
  - TestPlaceLineNearCursor: Cursor exclusion zone logic
  - TestPlaceLineSkipsSpaces: Space character handling
  - TestPlaceLinePositionMaintenance: Position tracking with spaces
  - TestPlaceLinePackageMd5: Specific line placement testing
  - TestPlaceLineConstBlockSize: Complex multi-space placement
- **spawn_blocks_test.go**: Block grouping and spawning
  - TestGroupIntoBlocks: Logical code block grouping
  - TestGetIndentLevel: Indentation calculation
  - TestBlockSpawning: Complete block spawning

#### Nugget System Tests
The nugget system tests are organized into focused files:

**Located in `systems/`:**
- **nugget_edge_cases_test.go**: Nugget lifecycle and edge cases
  - TestNuggetSingleInvariant: Verifies only one nugget active at a time
  - TestNuggetRapidCollectionAndRespawn: Tests rapid collection/respawn cycles
  - TestNuggetClearWithWrongEntityID: Entity ID validation for clearing
  - TestNuggetDoubleDestruction: Graceful handling of double destruction
  - TestNuggetClearAfterNewSpawn: Stale entity reference handling
  - TestNuggetVerificationClearsStaleReference: Automatic stale reference cleanup
  - TestNuggetSpawnPositionExclusionZone: Cursor exclusion zone enforcement
  - TestNuggetGetSystemState: Debug state string validation
  - TestNuggetConcurrentClearAttempts: Atomic CAS operation validation
- **nugget_decay_test.go**: Decay system interaction with nuggets
  - TestDecayDestroysNugget: Falling decay entities destroy nuggets
  - TestDecayDoesNotDestroyNuggetAtDifferentPosition: Position-specific destruction
  - TestDecayDestroyMultipleNuggetsInDifferentColumns: Multi-column destruction
  - TestDecayDestroyNuggetAndSequence: Mixed entity type processing
  - TestDecayNuggetRespawnAfterDestruction: Respawn after decay destruction
  - TestDecayDoesNotProcessSameNuggetTwice: Idempotent destruction
  - **Frame Rate Matching**: Uses 16ms time steps (matching real game) to prevent row skipping at high speeds (max 15 rows/s)
- **nugget_jump_test.go**: Tab key jumping to nuggets
  - TestNuggetJumpWithSufficientScore: Jump mechanics with score >= 10
  - TestNuggetJumpWithInsufficientScore: Jump prevention with score < 10
  - TestNuggetJumpWithNoActiveNugget: Graceful handling when no nugget exists
  - TestNuggetJumpUpdatesPosition: Cursor position update verification
  - TestNuggetJumpMultipleTimes: Sequential jump operations
  - TestNuggetJumpWithNuggetAtEdge: Edge position handling
  - TestJumpToNuggetMethodReturnsCorrectPosition: Method contract validation
  - TestJumpToNuggetWithMissingComponent: Graceful degradation
  - TestJumpToNuggetAtomicCursorUpdate: Atomic state update verification
  - TestJumpToNuggetEntityStillExists: Non-destructive jump validation
- **nugget_typing_test.go**: Typing on nuggets
  - TestNuggetTypingIncreasesHeat: Heat increase by 10% of max
  - TestNuggetTypingDestroysAndReturnsSpawn: Complete collection/respawn cycle
  - TestNuggetTypingNoScoreEffect: Score independence verification
  - TestNuggetTypingNoErrorEffect: No error state on collection
  - TestNuggetTypingMultipleCollections: Heat accumulation validation
  - TestNuggetTypingWithSmallScreen: Minimum heat increase of 1
  - TestNuggetAlwaysIncreasesVisualBlocks: Visual feedback guarantee

**Located in `render/`:**
- **nugget_cursor_test.go**: Nugget rendering and cursor interaction
  - TestNuggetDarkensUnderCursor: Contrast enhancement when cursor on nugget
  - TestNormalCharacterStaysBlackUnderCursor: Normal character behavior unchanged
  - TestCursorWithoutCharacterHasNoContrast: Empty cursor rendering
  - TestNuggetContrastInInsertMode: Mode-independent contrast behavior
  - TestNuggetOffCursorHasNormalColor: Standard rendering when not under cursor
  - TestCursorErrorOverridesNuggetContrast: Error cursor precedence
  - TestNuggetComponentDetectionLogic: Component type detection
  - TestNuggetLayeringCursorOnTop: Render layer ordering
  - TestMultipleNuggetInstances: Multi-nugget rendering validation

#### Drain System Tests
The drain system tests are organized into focused files:

**Located in `systems/`:**
- **drain_lifecycle_test.go**: Drain entity spawn and despawn lifecycle
- **drain_movement_test.go**: Movement toward cursor and pathfinding logic
- **drain_collision_test.go**: Collision detection with sequences, nuggets, and gold
- **drain_score_test.go**: Score draining mechanics and timing
- **drain_integration_test.go**: Integration with other game systems
- **drain_visualization_test.go**: Rendering and visual feedback

#### Content Manager Tests
The content manager system tests validate content loading and management:

**Located in `content/`:**
- **manager_test.go**: Core content manager functionality
- **integration_test.go**: Integration with spawn system
- **defensive_test.go**: Error handling and edge cases
- **example_test.go**: Usage examples and documentation

### Integration Tests
Located in `systems/integration_test.go`:
- **TestDecaySystemCounterUpdates**: Validates color counter updates during decay
- **TestDecaySystemColorTransitionWithCounters**: Tests color transitions with atomic counter updates
- **TestScoreSystemCounterDecrement**: Verifies counter decrements when typing characters
- **TestScoreSystemDoesNotDecrementRedCounter**: Ensures red characters don't affect color counters

Also see `engine/integration_test.go` for phase transition and game cycle integration tests.

### Race Condition Tests
The race condition tests have been reorganized into focused files:

**Located in `systems/`:**
- **race_content_test.go**: Content system race conditions
  - TestConcurrentContentRefresh: Concurrent content refresh with spawning
  - TestRenderWhileSpawning: Render operations during spawn operations
  - TestContentSwapDuringRead: Content swap during concurrent reads
  - TestStressContentSystem: Stress testing of content management
- **race_counters_test.go**: Color counter race conditions
  - TestConcurrentColorCounterUpdates: Cross-system color counter race conditions
- **race_snapshots_test.go**: Snapshot consistency race conditions
  - TestSnapshotConsistencyUnderRapidChanges: Snapshot consistency during rapid changes
  - TestSnapshotImmutabilityWithSystemUpdates: Snapshot immutability during system updates
  - TestNoPartialSnapshotReads: No partial state updates in snapshots
  - TestPhaseSnapshotConsistency: Phase snapshot consistency
  - TestMultiSnapshotAtomicity: Multiple snapshot atomicity
- **cleaner_race_test.go**: Cleaner system race conditions
  - TestNoRaceCleanerConcurrentRenderUpdate: Concurrent component updates and snapshot rendering
  - TestNoRaceRapidCleanerCycles: Rapid activation via atomic pendingSpawn flag
  - TestNoRaceCleanerStateAccess: Concurrent component reads during ECS queries
  - TestNoRaceFlashEffectManagement: Concurrent RemovalFlashComponent creation/cleanup
  - TestNoRaceCleanerPoolAllocation: Trail slice growth/shrinkage during updates
  - TestNoRaceCleanerAnimationCompletion: Entity destruction during active iteration
- **boost_race_test.go**: Boost system race conditions
  - TestBoostRapidToggle: Rapid boost activation/deactivation
  - TestBoostConcurrentRead: Concurrent boost state reads
  - TestBoostExpirationRace: Boost expiration race conditions
  - TestAllAtomicStateAccess: Comprehensive atomic state access validation
  - TestConcurrentPingTimerUpdates: Concurrent ping timer updates
  - TestConcurrentBoostUpdates: Concurrent boost state updates

### Deterministic Tests
Located in `systems/cleaner_deterministic_test.go`:
- **TestDeterministicCleanerLifecycle**: Frame-by-frame cleaner behavior verification
- **TestDeterministicCleanerTiming**: Exact animation duration validation
- **TestDeterministicCollisionDetection**: Predictable collision timing
- Uses `MockTimeProvider` for precise time control

### Benchmark Tests
Located in `systems/cleaner_benchmark_test.go`:
- **Cleaner Spawn Performance**: `BenchmarkCleanerSpawn`
- **Collision Detection**: `BenchmarkCleanerDetectAndDestroy`
- **Row Scanning**: `BenchmarkCleanerScanRedRows`
- **Physics Updates**: `BenchmarkCleanerUpdate` (position, velocity, trail)
- **Flash Effects**: `BenchmarkFlashEffectCreation`, `BenchmarkFlashEffectCleanup`
- **Gold Sequence Operations**: `BenchmarkGoldSequenceSpawn`, `BenchmarkGoldSequenceCompletion`
- **Concurrent Operations**: `BenchmarkConcurrentCleanerOperations`
- **Full Pipeline**: `BenchmarkCompleteGoldCleanerPipeline`
- **Performance Target**: < 0.5ms for 24 cleaners (pure ECS synchronous update)

### Running Tests

#### Standard Test Run
```bash
# Run all tests
go test ./... -v

# Run modes tests only
go test ./modes/... -v

# Run systems tests only
go test ./systems/... -v
```

#### Race Detector (CRITICAL for concurrency validation)
```bash
# Run all tests with race detection
go test ./... -race -v

# Run modes tests with race detection
go test ./modes/... -race -v

# Run systems tests with race detection
go test ./systems/... -race -v
```

#### Benchmarks
```bash
go test ./systems/... -bench=. -benchmem
```

#### Specific Test Categories
```bash
# Find motion tests only
go test ./modes/... -run TestFindChar -v

# Regression tests only
go test ./modes/... -run "Regression|StillWork" -v

# Integration tests only
go test ./systems/... -run TestConcurrent -v

# Race condition tests only
go test ./systems/... -run TestRapid -v -race
go test ./modes/... -run TestNoRace -v -race

# Memory leak detection (long running)
go test ./systems/... -run TestMemoryLeak -v
```

#### Debug Logging
Set environment variable for verbose race logging:
```bash
VERBOSE_RACE_LOG=1 go test ./systems/... -race -v
```

### Debug Helpers and Race Detection Tools

The codebase includes specialized tools for testing thread-safety and detecting race conditions. These tools are essential for maintaining the correctness of the hybrid real-time/clock-based architecture.

Located in `systems/test_helpers.go`:

**RaceDetectionLogger** - Event logging for concurrent execution analysis:
```go
logger := NewRaceDetectionLogger(enabled bool)
logger.Log(goroutine, operation, details string)  // Records timestamped events
events := logger.GetEvents()                      // Retrieves all logged events
logger.DumpEvents(filename string)                // Exports to file for analysis
```

Features:
- Atomic event counter (thread-safe ID generation)
- Mutex-protected event storage
- Optional verbose logging via `VERBOSE_RACE_LOG=1` environment variable
- Timestamp precision for ordering concurrent operations
- Useful for post-mortem analysis of race condition failures

**ConcurrencyMonitor** - Operation tracking and anomaly detection:
```go
monitor := NewConcurrencyMonitor()
monitor.StartOperation(operation string)  // Track operation start
monitor.EndOperation(operation string)    // Track operation end
stats := monitor.GetStats()               // Get concurrency statistics
```

Tracks:
- Active operations per type (current concurrent count)
- Maximum concurrent operations observed
- Total operation count
- Anomaly detection (e.g., EndOperation without StartOperation)
- Thread-safe via mutex protection

Use cases:
- Verify system calls are properly balanced (start/end pairs)
- Measure peak concurrency levels
- Detect operation leaks (operations that start but never end)

**AtomicStateValidator** - State consistency validation:
```go
validator := NewAtomicStateValidator()
validator.ValidateCleanerState(isActive, activationTime, lastUpdateTime)
validator.ValidateCounterState(colorType, level string, count int64)
validator.ValidateGoldState(isActive bool, sequenceID int)
violations := validator.GetViolations()  // Retrieve all recorded violations
```

Validates:
- Cleaner state consistency (active state vs. timestamps)
- Color counter non-negativity
- Gold sequence state invariants (active implies non-zero sequence ID)
- Records violations with descriptive messages
- Atomic validation counter for performance metrics

**EntityLifecycleTracker** - Memory leak detection:
```go
tracker := NewEntityLifecycleTracker()
tracker.TrackCreate(entityID uint64)      // Record entity creation
tracker.TrackDestroy(entityID uint64)     // Record entity destruction
leaked := tracker.DetectLeaks()           // Find entities never destroyed
stats := tracker.GetStats()               // Creation/destruction statistics
```

Features:
- Tracks entity creation and destruction timestamps
- Detects entities created but never destroyed (memory leaks)
- Thread-safe via mutex protection
- Atomic operation counters for performance tracking
- Useful for long-running integration tests

**Usage in Tests:**

These tools are designed for integration into race condition and stress tests:

```go
func TestConcurrentSystemBehavior(t *testing.T) {
    logger := NewRaceDetectionLogger(true)
    monitor := NewConcurrencyMonitor()
    validator := NewAtomicStateValidator()

    // Test execution with logging
    monitor.StartOperation("spawn")
    logger.Log("spawner", "spawn", "Spawning entity...")
    // ... perform operation ...
    validator.ValidateCounterState("Blue", "Bright", blueCount)
    monitor.EndOperation("spawn")

    // Verify no issues
    if monitor.HasAnomalies() {
        t.Errorf("Anomalies detected: %v", monitor.GetAnomalies())
    }
    if validator.HasViolations() {
        t.Errorf("State violations: %v", validator.GetViolations())
    }

    // Optional: Export event log for analysis
    logger.DumpEvents("race_events.log")
}
```

**Why These Tools Matter:**

The vi-fighter architecture uses a hybrid approach:
- Real-time atomic updates (heat, score, cursor) - lock-free, high frequency
- Clock-tick mutex updates (spawn, phase, gold, decay) - locked, lower frequency

Race detection tools help verify:
1. Atomics are never mixed with non-atomic access
2. Mutexes are properly paired (RLock/RUnlock, Lock/Unlock)
3. State invariants hold even under concurrent access
4. No operations are lost or double-counted
5. Memory is properly released (no entity leaks)

All tests using these tools must pass with `go test -race` flag.

### Test Coverage Goals
- **Unit Tests**: >80% coverage for core systems
- **Integration Tests**: All critical concurrent scenarios
- **Race Tests**: All atomic operations and shared state access
- **Benchmarks**: All performance-critical paths

### Common Race Conditions to Test
1. **Cleaner System**:
   - Concurrent spawn requests (atomic flag operations)
   - Component updates during snapshot capture
   - Trail slice modification during deep copy
   - Entity destruction during iteration

2. **Gold System**:
   - Spawn during active sequence
   - Completion during spawn
   - Cleaner triggering race conditions

3. **Color Counters**:
   - Concurrent increment/decrement (spawn vs. score vs. decay)
   - Read during modification
   - Negative counter prevention

4. **Spatial Index**:
   - Concurrent reads during entity destruction
   - Position updates during queries
   - Entity removal during iteration

### Continuous Integration Requirements
- All tests must pass with `-race` flag
- No memory leaks detected in leak tests
- Benchmarks should not regress >10% between commits
- Test coverage should not decrease