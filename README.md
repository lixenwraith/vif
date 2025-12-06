# vi-fighter

A terminal-based typing game built with Go that combines vi/vim motion commands with fast-paced sequence typing. Built using strict Entity-Component-System (ECS) architecture.

## Overview

vi-fighter is a terminal typing game where players type character sequences that appear on screen using vi/vim-style commands and motions. The game features dynamic difficulty scaling through a decay system, multiple sequence types with different behaviors, a heat mechanic that rewards skilled play, and real-time performance metrics (FPS, Game Ticks, Actions Per Minute).

## Documentation

- **[Game Guide](doc/game.md)** - Player guide with controls, mechanics, and strategies
- **[Architecture](doc/architecture.md)** - Technical documentation and development guidelines

## Game Mechanics Overview

vi-fighter features a dynamic typing system with multiple sequence types and game mechanics:

- **Sequence Types**: Green, Blue, Red (penalties), and Gold (bonus)
- **Nuggets**: Collectible orange alphanumeric characters that provide heat bonuses
- **Heat System**: Typing momentum that multiplies energy and affects decay speed
- **Boost System**: 2× heat multiplier activated at maximum heat
- **Shield System**: Energy-powered protective field (activates when Energy > 0, passive 1/sec + zone 100/tick/drain costs)
- **Decay System**: Automated pressure that degrades sequences over time
- **Cleaners**: Clears Red sequences - horizontal sweeps (gold at max heat) or 4-directional bursts (nugget at max heat, Enter key)
- **Multi-Drain System**: Heat-based hostile entities (floor(heat/10), max 10), staggered spawns with materialize animations, shield interaction (energy cost vs heat loss), drain-drain collisions, despawn when Energy drops to 0
- **Splash Feedback System**: Event-driven visual feedback with two modes:
  - **Transient Splashes**: Large block-character feedback for typing, commands, and nugget collection (1-second fade, smart quadrant placement avoiding cursor and gold)
  - **Gold Countdown Timer**: Persistent single-digit timer (9 → 0) anchored above/below gold sequences
- **Runtime Metrics**: Real-time performance tracking (FPS, Game Ticks, APM) displayed in status bar

## Vi Motion Commands

vi-fighter supports a comprehensive set of vi/vim motion commands for navigation:

**Basic**: h, j, k, l, Space (count prefixes supported)
**Line**: 0, ^, $ (start, first non-space, end)
**Word**: w, b, e (word), W, B, E (WORD)
**Screen**: gg, G, go, H, M, L
**Paragraph**: {, } (empty lines), % (matching brackets)
**Find**: f, F, t, T, ; , (with count support: 2fa, 3Fb)
**Search**: /, n, N
**Delete**: x, dd, d<motion>, D

**Note**: Using `h`/`j`/`k`/`l` more than 3 times consecutively resets heat. Use word motions or other movement commands for efficiency.

## Technical Architecture

The game strictly follows ECS architecture principles with a hybrid real-time/clock-based game loop:

- **Entities**: Simple uint64 identifiers
- **Components**: Pure data structures (Position, Character, Sequence, Gold, Nugget, Cleaner, Drain, etc.)
- **Systems**: All game logic (Boost, Energy, Spawn, Nugget, Gold, Cleaner, Drain, Decay)
- **World**: Single source of truth for game state
- **Event System**: Dedicated `events` package with generic routing (`Router[T]`) for decoupled inter-system communication (MPSC pattern, lock-free queue)
- **Concurrency**: Lock-free atomics for real-time state, mutexes for clock-tick state
- **Rendering**: Direct terminal rendering with custom terminal package, RenderOrchestrator, and stencil-based post-processing

## Building and Running

### Prerequisites

- Go 1.24 or later
- Terminal with color support
- `data/` directory containing `.txt` files with source code (included in repository)

### Build

```bash
go build -o vi-fighter ./cmd/vi-fighter
```

### Run

The game automatically locates the `data/` directory at the project root:

```bash
# From repository root
./vi-fighter
```

Or directly:

```bash
go run ./cmd/vi-fighter
```

**Note**: The ContentManager automatically finds the project root by searching for `go.mod`, then uses the `data/` directory from there. If `data/` is missing or contains no valid `.txt` files, the game gracefully falls back to default content.

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

**Note**: Tests automatically locate the project root and `data/` directory regardless of the current working directory. This ensures tests pass consistently in CI environments and when run from subdirectories.

## Controls

- **Movement**: Vi motion commands (h/j/k/l, w/b/e, gg/G/H/M/L, etc.)
- **Insert Mode**: Press `i` to begin typing sequences
- **Search Mode**: Press `/` followed by pattern, then `Enter`
- **Command Mode**: Press `:` to enter COMMAND mode (e.g., `:boost`, `:debug`, `:help`)
- **Overlay Mode**: `:debug` or `:help` commands open modal popups with information
- **Exit Insert/Search/Command/Overlay**: Press `ESC` to return to NORMAL mode
- **Ping Grid**: Press `ESC` in NORMAL mode to show row/column guides (1 second)
- **Directional Cleaners**: Press `Enter` in NORMAL mode (requires heat ≥ 10, costs 10 heat) to spawn 4-directional cleaners from cursor
- **Quit**: `Ctrl+C` or `Ctrl+Q`

**Note**: When in COMMAND or OVERLAY mode, the game is paused - all non-UI content is dimmed (50% brightness) via post-processing to indicate the paused state while preserving UI visibility.

## Game Strategy (Quick Tips)

1. **Prioritize Gold**: Gold sequences fill heat to maximum - chase them when they appear
2. **Collect Nuggets**: Free heat boost (+10% max heat) - collect at max heat to spawn directional cleaners
3. **Avoid Red**: Red sequences penalize your energy and reset heat
4. **Type Bright When Hot**: Bright sequences at high heat give maximum points (Heat × 3)
5. **Manage Decay**: Higher heat = faster decay - balance aggressive play with sustainability
6. **Boost Color Matching**: When boost activates, type same color to extend timer; different colors update BoostColor without penalty
7. **Manage Shield Energy**: Shield activates automatically when Energy > 0 - monitor energy costs (1/sec passive + 100/tick per drain)
8. **Motion Efficiency**: Use `w`/`b`/`e` instead of repeated `h`/`j`/`k`/`l` to avoid heat reset
9. **Use Cleaners**: Gold at max heat → horizontal row cleaners; Nugget at max heat or Enter key → 4-directional cleaners from cursor

## License

BSD-3 Clause
