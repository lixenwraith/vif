# Concurrency Model

## Main Architecture

- **Main game loop**: Single-threaded ECS updates (16ms frame tick)
- **Input events**: Goroutine → channel → main loop
- **Clock scheduler**: Separate goroutine for phase transitions (50ms tick)
- **All systems**: Run synchronously in main game loop, no autonomous goroutines

## CleanerSystem Synchronous Model

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

## Shared State Synchronization

### Color Counters (Atomic)

```go
// Atomic operations for thread-safe updates
ctx.State.AddColorCount(Blue, Bright, 1)  // SpawnSystem
ctx.State.AddColorCount(Blue, Bright, -1) // ScoreSystem

// Multiple systems update counters concurrently:
```

- **SpawnSystem**: Increments counters when blocks placed
- **ScoreSystem**: Decrements counters when characters typed
- **DecaySystem**: Updates counters during decay transitions
- All counter operations are race-free and thread-safe

### GameState (Mutex + Atomics)

- **Atomic fields**: Heat, Score, Cursor, Color Counters, Boost State
- **Mutex-protected fields**: Spawn timing, Phase state, Gold state, Decay state, Cleaner state
- Uses `sync.RWMutex` for phase state and timing
- Snapshot pattern for consistent multi-field reads

### World (Internal Locking)

- **Entity/component access**: Thread-safe via internal locking
- **Spatial index**: Protected by World's internal mutexes
- **Component queries**: Safe from concurrent goroutines

### NuggetSystem (Atomic CAS)

```go
// Single nugget invariant enforced via atomic CompareAndSwap
func (s *NuggetSystem) ClearActiveNuggetIfMatches(entityID uint64) bool {
    return s.activeNugget.CompareAndSwap(entityID, 0)
}
```

- **Active nugget reference**: `atomic.Uint64` for lock-free access
- **CAS operations**: Prevent race conditions in collection/decay
- **Spawn timing**: Mutex-protected for consistent intervals

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

### NuggetSystem Race Prevention Strategy

**Problem**: Multiple systems (ScoreSystem, DecaySystem) may destroy nuggets concurrently

**Solution**:
- ✅ **Atomic CAS**: `CompareAndSwap` ensures only one destruction clears reference
- ✅ **Entity ID matching**: CAS only succeeds if entity ID matches current active nugget
- ✅ **Verification logic**: `Update()` uses CAS to clear stale references
- ✅ **Single nugget invariant**: At most one nugget active at any time

**Example Race Scenario (Prevented)**:
```
Thread A: Destroys nugget E1, calls ClearActiveNuggetIfMatches(E1)
Thread B: Spawns new nugget E2, sets activeNugget = E2
Thread A: CAS fails because activeNugget is E2, not E1 - E2 remains active ✓
```

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

## Testing for Race Conditions

### Test Suite Organization

All tests must pass with `go test -race`

**Dedicated race tests**:
- `systems/cleaner_race_test.go` - Cleaner system race conditions
- `systems/boost_race_test.go` - Boost system race conditions
- `systems/race_counters_test.go` - Color counter race conditions
- `systems/race_snapshots_test.go` - Snapshot consistency race conditions
- `systems/race_content_test.go` - Content system race conditions
- `systems/nugget_edge_cases_test.go` - Nugget CAS race conditions

**Integration tests**: Verify concurrent scenarios across systems

**Benchmarks**: Validate performance impact of synchronization

### Spatial Transaction System Race Prevention Strategy

**Problem**: `UpdateSpatialIndex()` overwrites without checking, causing "phantom entities"

**Solution**:
- ✅ **Spatial transactions**: Atomic operations for move, spawn, destroy
- ✅ **Collision detection**: Check for existing entities before placement
- ✅ **Single lock commit**: All operations applied under one mutex
- ✅ **MoveEntitySafe()**: Convenience method for common case
- ✅ **ValidateSpatialIndex()**: Debug helper to detect inconsistencies

**Implementation**:
```go
// Safe move with collision detection
result := world.MoveEntitySafe(entity, oldX, oldY, newX, newY)
if result.HasCollision {
    // Handle collision atomically
    handleCollision(result.CollidingEntity)
}

// Or use explicit transaction for multiple operations
tx := world.BeginSpatialTransaction()
tx.Move(entity1, oldX1, oldY1, newX1, newY1)
tx.Move(entity2, oldX2, oldY2, newX2, newY2)
tx.Commit() // All moves applied atomically
```

