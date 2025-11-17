# vi-fighter

A terminal-based typing game built with Go that combines vi/vim motion commands with fast-paced sequence typing. Built using strict Entity-Component-System (ECS) architecture.

## Overview

vi-fighter is a terminal typing game where players type character sequences that appear on screen using vi/vim-style commands and motions. The game features dynamic difficulty scaling through a decay system, multiple sequence types with different behaviors, and a heat mechanic that rewards skilled play.

## Game Mechanics

### Sequence Types

The game features four types of character sequences, each with distinct behaviors:

#### Green Sequences
- **Spawning**: Generated randomly across the screen at regular intervals
- **Scoring**: Positive points when typed correctly
- **Levels**: Three brightness levels (Dark, Normal, Bright) with multipliers (x1, x2, x3)
- **Decay**: Decays through brightness levels, then transforms into Red sequences

#### Blue Sequences
- **Spawning**: Generated randomly across the screen at regular intervals
- **Scoring**: Positive points when typed correctly
- **Special Effect**: Completing a blue sequence triggers a heat boost (x2 multiplier) for a limited time
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
- **Special**: Typing gold sequence characters does not affect heat or score directly

### Heat System

- **Heat Meter**: Displayed at the bottom of the screen, representing typing skill/performance
- **Gaining Heat**: Successfully typing correct sequences increases heat
- **Losing Heat**: Typing incorrect characters or red sequences decreases heat
- **Effects**:
  - Heat level determines decay speed (higher heat = faster decay)
  - Maximum heat can be instantly achieved by completing gold sequences
  - Blue sequences provide temporary heat boost multiplier

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

- **Spawn Rate**: Sequences spawn every 2 seconds (base rate)
- **Adaptive Rate**:
  - < 30% screen filled: 2x faster spawning (1 second intervals)
  - > 70% screen filled: 2x slower spawning (4 second intervals)
- **Sequence Generation**:
  - Random length: 1-10 characters
  - Character set: Full alphanumeric + special characters
  - Types: Only Blue and Green are spawned directly (Red comes from decay)
- **Position Rules**:
  - Random placement across game area
  - Maintains exclusion zone around cursor (5 units horizontal, 3 units vertical)
  - No overlapping with existing sequences
- **Maximum Capacity**: 200 characters on screen at once

## Vi Motion Commands

vi-fighter supports standard vi/vim motion commands for navigation:

### Basic Navigation
- `h` / `j` / `k` / `l` - Move left/down/up/right
- `w` / `b` / `e` - Word motions (forward word, back word, end of word)
- `0` / `$` - Start/end of line
- `gg` / `G` - Top/bottom of screen
- `f<char>` - Find character forward on current line

### Advanced Features
- Count prefixes: `5j`, `3w`, etc.
- Search: `/` followed by pattern
- Insert mode: `i` to start typing sequences

## Technical Architecture

### Entity-Component-System (ECS)

The game strictly follows ECS architecture principles:

- **Entities**: Simple uint64 identifiers
- **Components**: Pure data structures (Position, Character, Sequence)
- **Systems**: All game logic (Spawn, Decay, Score, Gold Sequence)
- **World**: Single source of truth for game state

### System Execution Order

Systems execute in priority order each frame:

1. **Input/Score System** (Priority 10): Process user input and update score
2. **Spawn System** (Priority 10): Generate new character sequences
3. **Gold Sequence System** (Priority 20): Manage gold sequence lifecycle
4. **Decay System** (Priority 30): Apply character degradation and color transitions

### Key Features

- **Spatial Indexing**: Fast position-based entity lookups
- **Monotonic Time**: Consistent time handling for reproducible behavior
- **Concurrency Safety**: RWMutex protection for all shared state
- **Terminal Rendering**: Built with tcell for cross-platform terminal graphics

## Building and Running

### Prerequisites

- Go 1.19 or later
- Terminal with color support

### Build

```bash
go build -o vi-fighter ./cmd/vi-fighter
```

### Run

```bash
./vi-fighter
```

Or directly:

```bash
go run ./cmd/vi-fighter
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run specific test package
go test ./systems
```

## Controls

- **Movement**: Vi motion commands (h/j/k/l, w/b/e, etc.)
- **Insert Mode**: Press `i` to begin typing sequences
- **Search**: Press `/` followed by pattern
- **Exit Insert/Search**: Press `ESC`
- **Quit**: `Ctrl+C` or close terminal

## Game Strategy

1. **Prioritize Decay**: Focus on typing sequences that are close to decaying (darker colors)
2. **Avoid Red**: Red sequences penalize your score - clear them carefully or avoid if possible
3. **Chase Gold**: Gold sequences provide massive heat rewards - prioritize them when they appear
4. **Use Blue Boosts**: Complete blue sequences strategically to activate heat multipliers
5. **Manage Heat**: Higher heat means faster decay - balance aggressive clearing with accuracy

## Development

See [architecture.md](./architecture.md) for detailed architecture documentation and development guidelines.

## License

MIT License - See LICENSE file for details
