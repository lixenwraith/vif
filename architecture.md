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
└── CleanerComponent {Row, XPosition, Speed, Direction, TrailPositions, TrailMaxAge}
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
5. Draw cursor (topmost layer)

## Input State Machine
```
NORMAL ─[i]→ INSERT
NORMAL ─[/]→ SEARCH
INSERT / SEARCH ─[ESC]→ NORMAL
```

### Motion Commands (NORMAL Mode)
- **Single character**: Direct execution (h, j, k, l, w, b, e, etc.)
- **Prefix commands**: Build state (`g`, `d`, `f`) then wait for completion
  - `gg` - Jump to top
  - `go` - Jump to top-left corner
  - `dd` - Delete line
  - `dw`, `d$`, `d<motion>` - Delete with motion
  - `f<char>` - Find character on line
- **Count prefix**: Accumulate digits until motion (e.g., `5j`, `10l`, `3w`)
- **Consecutive move penalty**: Using h/j/k/l more than 3 times consecutively resets heat
- **Arrow keys**: Function like h/j/k/l but always reset heat

### Supported Vi Motions
**Basic**: h, j, k, l, Space (as l)
**Line**: 0 (start), ^ (first non-space), $ (end)
**Word**: w, b, e (word), W, B, E (WORD)
**Screen**: gg (top), G (bottom), go (top-left), H (high), M (middle), L (low)
**Paragraph**: { (prev empty), } (next empty)
**Find/Search**: f<char> (find), / (search), n/N (next/prev match)
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
  - Selects random 5-10 consecutive lines from file per spawn
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
- **Behavior**: Sweeps across rows containing Red characters, removing them on contact
- **Direction**: Alternating - odd rows sweep L→R, even rows sweep R→L
- **Speed**: Calculated to traverse screen width in 1 second (CleanerAnimationDuration)
- **Selectivity**: Only destroys Red characters, leaves Blue/Green untouched
- **Animation**: Bright yellow blocks with fade trail effect (10 position history)
- **Concurrency**:
  - Updates run in separate goroutine at 60 FPS for smooth animation
  - Non-blocking spawn requests via buffered channel
  - Atomic state management for thread-safe activation/deactivation
  - Trail slices allocated from `sync.Pool` for efficient memory reuse
- **Duration**: 1 second from spawn to cleanup

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