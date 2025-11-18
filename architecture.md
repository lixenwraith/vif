# Vi-Fighter Architecture

## Core Paradigms

### Entity-Component-System (ECS)
**Strict Rules:**
- Entities are ONLY identifiers (uint64)
- Components contain ONLY data, NO logic
- Systems contain ALL logic, operate on component sets
- World is the single source of truth for all game state

### System Priorities
Systems execute in priority order (lower = earlier):
1. **Input/Score (10)**: Process user input, update score
2. **Spawn (10)**: Generate new character sequences (Blue and Green only)
3. **Gold Sequence (20)**: Manage gold sequence lifecycle and random placement
4. **Decay (30)**: Apply character degradation and color transitions
5. **Cleaner (35)**: Process cleaner spawn requests (actual updates run concurrently)

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
     ↓                                                      ↓
 (Optional)                                          (Always happens)
 Cleaner Trigger                                  Characters decay levels
 (if heat maxed)
```

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

#### Invalid Transitions (Prevented)
- Gold spawning while Gold already active → Ignored
- Decay animation starting while already animating → Ignored
- Decay timer restarting during active animation → Blocked
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
**Paragraph**: { (prev empty), } (next empty), % (matching bracket)
**Find/Search**: f<char> (find forward), F<char> (find backward), / (search), n/N (next/prev match)
**Delete**: x (char), dd (line), d<motion> (delete with motion), D (to end of line)

## Concurrency Model

- Main game loop: Single-threaded ECS updates
- Input events: Goroutine → channel → main loop
- **CleanerSystem**: Concurrent update loop running at 60 FPS in separate goroutine
  - Uses `sync.Pool` for cleaner trail slice allocation/deallocation
  - Channel-based spawn requests for non-blocking trigger mechanism
  - Atomic operations (`atomic.Bool`, `atomic.Int64`) for thread-safe state
  - Mutex protection for screen buffer scanning
- Use `sync.RWMutex` for all shared state
- **Atomic Operations**: Color counters use `atomic.Int64` for lock-free updates
  - `SpawnSystem`: Increments counters when blocks placed
  - `ScoreSystem`: Decrements counters when characters typed
  - `DecaySystem`: Updates counters during decay transitions
  - All counter operations are race-free and thread-safe

## Performance Guidelines

### Hot Path Optimizations
1. Cache entity queries per frame
2. Use spatial index for position lookups
3. Batch similar operations (e.g., all destroys at end)
4. Reuse allocated slices where possible

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
4. **Score Monotonicity**: Score can decrease (red chars) but ScoreIncrement >= 0
5. **Boost Mechanic**: When heat reaches maximum, boost activates with color-matching (Blue or Green) providing x2 heat multiplier. Typing the matching color extends boost duration by 500ms per character, while typing a different color deactivates boost
6. **Red Spawn Invariant**: Red sequences are NEVER spawned directly, only through decay
7. **Gold Randomness**: Gold sequences spawn at random positions (not fixed center-top)
8. **6-Color Limit**: At most 6 Blue/Green color/level combinations present simultaneously
9. **Counter Accuracy**: Color counters must match actual on-screen character counts
10. **Atomic Operations**: All color counter updates use atomic operations for thread safety

## Game Mechanics Details

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
- **Activation Condition**: Heat reaches maximum value (screen width - HeatBarIndicatorWidth)
- **Initial Duration**: 500ms (BoostExtensionDuration constant)
- **Color Binding**: Boost is tied to the color (Blue or Green) of the character that triggered max heat
- **Extension Mechanic**:
  - Typing matching color: Extends boost timer by 500ms per character
  - Typing different color: Deactivates boost immediately (heat remains at max)
  - Typing red or incorrect: Deactivates boost and resets heat to 0
- **Effect**: Heat gain multiplier of 2× (+2 heat per character instead of +1)
- **Visual Indicator**: Pink background "Boost: X.Xs" in status bar
- **Implementation**: Managed within ScoreSystem, not a separate system

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
- **Configuration**: Fully configurable via `constants.CleanerConfig` struct
  - **Animation Duration**: Time to traverse screen (default: 1 second)
  - **Speed**: Characters/second (default: auto-calculated from duration and screen width)
  - **Trail Length**: Number of trail positions (default: 10)
  - **Trail Fade Time**: Duration for complete trail fade (default: 0.3 seconds)
  - **Trail Fade Curve**: Linear or exponential interpolation (default: linear)
  - **Max Concurrent Cleaners**: Limit simultaneous cleaners (default: 0/unlimited)
  - **Scan Interval**: Periodic Red character scanning (default: 0/disabled)
  - **FPS**: Animation frame rate (default: 60)
  - **Character**: Unicode block character (default: '█')
  - **Flash Duration**: Removal flash duration in ms (default: 150)
- **Behavior**: Sweeps across rows containing Red characters, removing them on contact
- **Direction**: Alternating - odd rows sweep L→R, even rows sweep R→L
- **Selectivity**: Only destroys Red characters, leaves Blue/Green untouched
- **Animation**: Configurable block character with fade trail effect
- **Visual Effects**:
  - Pre-calculated gradient table for trail rendering (bright yellow → transparent)
  - Configurable trail length with smooth opacity falloff
  - Configurable removal flash effect when Red characters destroyed
  - Flash cleanup: Automatic removal of expired flash entities
- **Concurrency**:
  - Updates run in separate goroutine at configurable FPS for smooth animation
  - Non-blocking spawn requests via buffered channel
  - Atomic state management for thread-safe activation/deactivation
  - Trail slices allocated from `sync.Pool` for efficient memory reuse
  - Optional periodic scanning for Red characters (configurable interval)
- **Performance**:
  - Pre-calculated color gradients avoid per-frame calculations
  - Batch terminal updates for trail rendering
  - Minimal allocations in render loop (gradient array vs dynamic color creation)

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

### Unit Tests
- **Component Tests**: Validate individual component data structures
- **System Tests**: Test system logic in isolation
- **Helper Tests**: Verify utility functions and time providers

### Integration Tests
Located in `systems/cleaner_gold_integration_test.go`:
- **Concurrent Gold Activation**: Tests gold sequence activation during active cleaners
- **Multiple Cleaners on Adjacent Lines**: Verifies cleaners on adjacent rows don't interfere
- **Cleaner Collision with Changing Text**: Tests cleaners hitting characters being modified by decay
- **Heat Meter State Transitions**: Validates heat changes during cleaner animation
- **Screen Buffer Modifications**: Tests concurrent buffer reads/writes during cleaner scan
- **Concurrent Cleaner Spawns**: Verifies thread-safe spawn request handling
- **Text Color Changes**: Tests decay-induced color changes during cleaner transit
- **Game State Pause/Resume**: Validates cleaner behavior during time control changes

### Race Condition Tests
Located in `systems/cleaner_race_stress_test.go`:
- **Rapid Gold Activation**: Stress tests rapid gold sequence spawning
- **Maximum Cleaners on Screen**: Tests system with 24 concurrent cleaners
- **Memory Leak Detection**: Verifies proper cleanup after multiple cycles
- **Extreme Concurrency**: 20+ goroutines performing various operations
- **Atomic Operation Consistency**: Validates atomic state management
- **Flash Effect Stress Test**: Tests flash creation/cleanup under load
- **Spatial Index Race Conditions**: Tests concurrent spatial index operations

### Benchmark Tests
Located in `systems/cleaner_benchmark_test.go`:
- **Cleaner Spawn Performance**: `BenchmarkCleanerSpawn`
- **Collision Detection**: `BenchmarkCleanerDetectAndDestroy`
- **Row Scanning**: `BenchmarkCleanerScanRedRows`
- **Position Updates**: `BenchmarkCleanerUpdate`
- **Flash Effects**: `BenchmarkFlashEffectCreation`, `BenchmarkFlashEffectCleanup`
- **Gold Sequence Operations**: `BenchmarkGoldSequenceSpawn`, `BenchmarkGoldSequenceCompletion`
- **Concurrent Operations**: `BenchmarkConcurrentCleanerOperations`
- **Full Pipeline**: `BenchmarkCompleteGoldCleanerPipeline`

### Running Tests

#### Standard Test Run
```bash
go test ./systems/... -v
```

#### Race Detector (CRITICAL for concurrency validation)
```bash
go test ./systems/... -race -v
```

#### Benchmarks
```bash
go test ./systems/... -bench=. -benchmem
```

#### Specific Test Categories
```bash
# Integration tests only
go test ./systems/... -run TestConcurrent -v

# Race condition tests only
go test ./systems/... -run TestRapid -v -race

# Memory leak detection (long running)
go test ./systems/... -run TestMemoryLeak -v
```

#### Debug Logging
Set environment variable for verbose race logging:
```bash
VERBOSE_RACE_LOG=1 go test ./systems/... -race -v
```

### Debug Helpers
Located in `systems/test_helpers.go`:
- **RaceDetectionLogger**: Logs concurrent events for race analysis
- **ConcurrencyMonitor**: Tracks operation concurrency and detects anomalies
- **AtomicStateValidator**: Validates atomic state consistency
- **EntityLifecycleTracker**: Detects entity memory leaks

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