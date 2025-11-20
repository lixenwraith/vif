# State Management

## State Ownership Model

**GameState** (`engine/game_state.go`) centralizes game state with clear ownership boundaries between real-time and clock-tick state.

### Real-Time State (Lock-Free Atomics)

Updated immediately on user input/spawn events, read by all systems:

- **Heat** (`atomic.Int64`) - Current heat value (typing momentum)
- **Score** (`atomic.Int64`) - Player score
- **Cursor Position** (`atomic.Int32`) - CursorX, CursorY for spawn exclusion zone
- **Color Counters** (6× `atomic.Int64`) - Blue/Green × Bright/Normal/Dark tracking
- **Boost State** (`atomic.Bool`, `atomic.Int64`, `atomic.Int32`) - Enabled, EndTime, Color
- **Visual Feedback** - CursorError, ScoreBlink, PingGrid (atomic)
- **Sequence ID** (`atomic.Int64`) - Thread-safe ID generation
- **Pause State** (`atomic.Bool`, `atomic.Int64`) - IsPaused flag, PauseStartTime, TotalPauseDuration for time adjustment

**Why Atomic**: These values are accessed on every frame and every keystroke. Atomics provide:
- Lock-free reads (no contention on render or input threads)
- Immediate consistency (typing feedback feels instant)
- Race-free updates without blocking

### Clock-Tick State (Mutex Protected)

Updated during scheduled game logic ticks (50ms intervals), read by all systems:

- **Spawn Timing** (`sync.RWMutex`) - LastTime, NextTime, RateMultiplier
- **Screen Density** - EntityCount, ScreenDensity, SpawnEnabled
- **6-Color Limit** - Enforced via atomic color counter checks
- **Game Phase State** - CurrentPhase, PhaseStartTime
- **Gold Sequence State** - GoldActive, GoldSequenceID, GoldStartTime, GoldTimeoutTime
- **Decay Timer State** - DecayTimerActive, DecayNextTime
- **Decay Animation State** - DecayAnimating, DecayStartTime
- **Cleaner State** - CleanerPending, CleanerActive, CleanerStartTime

**Why Mutex**: These values change infrequently (every 2-50 seconds) and require:
- Consistent multi-field reads (spawn timing snapshot, phase state)
- Atomic state transitions (spawn rate adaptation, phase changes)
- Blocking is acceptable (not on hot path)

## State Access Patterns

### Through GameContext

All systems hold a reference to `*engine.GameContext` and access state via `ctx.State`:

```go
// Real-time (typing): Direct atomic access
ctx.State.AddHeat(1)                    // No lock, instant
ctx.State.AddColorCount(Blue, Bright, 1) // Atomic increment

// Clock-tick (spawn): Snapshot pattern
snapshot := ctx.State.ReadSpawnState()  // RLock, consistent view
if ctx.State.ShouldSpawn() {            // RLock, check timing
    // ... spawn logic ...
    ctx.State.UpdateSpawnTiming(now, next) // Lock, update state
}

// Render: Safe concurrent reads
heat := ctx.State.GetHeat()             // Atomic load
snapshot := ctx.State.ReadSpawnState()  // RLock, no blocking

// Dimensions: Read directly from GameContext (no caching)
width := ctx.GameWidth   // Always current
height := ctx.GameHeight // Always current
```

### GameContext Role

- GameContext is **NOT** a delegation layer for State
- It provides direct access to GameState via the `State` field
- Systems read dimensions directly from `ctx.GameWidth` and `ctx.GameHeight`
- No local dimension caching - always read current values
- Input-specific methods (cursor position, mode, motion commands) remain on GameContext

## Snapshot Pattern

The **snapshot pattern** is the primary mechanism for safely reading multi-field state across concurrent goroutines. All mutex-protected state is accessed through immutable snapshot structures that guarantee internally consistent views.

### Core Principles

1. **Immutability**: Snapshots are value copies, never references
2. **Atomicity**: All fields in a snapshot come from the same moment in time
3. **No Partial Reads**: Readers never see half-updated state
4. **Lock-Free After Capture**: Once a snapshot is taken, no locks are held

### Available Snapshot Types

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

### Usage Examples

**✅ GOOD: Snapshot Pattern**
```go
// Read once, use multiple times
snapshot := ctx.State.ReadSpawnState()
if snapshot.Enabled && snapshot.EntityCount < snapshot.MaxEntities {
    density := snapshot.ScreenDensity
    rate := snapshot.RateMultiplier
    // All fields guaranteed consistent
}
```

