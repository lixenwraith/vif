# Testing Strategy

## Test Suite Organization

The test suite has been reorganized into smaller, focused files for better maintainability and clarity. Large test files (>30KB) have been split into logical groups.

## Unit Tests

### Component Tests
Validate individual component data structures

### System Tests
Test system logic in isolation

### Helper Tests
Verify utility functions and time providers

### Motion Tests

The motion tests have been reorganized from a single large file into focused test files:

**Located in `modes/`**:
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
- **count_aware_commands_test.go**: Count-aware command state management
- **input_test.go**: Input handling and mode switching
- **delete_operator_test.go**: Delete operation tests

### Heat Display Tests

Located in `render/heat_display_test.go`:
- **TestHeatPercentageMapping**: Heat display correctly maps percentage to 0-10 range
- **TestHeatDisplayBounds**: Display heat is always within 0-10 range
- **TestHeatDisplayEdgeCases**: Boundary conditions in heat calculation
- **TestHeatDisplayGranularity**: Correct granularity at different heat levels

### Cleaner System Tests

The cleaner system tests have been reorganized into focused files:

**Located in `systems/`**:
- **cleaner_activation_test.go**: Trigger conditions and activation logic
- **cleaner_movement_test.go**: Movement and direction logic
- **cleaner_collision_test.go**: Collision detection and character removal
- **cleaner_lifecycle_test.go**: Animation lifecycle and resource management
- **cleaner_flash_test.go**: Visual flash effects
- **cleaner_test_helpers.go**: Shared helper functions

### Spawn System Tests

The spawn system tests have been reorganized into focused files:

**Located in `systems/`**:
- **spawn_content_test.go**: Content management and filtering
- **spawn_colors_test.go**: Color counter and availability
- **spawn_placement_test.go**: Line placement and positioning
- **spawn_blocks_test.go**: Block grouping and spawning

### Nugget System Tests

**Located in `systems/`**:
- **nugget_typing_test.go**: Collection mechanics (heat gain, no score effect)
- **nugget_jump_test.go**: Tab jump mechanic (score cost, position update)
- **nugget_decay_test.go**: Decay destruction and respawn
- **nugget_edge_cases_test.go**: Race conditions, CAS correctness, single nugget invariant

## Integration Tests

Located in `systems/integration_test.go` and `engine/integration_test.go`:
- **TestDecaySystemCounterUpdates**: Color counter updates during decay
- **TestDecaySystemColorTransitionWithCounters**: Color transitions with atomic counter updates
- **TestScoreSystemCounterDecrement**: Counter decrements when typing characters
- **TestScoreSystemDoesNotDecrementRedCounter**: Red characters don't affect color counters
- **TestCompleteGameCycle**: Full Normal→Gold→DecayWait→DecayAnim→Normal cycle
- **TestGoldCompletionBeforeTimeout**: Early gold completion handling
- **TestConcurrentPhaseReadsDuringTransitions**: 20 readers × concurrent access test

## Race Condition Tests

The race condition tests have been reorganized into focused files:

**Located in `systems/`**:
- **race_content_test.go**: Content system race conditions
  - TestConcurrentContentRefresh
  - TestRenderWhileSpawning
  - TestContentSwapDuringRead
  - TestStressContentSystem
- **race_counters_test.go**: Color counter race conditions
  - TestConcurrentColorCounterUpdates
- **race_snapshots_test.go**: Snapshot consistency race conditions
  - TestSnapshotConsistencyUnderRapidChanges
  - TestSnapshotImmutabilityWithSystemUpdates
  - TestNoPartialSnapshotReads
  - TestPhaseSnapshotConsistency
  - TestMultiSnapshotAtomicity
- **cleaner_race_test.go**: Cleaner system race conditions
  - TestNoRaceCleanerConcurrentRenderUpdate
  - TestNoRaceRapidCleanerCycles
  - TestNoRaceCleanerStateAccess
  - TestNoRaceFlashEffectManagement
  - TestNoRaceCleanerPoolAllocation
  - TestNoRaceDimensionUpdate
  - TestNoRaceCleanerAnimationCompletion
