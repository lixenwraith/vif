# vi-fighter

A terminal-based typing game built with Go that combines vi/vim motion commands with fast-paced sequence typing. Built using strict Entity-Component-System (ECS) architecture.

## Overview

vi-fighter is a terminal typing game where players type character sequences that appear on screen using vi/vim-style commands and motions. The game features dynamic difficulty scaling through a decay system, multiple sequence types with different behaviors, and a heat mechanic that rewards skilled play.

## Game Mechanics

### Sequence Types

The game features four types of character sequences, each with distinct behaviors:

#### Green Sequences
- **Spawning**: Generated from code blocks in `assets/data.txt` at regular intervals
- **Content**: Lines of Go source code (trimmed and randomly placed)
- **Scoring**: Positive points when typed correctly
- **Levels**: Three brightness levels (Dark, Normal, Bright) with multipliers (x1, x2, x3)
- **Decay**: Decays through brightness levels, then transforms into Red sequences

#### Blue Sequences
- **Spawning**: Generated from code blocks in `assets/data.txt` at regular intervals
- **Content**: Lines of Go source code (trimmed and randomly placed)
- **Scoring**: Positive points when typed correctly
- **Boost Effect**: When heat is maxed and a Blue character triggers boost, typing more Blue extends the boost timer (x2 heat multiplier)
- **Levels**: Three brightness levels (Dark, Normal, Bright) with multipliers (x1, x2, x3)
- **Decay**: Decays through brightness levels, then transforms into Green (Bright) sequences

#### Red Sequences
- **Spawning**: NOT generated directly - only appear through decay of Green sequences
- **Scoring**: Negative points (penalties) when typed
- **Levels**: Three brightness levels (Dark, Normal, Bright)
- **Decay**: When Red sequences at Dark level decay, they are destroyed
- **Purpose**: Provides pressure to clear sequences before they decay too far

#### Gold Sequences
- **Spawning**: Appears randomly on the screen after a decay animation completes
- **Length**: Always exactly 10 characters (alphanumeric only)
- **Duration**: 10 seconds before timeout
- **Position**: Random location avoiding cursor proximity (not fixed to center-top)
- **Reward**: Completing a gold sequence fills the heat meter to maximum
- **Bonus Mechanic**: If heat is already at maximum when gold completed, triggers **Cleaners** (see below)
- **Special**: Typing gold sequence characters does not affect heat or score directly

#### Cleaners (Bonus Mechanic)
- **Trigger**: Automatically activated when completing a gold sequence while heat is already at maximum
- **Behavior**: Bright yellow blocks sweep horizontally across rows containing Red sequences
- **Pattern**: Alternating direction (odd rows left-to-right, even rows right-to-left)
- **Effect**: Destroys Red characters on contact, leaving Green/Blue sequences untouched
- **Duration**: Configurable animation (default: 1 second per cleaner)
- **Visual**: Block character ('█') with trailing fade effect and removal flash on Red destruction
- **Strategy**: Allows aggressive high-heat play without Red penalty accumulation

### Heat System

- **Heat Meter**: Displayed at the top of the screen as a colored progress bar with numeric value
- **Gaining Heat**: +1 for each correct Green/Blue character (+2 if boost active)
- **Losing Heat**: Resets to 0 on incorrect typing, typing red sequences, arrow keys, or 3+ consecutive h/j/k/l moves
- **Effects**:
  - Heat level determines decay speed (higher heat = faster decay, from 60s to 10s intervals)
  - Heat directly multiplies score for each character typed
  - Maximum heat activates boost (x2 heat multiplier for matching color)
  - Completing gold sequences instantly fills heat to maximum

### Decay System

The decay system creates pressure by gradually degrading sequences over time:

1. **Brightness Decay**: Sequences first decay through brightness levels:
   - Bright → Normal → Dark
   - Each level change reduces the score multiplier

2. **Color Decay Chain**:
   - Blue (Dark) → Green (Bright)
   - Green (Dark) → Red (Bright)
   - Red (Dark) → Destroyed

3. **Decay Timing**:
   - Decay interval varies from 10 to 60 seconds based on heat level
   - Higher heat = faster decay = more pressure
   - Decay animation sweeps from top to bottom of the screen

### Spawn System

The spawn system uses **file-based code blocks** from `assets/data.txt` to populate the screen:

- **Content Source**: Reads Go source code from `assets/` directory at startup (automatically located at project root)
- **Block Selection**:
  - Random 3-15 consecutive lines selected from file (grouped by indent level and structure)
  - Lines are trimmed of whitespace before placement
  - Line order within block doesn't matter (can be shuffled)

- **6-Color Limit System**:
  - Only 6 color/level combinations allowed on screen simultaneously:
    - Blue: Bright, Normal, Dark (3 states)
    - Green: Bright, Normal, Dark (3 states)
  - New blocks only spawn when fewer than 6 colors are present
  - When a color is fully cleared (typed or decayed), a new block can spawn

- **Intelligent Placement**:
  - Each line attempts placement up to 3 times
  - Random row and column selection per attempt
  - Checks for collisions with existing characters
  - Maintains cursor exclusion zone (5 units horizontal, 3 units vertical)
  - Lines that can't be placed after 3 attempts are discarded

- **Spawn Rate**: Blocks spawn every 2 seconds (base rate)
- **Adaptive Rate**:
  - < 30% screen filled: 2x faster spawning (1 second intervals)
  - > 70% screen filled: 2x slower spawning (4 second intervals)

