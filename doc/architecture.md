# Vi-Fighter Architecture

## Core Paradigms

### Entity-Component-System (ECS)
**Strict Rules:**
- Entities are ONLY identifiers (uint64)
- Components contain ONLY data, NO logic
- Systems contain ALL logic, operate on component sets
- World is the single source of truth for all game state

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
- **Cleaner State**: CleanerPending, CleanerActive, CleanerStartTime

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

// Cleaner State (mutex-protected)
type CleanerSnapshot struct {
    Pending   bool
    Active    bool
    StartTime time.Time
    Elapsed   time.Duration
}
snapshot := ctx.State.ReadCleanerState() // Used by: CleanerSystem, Renderer

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
| CleanerSystem | CleanerState, PhaseState | Manage cleaner lifecycle |
| Renderer | All snapshot types | Render game state without blocking game loop |
| ClockScheduler | PhaseState, GoldState, DecayState | Manage phase transitions |

**Concurrency Guarantees:**

1. **Mutex-Protected Snapshots** (SpawnState, PhaseState, GoldState, DecayState, CleanerState):
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

### Clock Scheduler

**Architecture**: Hybrid real-time/clock-based game loop with separate tickers:
- **Frame Ticker** (16ms): Real-time input, scoring, cursor movement, rendering
- **Clock Ticker** (50ms): Game logic phase transitions, spawn decisions

**Purpose**: Centralizes phase transitions on a predictable clock tick, preventing race conditions in inter-dependent mechanics (Gold→Decay→Cleaner flow).

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
- **Gold sequence state**: `GoldActive`, `GoldSequenceID`, `GoldStartTime`, `GoldTimeoutTime`
- **Decay timer state**: `DecayTimerActive`, `DecayNextTime`
- **Decay animation state**: `DecayAnimating`, `DecayStartTime`
- **Cleaner state**: `CleanerPending`, `CleanerActive`, `CleanerStartTime`
  - Cleaners run in parallel with main phase cycle (non-blocking)

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
- 50ms ticker running in dedicated goroutine
- Thread-safe start/stop with idempotent Stop()
- Tick counter for debugging and metrics
- Graceful shutdown on game exit

**Behavior**:
- Ticks every 50ms independently of frame rate
- **Phase transitions handled on clock tick**:
  - `PhaseGoldActive`: Check gold timeout → remove gold → start decay timer
  - `PhaseDecayWait`: Check decay ready → start decay animation
  - `PhaseDecayAnimation`: Handled by DecaySystem → return to PhaseNormal
  - `PhaseNormal`: Gold spawning handled by GoldSequenceSystem
- **Cleaner triggers handled on clock tick**:
  - Check `CleanerPending` → activate cleaners via CleanerSystem
  - Check `CleanerActive` + animation complete → deactivate cleaners and transition to PhaseDecayWait
  - Cleaners run in parallel with phase transitions (non-blocking)
  - After cleaner completion, decay timer starts automatically (maintains game flow cycle)
- **Critical**: Decay timer reads heat atomically at transition (no caching)

**Integration** (`cmd/vi-fighter/main.go`):
```go
// Create and start clock scheduler (runs in background goroutine)
clockScheduler := engine.NewClockScheduler(ctx)
clockScheduler.Start()
defer clockScheduler.Stop()

// Separate frame ticker for rendering
ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
defer ticker.Stop()
```

#### Architecture Benefits

**Separation of Concerns**:
- **Real-time layer** (16ms): User input, typing feedback, visual updates (no blocking)
- **Game logic layer** (50ms): Phase transitions, spawn decisions (can use mutex safely)

**Race Condition Prevention**:
- Phase transitions happen atomically on clock tick
- Heat snapshots taken at specific moments (not cached)
- State ownership model eliminates conflicting writes

**Testability**:
- Clock tick is deterministic with `MockTimeProvider`
- Phase transitions can be unit tested independently
- Integration tests can advance time precisely

**Performance**:
- Real-time input remains responsive (no clock blocking)
- Clock logic only runs 3× per frame (50ms vs 16ms)
- Mutex contention minimized (clock thread vs main thread)

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
  - `TestCleanerTrailCollisionLogic`: Trail-based collision detection verification
  - `TestNoSkippedCharacters`: Verification that truncation logic doesn't skip characters
  - `TestRapidPhaseTransitions`: Rapid phase transition stress test
  - `TestGoldSequenceIDIncrement`: Sequential ID generation
