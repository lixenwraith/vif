# Systems & Coordination

## System Priorities

Systems execute in priority order (lower number = earlier execution):

1. **ScoreSystem (10)** - Process user input, update score/heat, clear visual feedback timeouts (highest priority for responsiveness)
2. **SpawnSystem (15)** - Generate new character sequences (Blue and Green only)
3. **NuggetSystem (18)** - Manage nugget spawn and lifecycle
4. **GoldSequenceSystem (20)** - Manage gold sequence lifecycle and random placement
5. **DrainSystem (22)** - Manage drain entity lifecycle, movement, and collisions
6. **DecaySystem (25)** - Apply character degradation and color transitions
7. **CleanerSystem (30)** - Process cleaner spawn requests (actual updates run concurrently)

**Important**: All priorities must be unique to ensure deterministic execution order. The priority values define the exact order in which systems process game state each frame.

## Clock Scheduler

### Architecture

Hybrid real-time/clock-based game loop with separate tickers:
- **Frame Ticker** (16ms): Real-time input, scoring, cursor movement, rendering
- **Clock Ticker** (50ms): Game logic phase transitions, spawn decisions

**Purpose**: Centralizes phase transitions on a predictable clock tick, preventing race conditions in inter-dependent mechanics (Gold→Decay→Cleaner flow).

### GamePhase State Machine

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
- `CurrentPhase` (`GamePhase`) - Current game phase (mutex protected)
- `PhaseStartTime` (`time.Time`) - When current phase started
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

### ClockScheduler Implementation

**Infrastructure** (`engine/clock_scheduler.go`):
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

### Architecture Benefits

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

## System Coordination and Event Flow

### Complete Game Cycle

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
  - **Nuggets destroyed on contact** (triggers respawn after 5 seconds)
- **Next Action**: Returns to Gold Sequence Phase

**State Validation**:
- Animation cannot start if already animating
- Each character decayed at most once per animation
- Falling entities properly cleaned up on completion

### Score System Integration

#### Update Method - Visual Feedback Timeouts
The ScoreSystem.Update() method runs every frame to clear expired visual feedback:

```
ScoreSystem.Update():
  1. Check cursor error flash timeout (200ms using Game Time)
     - If expired: Clear cursor error state
  2. Check score blink timeout (200ms using Game Time)
     - If expired: Clear score blink state
```

**Key Behavior**:
- Uses Game Time (`ctx.TimeProvider.Now()`), so timeouts freeze during pause
- Ensures visual feedback (red flash, score blink) pauses when game is paused
- Runs on every frame tick (16ms) for responsive visual updates
- Constants defined in `constants/ui.go`: `ErrorCursorTimeout`, `ScoreBlinkTimeout`

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

#### Nugget Collection
When user types on nugget position:

```
ScoreSystem.handleNuggetCollection():
  1. Calculate heat increase (10% of max heat, minimum 1)
  2. Add heat to game state (atomic operation)
  3. Destroy nugget entity (SafeDestroyEntity)
  4. Clear active nugget reference (atomic CAS)
  5. Move cursor right
  6. No score change, no error effects
```

**Key Behavior**:
- Silent collection (no visual/audio feedback)
- Heat gain not affected by boost multiplier
- Triggers automatic respawn after 5 seconds

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

#### Atomic Operations (NuggetSystem)
NuggetSystem uses atomic operations for single nugget invariant:
- `activeNugget`: Entity ID of current nugget (atomic.Uint64)
- `nuggetID`: Unique ID counter (atomic.Int32)

**Single Nugget Invariant**:
- Uses `CompareAndSwap` (CAS) to prevent race conditions
- Ensures at most one nugget active at any time
- ClearActiveNuggetIfMatches prevents clearing wrong nugget

#### Gold System Synchronization
GoldSequenceSystem uses `sync.RWMutex` for all state:
- `active`: Gold sequence active state
- `sequenceID`: Current sequence identifier
- `startTime`: When sequence was spawned
- Cleaner trigger function reference

### State Transition Rules

#### Valid Transitions
- Gold End → Decay Timer Start: Always allowed
- Decay Timer Expire → Animation Start: Atomic transition
- Animation Complete → Gold Spawn: Automatic, immediate
- Nugget Collection → Respawn (5s): Atomic reference clear

#### Invalid Transitions (Prevented)
- Gold spawning while Gold already active → Ignored
- Decay animation starting while already animating → Ignored
- Decay timer restarting during active animation → Blocked
- CleanerActive → Normal (old behavior, now invalid) → Must go through DecayWait
- Cleaners triggering while already active → Ignored (queued)
- Multiple nuggets spawning simultaneously → Atomic CAS prevents

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

nuggetSystem.GetSystemState()
// Returns: "Nugget[active, entityID=42]"
// or: "Nugget[inactive, nextSpawn=2.3s]"
```

**Usage**: Call during test failures or production debugging to understand system state.

## Testing

### Clock Scheduler Tests (`engine/clock_scheduler_test.go`)
- `TestClockSchedulerBasicTicking`: Tick count increment verification
- `TestClockSchedulerConcurrentTicking`: Concurrent goroutine safety
- `TestClockSchedulerStopIdempotent`: Multiple Stop() calls safety
- `TestClockSchedulerTickRate`: 50ms tick rate verification
- `TestPhaseTransitions`: Phase transition logic validation
- `TestConcurrentPhaseReads`: Concurrent phase access safety

### Integration Tests (`engine/integration_test.go`)
- `TestCompleteGameCycle`: Full Normal→Gold→DecayWait→DecayAnim→Normal cycle
- `TestGoldCompletionBeforeTimeout`: Early gold completion handling
- `TestConcurrentPhaseReadsDuringTransitions`: 20 readers × concurrent access test
- `TestPhaseTimestampConsistency`: Timestamp accuracy verification
- `TestPhaseDurationCalculation`: Duration calculation accuracy
- `TestCleanerTrailCollisionLogic`: Trail-based collision detection verification
- `TestNoSkippedCharacters`: Verification that truncation logic doesn't skip characters
- `TestRapidPhaseTransitions`: Rapid phase transition stress test
- `TestGoldSequenceIDIncrement`: Sequential ID generation

All tests pass with `-race` flag, no memory leaks detected, concurrent phase reads/writes tested (20 goroutines).

---

[← Back to Architecture Index](architecture-index.md)