- **boost_race_test.go**: Boost system race conditions
  - TestBoostRapidToggle
  - TestBoostConcurrentRead
  - TestBoostExpirationRace
  - TestAllAtomicStateAccess
- **nugget_edge_cases_test.go**: Nugget CAS race conditions
  - TestNuggetConcurrentClearAttempts (10 goroutines)

## Deterministic Tests

Located in `systems/cleaner_deterministic_test.go`:
- **TestDeterministicCleanerLifecycle**: Frame-by-frame cleaner behavior verification
- **TestDeterministicCleanerTiming**: Exact animation duration validation
- **TestDeterministicCollisionDetection**: Predictable collision timing
- Uses `MockTimeProvider` for precise time control

## Benchmark Tests

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

## Running Tests

### Standard Test Run

```bash
# Run all tests
go test ./... -v

# Run modes tests only
go test ./modes/... -v

# Run systems tests only
go test ./systems/... -v
```

### Race Detector (CRITICAL for concurrency validation)

```bash
# Run all tests with race detection
go test ./... -race -v

# Run modes tests with race detection
go test ./modes/... -race -v

# Run systems tests with race detection
go test ./systems/... -race -v
```

### Benchmarks

```bash
go test ./systems/... -bench=. -benchmem
```

### Specific Test Categories

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

# Nugget tests only
go test ./systems/... -run TestNugget -v -race

# Memory leak detection (long running)
go test ./systems/... -run TestMemoryLeak -v
```

### Debug Logging

Set environment variable for verbose race logging:

```bash
VERBOSE_RACE_LOG=1 go test ./systems/... -race -v
```

## Test Coverage Goals

- **Unit Tests**: >80% coverage for core systems
- **Integration Tests**: All critical concurrent scenarios
- **Race Tests**: All atomic operations and shared state access
- **Benchmarks**: All performance-critical paths

## Testing Patterns

### Using Test Helpers

```go
// Create simulation screen
screen := tcell.NewSimulationScreen("utf-8")

// Create mock time provider
timeProvider := engine.NewMockTimeProvider(time.Now())

// Create game context
ctx := engine.NewGameContext(screen)
```

### Frame-by-Frame Simulation

For time-dependent tests (decay, cleaners, nuggets):

```go
dt := 0.1 // 100ms per frame
maxTime := 5.0 // Maximum 5 seconds
for elapsed := dt; elapsed < maxTime; elapsed += dt {
    system.Update(dt, world)

    // Check condition, break early if met
    if conditionMet {
        break
    }
}
```

### Concurrent Testing

For race condition tests:

```go
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        // Concurrent operation
    }()
}
wg.Wait()

// Verify results
if validator.HasViolations() {
    t.Errorf("Race condition detected: %v", validator.GetViolations())
}
```

### Snapshot Testing

For state consistency:

```go
snapshot := ctx.State.ReadSpawnState()
// Use snapshot fields
if snapshot.Enabled && snapshot.EntityCount < snapshot.MaxEntities {
    // Consistent view guaranteed
}
```

## Test Organization Best Practices

1. **One Test File Per System**: Each system has its own test file(s)
2. **Focused Test Files**: Large test files split by functionality (activation, movement, collision, etc.)
3. **Shared Helpers**: Common test utilities in `_test_helpers.go` files
4. **Table-Driven Tests**: Use table-driven tests for multiple similar scenarios
5. **Clear Test Names**: Test names clearly describe what is being tested
6. **Race Detection**: All tests must pass with `-race` flag

## Continuous Integration

All tests must pass before merging:
- `go test ./... -race` - No data races
- `go test ./... -v` - All tests pass
- `go test ./systems/... -bench=.` - No performance regressions
- Test coverage maintained or improved

---

[← Back to Architecture Index](architecture-index.md)