- All tests pass with `-race` flag
- Clock runs in goroutine, no memory leaks detected
- Concurrent phase reads/writes tested (20 goroutines)

### System Priorities
Systems execute in priority order (lower = earlier):
1. **ScoreSystem (10)**: Process user input, update score (highest priority for input)
2. **SpawnSystem (15)**: Generate new character sequences (Blue and Green only)
3. **GoldSequenceSystem (20)**: Manage gold sequence lifecycle and random placement
4. **DecaySystem (25)**: Apply character degradation and color transitions
5. **CleanerSystem (30)**: Process cleaner spawn requests (actual updates run concurrently)

**Important**: All priorities must be unique to ensure deterministic execution order. The priority values define the exact order in which systems process game state each frame.

### Spatial Indexing
- Primary index: `World.spatialIndex[y][x] -> Entity`
- Secondary index: `World.componentsByType[Type] -> []Entity`
- ALWAYS update spatial index on position changes
- ALWAYS remove from spatial index before entity destruction

## Component Hierarchy
```
Component (marker interface)
├── PositionComponent {X, Y int}
├── CharacterComponent {Rune, Style}
├── SequenceComponent {ID, Index, Type, Level}
├── GoldSequenceComponent {Active, SequenceID, StartTime, CharSequence, CurrentIndex}
├── FallingDecayComponent {Column, YPosition, Speed, Char, LastChangeRow}
├── CleanerComponent {Row, XPosition, Speed, Direction, TrailPositions, TrailMaxAge}
└── RemovalFlashComponent {X, Y, Char, StartTime, Duration}
```

### Sequence Types
- **Green**: Positive scoring, spawned by SpawnSystem, decays to Red
- **Blue**: Positive scoring with boost effect, spawned by SpawnSystem, decays to Green
- **Red**: Negative scoring (penalty), ONLY created through decay (not spawned directly)
- **Gold**: Bonus sequence, spawned randomly by GoldSequenceSystem after decay animation

## Rendering Pipeline

1. Clear dirty regions (when implemented)
2. Draw static UI (heat meter, line numbers)
3. Draw game entities (characters)
4. Draw overlays (ping, decay animation)
5. Draw cleaners with gradient trail effects
6. Draw removal flash effects
7. Draw cursor (topmost layer)

## System Coordination and Event Flow

### Complete Game Cycle
The game operates in a continuous cycle managed by three primary systems:

```
Gold Sequence → Completion/Timeout → Decay Timer → Decay Animation → Gold Sequence
     ↓                                      ↑                ↓
 (Optional)                                 |         (Always happens)
 Cleaner Trigger                            |       Characters decay levels
 (if heat maxed)                            |
     ↓                                      |
 Cleaner Animation → Completion ────────────┘
     (transitions to DecayWait, starts decay timer)
```

**Key State Transitions:**
- Gold completion/timeout → PhaseDecayWait (starts decay timer)
- Cleaner completion → PhaseDecayWait (starts decay timer)
- Both paths converge at decay timer, ensuring consistent game flow

### Event Sequencing

#### 1. Gold Sequence Phase
- **Activation**: Gold sequence spawns after decay animation completes
- **Duration**: 10 seconds (constants.GoldSequenceDuration)
- **Completion**: Either typed correctly or times out
- **Next Action**: Calls `DecaySystem.StartDecayTimer()`

**State Validation**:
- Gold can only spawn when NOT active
- Gold entities have unique sequence IDs
- Position conflicts with existing entities are avoided

#### 2. Decay Timer Phase
- **Activation**: Started when Gold sequence ends (completion or timeout)
- **Duration**: 60-10 seconds (based on heat percentage at Gold end time)
  - Formula: `60s - (50s * heatPercentage)`
  - Higher heat = faster decay
- **Purpose**: Creates breathing room between Gold sequences
- **Next Action**: Triggers decay animation when timer expires

**State Validation**:
- Timer only starts if not already running
- Timer calculation is atomic (based on heat at specific moment)
- Timer does not restart during active animation

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
- **Next Action**: Returns to Gold Sequence Phase

**State Validation**:
- Animation cannot start if already animating
- Each character decayed at most once per animation
- Falling entities properly cleaned up on completion

### Score System Integration