**Example Race Scenario (Prevented)**:
```
Thread A: UpdateSpatialIndex(E1, 5, 5)    // Old way - overwrites
Thread B: UpdateSpatialIndex(E2, 5, 5)    // E1 becomes phantom
Result:   E1 exists but not in spatial index ✗

Thread A: MoveEntitySafe(E1, 0, 0, 5, 5)  // New way - collision detected
Thread B: MoveEntitySafe(E2, 1, 1, 5, 5)  // Returns HasCollision=true
Result:   Both entities tracked correctly ✓
```

### Common Race Conditions to Test

#### 1. Cleaner System
- Concurrent spawn requests
- Active state checks during cleanup
- Trail slice pool allocation/deallocation
- Screen buffer scanning during modifications

#### 2. Gold System
- Spawn during active sequence
- Completion during spawn
- Cleaner triggering race conditions

#### 3. Color Counters
- Concurrent increment/decrement (spawn vs. score vs. decay)
- Read during modification
- Negative counter prevention

#### 4. Spatial Index
- Concurrent reads during entity destruction
- Position updates during queries
- Entity removal during iteration
- **Phantom entities from overwritten spatial index (fixed via transactions)**
- **Collision detection races (fixed via atomic transaction commit)**

#### 5. Nugget System
- Concurrent collection and decay destruction
- Double destruction attempts
- Stale reference clearing after new spawn
- Spawn position validation concurrent with cursor movement

## Debug Helpers and Race Detection Tools

Located in `systems/test_helpers.go`:

### RaceDetectionLogger

Event logging for concurrent execution analysis:

```go
logger := NewRaceDetectionLogger(enabled bool)
logger.Log(goroutine, operation, details string)  // Records timestamped events
events := logger.GetEvents()                      // Retrieves all logged events
logger.DumpEvents(filename string)                // Exports to file for analysis
```

**Features**:
- Atomic event counter (thread-safe ID generation)
- Mutex-protected event storage
- Optional verbose logging via `VERBOSE_RACE_LOG=1` environment variable
- Timestamp precision for ordering concurrent operations

### ConcurrencyMonitor

Operation tracking and anomaly detection:

```go
monitor := NewConcurrencyMonitor()
monitor.StartOperation(operation string)  // Track operation start
monitor.EndOperation(operation string)    // Track operation end
stats := monitor.GetStats()               // Get concurrency statistics
```

**Tracks**:
- Active operations per type (current concurrent count)
- Maximum concurrent operations observed
- Total operation count
- Anomaly detection (e.g., EndOperation without StartOperation)

### AtomicStateValidator

State consistency validation:

```go
validator := NewAtomicStateValidator()
validator.ValidateCleanerState(isActive, activationTime, lastUpdateTime)
validator.ValidateCounterState(colorType, level string, count int64)
validator.ValidateGoldState(isActive bool, sequenceID int)
violations := validator.GetViolations()
```

**Validates**:
- Cleaner state consistency (active state vs. timestamps)
- Color counter non-negativity
- Gold sequence state invariants
- Nugget single instance invariant

### EntityLifecycleTracker

Memory leak detection:

```go
tracker := NewEntityLifecycleTracker()
tracker.TrackCreate(entityID uint64)      // Record entity creation
tracker.TrackDestroy(entityID uint64)     // Record entity destruction
leaked := tracker.DetectLeaks()           // Find entities never destroyed
stats := tracker.GetStats()               // Creation/destruction statistics
```

### Usage in Tests

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
}
```

## Why These Tools Matter

The vi-fighter architecture uses a hybrid approach:
- **Real-time atomic updates** (heat, score, cursor) - lock-free, high frequency
- **Clock-tick mutex updates** (spawn, phase, gold, decay) - locked, lower frequency

Race detection tools help verify:
1. Atomics are never mixed with non-atomic access
2. Mutexes are properly paired (RLock/RUnlock, Lock/Unlock)
3. State invariants hold even under concurrent access
4. No operations are lost or double-counted
5. Memory is properly released (no entity leaks)

All tests using these tools must pass with `go test -race` flag.

## Continuous Integration Requirements

- All tests must pass with `-race` flag
- No memory leaks detected in leak tests
- Benchmarks should not regress >10% between commits
- Test coverage should not decrease

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
- Use `sync.Pool` for frequently allocated/deallocated objects

### Lock Contention

- Minimize time spent holding locks
- Prefer RLock over Lock when possible
- Release locks before calling into other systems
- Use atomic operations for simple counters/flags

---

[← Back to Architecture Index](architecture-index.md)