**❌ BAD: Multiple Individual Reads**
```go
// Race condition - fields may change between reads
if ctx.State.ShouldSpawn() {                    // RLock #1
    count := ctx.State.ReadSpawnState().EntityCount  // RLock #2
    density := ctx.State.ReadSpawnState().ScreenDensity  // RLock #3
    // EntityCount and ScreenDensity may be from different updates!
}
```

**✅ GOOD: Atomic Snapshots**
```go
// Atomic fields read together
heat, score := ctx.State.ReadHeatAndScore()
if heat > 0 && score > 0 {
    // heat and score are consistent
}
```

**❌ BAD: Separate Atomic Reads**
```go
// heat and score may be from different moments
heat := ctx.State.GetHeat()   // Atomic read #1
score := ctx.State.GetScore() // Atomic read #2
// If another goroutine updates both, we might see heat=new, score=old
```

### System Usage Map

| System | Snapshot Types Used | Purpose |
|--------|-------------------|---------|
| SpawnSystem | SpawnState, ColorCounts, CursorPosition | Check spawn conditions, cursor exclusion zone |
| ScoreSystem | BoostState, GoldState, HeatAndScore | Process typing, update heat/score |
| GoldSequenceSystem | GoldState, PhaseState | Manage gold sequence lifecycle |
| DecaySystem | DecayState, PhaseState | Manage decay timer and animation |
| CleanerSystem | CleanerState, PhaseState | Manage cleaner lifecycle |
| NuggetSystem | CursorPosition | Check cursor exclusion zone for spawn |
| Renderer | All snapshot types | Render game state without blocking game loop |
| ClockScheduler | PhaseState, GoldState, DecayState | Manage phase transitions |

### Concurrency Guarantees

**1. Mutex-Protected Snapshots** (SpawnState, PhaseState, GoldState, DecayState, CleanerState):
- Use `RLock` to read state atomically
- All fields copied before returning
- Multiple concurrent readers allowed
- Writers block only during actual state modification

**2. Atomic Field Snapshots** (ColorCounts, CursorPosition, BoostState, HeatAndScore):
- No locks required
- Multiple atomic loads in sequence
- Still provides consistent view (atomic loads are sequentially consistent)
- Trade-off: Very rare possibility of seeing mixed state between loads (acceptable for these use cases)

**3. Immutability After Capture**:
- All snapshots are value types (structs)
- Modifying snapshot fields doesn't affect GameState
- Safe to pass snapshots across goroutine boundaries
- No memory aliasing issues

## Testing

### Unit Tests (`engine/game_state_test.go`)

- `TestSnapshotConsistency`: Verifies snapshots remain consistent under rapid state changes (10 concurrent readers)
- `TestNoPartialReads`: Ensures snapshots never show partial state updates (5 concurrent readers)
- `TestSnapshotImmutability`: Confirms snapshots are immutable value copies
- `TestAllSnapshotTypesConcurrent`: Tests all snapshot types under concurrent access (5 concurrent readers)
- `TestAtomicSnapshotConsistency`: Verifies atomic field snapshots (10 concurrent readers, 1000 rapid updates)

### Integration Tests (`systems/race_condition_comprehensive_test.go`)

- `TestSnapshotConsistencyUnderRapidChanges`: Multi-writer (3) + multi-reader (10) snapshot consistency
- `TestSnapshotImmutabilityWithSystemUpdates`: Snapshot immutability during active system modifications
- `TestNoPartialSnapshotReads`: Verifies no partial reads during rapid updates (8 concurrent readers)
- `TestPhaseSnapshotConsistency`: Phase snapshot consistency during rapid transitions (5 concurrent readers)
- `TestMultiSnapshotAtomicity`: Multiple snapshot types taken in rapid succession (10 concurrent readers)

All tests pass with `-race` flag (no data races detected).

## Color Counter System

### Atomic Operations

The 6-color limit system uses atomic operations for thread-safe character tracking:

```go
// Increment counter when spawning
ctx.State.AddColorCount(Blue, Bright, 1)

// Decrement counter when typing
ctx.State.AddColorCount(Blue, Bright, -1)

// Read counters for spawn eligibility
snapshot := ctx.State.ReadColorCounts()
if snapshot.BlueBright == 0 {
    // Blue Bright slot available, can spawn
}
```

### Counter Synchronization

Atomic counters are updated by multiple systems:
- **SpawnSystem**: Increments when blocks placed
- **ScoreSystem**: Decrements when characters typed
- **DecaySystem**: Updates during decay transitions (decrement old level, increment new level)

All operations are race-free and thread-safe.

### Testing

- `systems/integration_test.go`: Tests color counter accuracy across systems
- `systems/race_counters_test.go`: Tests concurrent color counter updates
- All tests verify counters match actual on-screen character counts

---

[← Back to Architecture Index](architecture-index.md)