#### Gold Sequence Typing
When user types during active gold sequence:

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
CleanerSystem uses atomic operations for lock-free state:
- `isActive`: Cleaner animation active (atomic.Bool)
- `activationTime`: When cleaners were triggered (atomic.Int64)
- `activeCleanerCount`: Number of active cleaners (atomic.Int64)

**Benefits**:
- No lock contention for reads
- Fast state checks from render thread
- Concurrent updates without blocking

#### Gold System Synchronization
GoldSequenceSystem uses `sync.RWMutex` for all state:
- `active`: Gold sequence active state
- `sequenceID`: Current sequence identifier
- `startTime`: When sequence was spawned
- Cleaner trigger function reference

### State Transition Rules

#### Cleaner Phase Transitions
- **PhaseCleanerPending** → **PhaseCleanerActive**: When cleaners are activated
- **PhaseCleanerActive** → **PhaseDecayWait**: When cleaner animation completes
  - After cleaners finish (whether phantom or real), the system always starts the decay timer
  - This ensures proper game flow cycle: Cleaners → Decay Timer → Decay Animation → Gold → ...
  - Cleaners DO NOT return to PhaseNormal - they always transition to PhaseDecayWait
  - The `DeactivateCleaners()` function transitions to PhaseDecayWait and starts the decay timer

#### Invalid Transitions (Prevented)
- Gold spawning while Gold already active → Ignored
- Decay animation starting while already animating → Ignored
- Decay timer restarting during active animation → Blocked
- CleanerActive → Normal (old behavior, now invalid) → Must go through DecayWait
- Cleaners triggering while already active → Ignored (queued)

#### Valid Transitions
- Gold End → Decay Timer Start: Always allowed
- Decay Timer Expire → Animation Start: Atomic transition
- Animation Complete → Gold Spawn: Automatic, immediate

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
- **Channel-based Triggering**: Non-blocking spawn requests via buffered channel
- **Atomic State**: `atomic.Bool` and `atomic.Int64` for lock-free state checks
- **Mutex Protection**:
  - `stateMu` protects `cleanerDataMap` (cleaner position data)
  - `flashMu` protects `flashPositions` (flash effect tracking)
  - Locks held only during data structure updates (not during world access)
- **Frame-Coherent Rendering**: `GetCleanerSnapshots()` provides immutable snapshot
- **Memory Pool**: `sync.Pool` for trail slice allocation/deallocation

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
**Problem**: Original implementation had autonomous goroutine modifying entities concurrently with main loop

