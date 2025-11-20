# Vi-Fighter Architecture

## Overview

Vi-fighter is built using strict Entity-Component-System (ECS) architecture with a hybrid real-time/clock-based game loop. This architecture ensures clean separation of concerns, thread-safe concurrency, and high performance.

## Architecture Documentation

- **[ECS & Core Paradigms](architecture-ecs.md)** - Entity-Component-System pattern, component hierarchy, spatial indexing
- **[State Management](architecture-state.md)** - State ownership model, atomic operations, snapshot pattern, GameState
- **[Systems & Coordination](architecture-systems.md)** - System priorities, execution order, event flow, game cycle
- **[Concurrency Model](architecture-concurrency.md)** - Thread safety, race condition prevention, synchronization patterns
- **[Testing Strategy](architecture-tests.md)** - Test organization, running tests, race detection, benchmarks

## Quick Reference

### Core Principles

1. **ECS Pattern**: Entities are IDs, Components are data, Systems are logic
2. **State Ownership**: Clear boundaries between real-time (atomics) and clock-tick (mutex) state
3. **Thread Safety**: Lock-free reads with atomic operations, mutex-protected clock state
4. **Snapshot Pattern**: Immutable state snapshots for consistent multi-field reads
5. **Concurrency**: Single-threaded ECS updates with separate clock scheduler

### System Priorities (Execution Order)

1. **ScoreSystem (10)** - Process user input, update score
2. **SpawnSystem (15)** - Generate new character sequences
3. **NuggetSystem (18)** - Manage nugget spawn and lifecycle
4. **GoldSequenceSystem (20)** - Manage gold sequence lifecycle
5. **DecaySystem (25)** - Apply character degradation
6. **CleanerSystem (30)** - Process cleaner animations

### Key Components

- **GameState** (`engine/game_state.go`) - Centralized game state with atomic and mutex-protected fields
- **GameContext** (`engine/game_context.go`) - System access point for state and dimensions
- **World** (`engine/world.go`) - Entity/component management with spatial indexing
- **ClockScheduler** (`engine/clock_scheduler.go`) - 50ms ticker for phase transitions

## Data Files

- **assets/** - Contains `.txt` files with game content (code blocks)
- Located automatically at project root by searching for `go.mod`

## Extension Points

### Adding New Components
1. Define data struct implementing `Component`
2. Register type in relevant systems
3. Update spatial index if position-related

### Adding New Systems
1. Implement `System` interface with `Update()` and `Priority()`
2. Register in `cmd/vi-fighter/main.go`
3. Wire up cross-system references as needed

## Invariants to Maintain

1. **One Entity Per Position** - Spatial index enforces uniqueness
2. **Component Consistency** - Sequence entities require Position + Character
3. **Cursor Bounds** - Always within game dimensions
4. **Heat Non-Negativity** - Heat >= 0 at all times
5. **Red Spawn Invariant** - Red sequences only created through decay
6. **6-Color Limit** - Max 6 Blue/Green color/level combinations
7. **Counter Accuracy** - Atomic color counters match on-screen characters
8. **Single Nugget Invariant** - At most one nugget active (enforced via atomic CAS)

## Performance Guidelines

- Cache entity queries per frame
- Use spatial index for position lookups
- Batch similar operations
- Pool temporary slices
- Limit total entity count (MAX_CHARACTERS = 200)

---

For detailed information on each topic, see the linked documentation files above.
