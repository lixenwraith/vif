# Vi-Fighter Test Files

## Test Suite Organization

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

#### Decay System Tests
The decay system tests validate the stateless decay architecture with swept collision detection:

**Located in `systems/`:**
- **decay_system_test.go**: Core decay system functionality
  - **createTestContext()**: Helper to create robust test context with fixed dimensions (80×30, gameHeight=25)
    - Uses `tcell.NewSimulationScreen("UTF-8")` with explicit SetSize
    - Forces dimensions to ensure physics bounds checks don't fail
    - No longer relies on brittle screen initialization
  - **TestDecaySweptCollisionNoTunneling**: Verifies swept collision prevents tunneling
    - Creates blue sequences at multiple rows (0, 2, 4, 6)
    - Spawns single falling entity with high speed (20 rows/sec) at column 10
    - Simulates physics with 16ms time steps (200 steps to traverse 25 rows)
    - Verifies all entities were decayed (no tunneling occurred)
  - **TestDecayCoordinateLatchPreventsReprocessing**: Verifies coordinate latch prevents hitting same cell twice
    - Creates blue sequence at row 5, column 10
    - Spawns falling entity just above target (Y=4.8)
    - Step 1: Move from 4.8 to 5.0 (should hit)
    - Step 2: Move from 5.0 to 5.2 (should NOT hit due to latch)
    - Verifies entity was hit exactly once
  - **TestDecayFrameDeduplicationMap**: Verifies processedGridCells prevents double-hits in same frame
    - Creates blue sequence at target position
    - Spawns TWO falling entities hitting same cell (5, 10) in same frame
    - Verifies target was hit only once (decayed by one level)
  - **TestDecayDifferentSpeeds**: Verifies decay works correctly with min and max speeds
    - Tests slow speed (FallingDecayMinSpeed) and fast speed (FallingDecayMaxSpeed)
    - Creates target at different rows (5 for slow, 20 for fast)
    - Simulates enough time to pass the row
    - Verifies entity was decayed at correct speed
  - **TestDecayFallingEntityPhysicsAccuracy**: Verifies falling entity moves at correct speed
    - Creates entity with speed 10.0 at Y=5.0
    - Simulates 100 frames × 16ms = 1.6 seconds
    - Calculates expected position: `initialY + (speed × elapsed)`
    - Verifies actual position within 0.01 tolerance
  - **TestDecayMatrixEffectCharacterChanges**: Verifies Matrix-style character changes occur
    - Creates entity with initial character 'A'
    - Simulates movement through multiple rows (50 steps × 0.1s)
    - Verifies character changed (probabilistic, 40% chance per row)
    - Logs warning if character didn't change (acceptable due to randomness)

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

### Architecture Component Tests

#### State Ownership Model Tests
Located in `engine/game_state_test.go`:
- **TestSnapshotConsistency**: Verifies snapshots remain consistent under rapid state changes (10 concurrent readers)
- **TestNoPartialReads**: Ensures snapshots never show partial state updates (5 concurrent readers)
- **TestSnapshotImmutability**: Confirms snapshots are immutable value copies
- **TestAllSnapshotTypesConcurrent**: Tests all snapshot types under concurrent access (5 concurrent readers)
- **TestAtomicSnapshotConsistency**: Verifies atomic field snapshots (10 concurrent readers, 1000 rapid updates)

Located in `systems/race_condition_comprehensive_test.go`:
- **TestSnapshotConsistencyUnderRapidChanges**: Multi-writer (3) + multi-reader (10) snapshot consistency
- **TestSnapshotImmutabilityWithSystemUpdates**: Snapshot immutability during active system modifications
- **TestNoPartialSnapshotReads**: Verifies no partial reads during rapid updates (8 concurrent readers)
- **TestPhaseSnapshotConsistency**: Phase snapshot consistency during rapid transitions (5 concurrent readers)
- **TestMultiSnapshotAtomicity**: Multiple snapshot types taken in rapid succession (10 concurrent readers)

All tests pass with `-race` flag (no data races detected).

#### Clock Scheduler Tests
Located in `engine/clock_scheduler_test.go`:
- **TestClockSchedulerBasicTicking**: Tick count increment verification
- **TestClockSchedulerConcurrentTicking**: Concurrent goroutine safety
- **TestClockSchedulerStopIdempotent**: Multiple Stop() calls safety
- **TestClockSchedulerTickRate**: 50ms tick rate verification
- **TestPhaseTransitions**: Phase transition logic validation
- **TestConcurrentPhaseReads**: Concurrent phase access safety

Located in `engine/integration_test.go`:
- **TestCompleteGameCycle**: Full Normal→Gold→DecayWait→DecayAnim→Normal cycle
- **TestGoldCompletionBeforeTimeout**: Early gold completion handling
- **TestConcurrentPhaseReadsDuringTransitions**: 20 readers × concurrent access test
- **TestPhaseTimestampConsistency**: Timestamp accuracy verification
- **TestPhaseDurationCalculation**: Duration calculation accuracy
- **TestCleanerTrailCollisionLogic**: Swept segment collision detection verification
- **TestNoSkippedCharacters**: Verification that truncation logic doesn't skip characters
- **TestRapidPhaseTransitions**: Rapid phase transition stress test
- **TestGoldSequenceIDIncrement**: Sequential ID generation

All tests pass with `-race` flag. Clock runs in goroutine, no memory leaks detected. Concurrent phase reads/writes tested (20 goroutines).

#### Race Condition Testing Guidelines
All tests must pass with `go test -race`. Dedicated race tests exist in multiple files to ensure proper synchronization of shared state. The race detector must be enabled for all test runs to catch potential concurrency issues.