**Solution**:
- ✅ **Removed goroutine**: Updates now synchronous in main loop `Update()` method
- ✅ **Delta time**: Uses frame delta time, not independent timer
- ✅ **Atomic flags**: `isActive`, `activationTime`, `activeCleanerCount` for lock-free checks
- ✅ **Mutex protection**: `stateMu` for `cleanerDataMap`, `flashMu` for `flashPositions`
- ✅ **Snapshot rendering**: `GetCleanerSnapshots()` returns deep-copied data
- ✅ **Channel triggering**: Non-blocking spawn requests (doesn't block caller)

### Frame Coherence Strategy
**Rendering Thread Safety**:
1. Renderer calls `GetCleanerSnapshots()` once per frame
2. Method acquires `stateMu.RLock()` and copies all trail positions
3. Renderer uses snapshot data (no shared references)
4. Main loop updates `cleanerDataMap` under `stateMu.Lock()`
5. No data races: snapshot is fully independent copy

**Implementation**:
```go
// Renderer (terminal_renderer.go)
snapshots := cleanerSystem.GetCleanerSnapshots()  // Once per frame
for _, snapshot := range snapshots {
    // Use snapshot.TrailPositions (independent copy)
}

// Update (cleaner_system.go)
cs.stateMu.Lock()
cs.cleanerDataMap[entity].trailPositions = newTrail
cs.stateMu.Unlock()
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
4. Reuse allocated slices where possible
5. CleanerSystem updates synchronously with frame-accurate delta time

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

### Gold Sequence System
- **Trigger**: Spawns when decay animation completes
- **Position**: Random location avoiding cursor (NOT fixed center-top)
- **Length**: Fixed 10 alphanumeric characters (randomly generated)
- **Duration**: 10 seconds before timeout
- **Reward**: Fills heat meter to maximum on completion
- **Cleaner Trigger**: If heat is already at maximum when gold completed, triggers Cleaner animation
- **Behavior**: Typing gold chars does not affect heat/score directly

### Cleaner System
- **Trigger**: Activated when gold sequence completed while heat meter already at maximum
- **Update Model**: **Synchronous** - runs in main game loop, not in separate goroutine
- **Configuration**: Fully configurable via `constants.CleanerConfig` struct
  - **Animation Duration**: Time to traverse screen (default: 1 second)
  - **Speed**: Characters/second (default: auto-calculated from duration and screen width)
  - **Trail Length**: Number of trail positions (default: 10)
  - **Trail Fade Time**: Duration for complete trail fade (default: 0.3 seconds)
  - **Trail Fade Curve**: Linear or exponential interpolation (default: linear)
  - **Max Concurrent Cleaners**: Limit simultaneous cleaners (default: 0/unlimited)
  - **Character**: Unicode block character (default: '█')
  - **Flash Duration**: Removal flash duration in ms (default: 150)
- **Behavior**: Sweeps across rows containing Red characters, removing them on contact
- **Phantom Cleaners**: System activates even when no Red characters exist to ensure proper phase transitions (no visual cleaners spawned, but animation timer runs normally)
- **Direction**: Alternating - odd rows sweep L→R, even rows sweep R→L
- **Selectivity**: Only destroys Red characters, leaves Blue/Green untouched
- **Animation**: Configurable block character with fade trail effect
- **Collision Detection** (Comprehensive range-based approach):
  - Checks ALL integer positions between consecutive trail points
  - Prevents position gaps when cleaner moves >1 char/frame (e.g., 8.84→10.12 checks 8, 9, 10)
  - Mathematical basis: 80 chars/sec × 16ms frame = 1.28 chars/frame movement
  - Uses `math.Min/Max` for bidirectional range checking (works for both L→R and R→L)
  - Single `checkTrailCollisions()` method with comprehensive coverage
  - Negligible performance impact: at most 2-3 extra positions per trail segment
- **Visual Effects**:
  - Pre-calculated color gradients avoid per-frame calculations
  - Configurable trail length with smooth opacity falloff
  - Configurable removal flash effect when Red characters destroyed
  - Flash cleanup: Automatic removal of expired flash entities
- **Thread Safety**:
  - Non-blocking spawn requests via buffered channel
  - Atomic state management for lock-free activation/deactivation checks
  - Mutex protection for cleanerDataMap and flashPositions
  - Trail slices allocated from `sync.Pool` for efficient memory reuse
  - Frame-coherent snapshots for renderer (GetCleanerSnapshots)
- **Performance**:
  - Synchronous updates eliminate goroutine overhead
  - Pre-calculated color gradients avoid per-frame calculations
  - Minimal allocations in update loop (gradient array vs dynamic color creation)
  - Reduced collision detection overhead (single method)
  - Typical update time: < 1ms for 24 cleaners

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
The cleaner system tests have been reorganized into focused files:

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
  - TestNoRaceCleanerConcurrentRenderUpdate: Concurrent cleaner updates and rendering
  - TestNoRaceRapidCleanerCycles: Rapid cleaner activation/deactivation
  - TestNoRaceCleanerStateAccess: Concurrent reads/writes to cleaner state
  - TestNoRaceFlashEffectManagement: Concurrent flash creation/cleanup
  - TestNoRaceCleanerPoolAllocation: Thread-safe trail slice pool allocation
  - TestNoRaceDimensionUpdate: Dimension updates during active cleaners
  - TestNoRaceCleanerAnimationCompletion: Animation completion race conditions
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
- **Position Updates**: `BenchmarkCleanerUpdate`, `BenchmarkCleanerUpdateSync`
- **Flash Effects**: `BenchmarkFlashEffectCreation`, `BenchmarkFlashEffectCleanup`
- **Gold Sequence Operations**: `BenchmarkGoldSequenceSpawn`, `BenchmarkGoldSequenceCompletion`
- **Concurrent Operations**: `BenchmarkConcurrentCleanerOperations`
- **Full Pipeline**: `BenchmarkCompleteGoldCleanerPipeline`
- **Performance Target**: < 1ms for 24 cleaners (synchronous update)

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
   - Concurrent spawn requests
   - Active state checks during cleanup
   - Trail slice pool allocation/deallocation
   - Screen buffer scanning during modifications

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