- **Character Tracking**: Atomic counters track character count per color/level for race-free state management
- **Maximum Capacity**: 200 characters on screen at once

## Vi Motion Commands

vi-fighter supports a comprehensive set of vi/vim motion commands for navigation:

### Basic Navigation
- `h` / `j` / `k` / `l` - Move left/down/up/right (supports count prefixes)
- Arrow keys - Same as hjkl but reset heat (not recommended)
- Space - Move right (same as `l` in NORMAL mode, safe movement in INSERT mode)

### Line Navigation
- `0` - Jump to start of line (column 0)
- `^` - Jump to first non-whitespace character
- `$` - Jump to end of line

### Word Motions
- `w` / `b` / `e` - Word forward/backward/end (alphanumeric/punctuation boundaries)
- `W` / `B` / `E` - WORD forward/backward/end (space-delimited only)

### Screen Motions
- `gg` - Jump to top-left (row 0, same column)
- `G` - Jump to bottom (last row, same column)
- `go` - Jump to absolute top-left corner (row 0, column 0)
- `H` / `M` / `L` - Jump to top/middle/bottom of screen (same column)

### Paragraph Navigation
- `{` - Jump to previous empty line
- `}` - Jump to next empty line
- `%` - Jump to matching bracket (works with (), {}, [])

### Find & Search
- `f<char>` - Find character forward on current line
- `/` - Enter search mode (type pattern, press Enter)
- `n` / `N` - Repeat last search forward/backward

### Delete Operations
- `x` - Delete character at cursor (resets heat if Green/Blue)
- `dd` - Delete entire line
- `d<motion>` - Delete with motion (e.g., `dw`, `d$`, `d5j`)
- `D` - Delete to end of line (same as `d$`)

### Advanced Features
- **Count prefixes**: Any motion supports numeric prefixes (e.g., `5j`, `3w`, `10l`)
- **Insert mode**: Press `i` to start typing sequences
- **Mode switching**: Press `ESC` to return to NORMAL mode
- **Ping grid**: Press `Enter` to activate row/column guides for 1 second

**Note**: Using `h`/`j`/`k`/`l` more than 3 times consecutively resets heat. Use word motions or other movement commands for efficiency.

For a complete player guide with all commands and strategies, see [game.md](./game.md).

## Technical Architecture

### Entity-Component-System (ECS)

The game strictly follows ECS architecture principles:

- **Entities**: Simple uint64 identifiers
- **Components**: Pure data structures (Position, Character, Sequence, Cleaner, etc.)
- **Systems**: All game logic (Spawn, Decay, Score, Gold Sequence, Cleaner)
- **World**: Single source of truth for game state

### System Execution Order

Systems execute in priority order each frame:

1. **Input/Score System** (Priority 10): Process user input and update score
2. **Spawn System** (Priority 10): Generate new character sequences
3. **Gold Sequence System** (Priority 20): Manage gold sequence lifecycle
4. **Decay System** (Priority 30): Apply character degradation and color transitions
5. **Cleaner System** (Priority 35): Process cleaner spawn requests and manage animation state

### Key Features

- **Spatial Indexing**: Fast position-based entity lookups
- **Monotonic Time**: Consistent time handling for reproducible behavior
- **Concurrency Safety**: RWMutex protection for all shared state
- **Terminal Rendering**: Built with tcell for cross-platform terminal graphics

## Building and Running

### Prerequisites

- Go 1.19 or later
- Terminal with color support
- `assets/` directory containing `.txt` files with source code (included in repository)

### Build

```bash
go build -o vi-fighter ./cmd/vi-fighter
```

### Run

The game automatically locates the `assets/` directory at the project root:

```bash
# From repository root
./vi-fighter
```

Or directly:

```bash
go run ./cmd/vi-fighter
```

**Note**: The ContentManager automatically finds the project root by searching for `go.mod`, then uses the `assets/` directory from there. If `assets/` is missing or contains no valid `.txt` files, the game gracefully falls back to default content.

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run tests with verbose output
go test -v ./...

# Run specific test package
go test ./systems
```

**Note**: Tests automatically locate the project root and `assets/` directory regardless of the current working directory. This ensures tests pass consistently in CI environments and when run from subdirectories.

## Controls

- **Movement**: Vi motion commands (h/j/k/l, w/b/e, gg/G/H/M/L, etc.)
- **Insert Mode**: Press `i` to begin typing sequences
- **Search Mode**: Press `/` followed by pattern, then `Enter`
- **Exit Insert/Search**: Press `ESC` to return to NORMAL mode
- **Ping Grid**: Press `Enter` to show row/column guides (1 second)
- **Quit**: `Ctrl+C` or `Ctrl+Q`

For detailed command reference and gameplay strategies, see [game.md](./game.md).

## Game Strategy (Quick Tips)

1. **Prioritize Gold**: Gold sequences fill heat to maximum - chase them when they appear
2. **Avoid Red**: Red sequences penalize your score and reset heat
3. **Type Bright When Hot**: Bright sequences at high heat give maximum points (Heat × 3)
4. **Manage Decay**: Higher heat = faster decay - balance aggressive play with sustainability
5. **Boost Color Matching**: When boost activates, keep typing the same color to extend it
6. **Motion Efficiency**: Use `w`/`b`/`e` instead of repeated `h`/`j`/`k`/`l` to avoid heat reset

**For comprehensive strategy guides including beginner, intermediate, and advanced tactics, see [game.md](./game.md).**

## Development

See [architecture.md](./architecture.md) for detailed architecture documentation and development guidelines.

## License

MIT License - See LICENSE file for details
