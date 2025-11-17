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
2. **Spawn (10)**: Generate new character sequences
3. **Trail (20)**: Update visual effects
4. **Decay (30)**: Apply character degradation

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
└── TrailComponent {Intensity, Timestamp}
```

## Rendering Pipeline

1. Clear dirty regions (when implemented)
2. Draw static UI (heat meter, line numbers)
3. Draw game entities (characters, trails)
4. Draw overlays (ping, decay animation)
5. Draw cursor (topmost layer)

## Input State Machine
```
NORMAL ─[i]→ INSERT 
NORMAL ─[/]→ SEARCH 
INSERT / SEARCH ─[ESC]→ NORMAL
```

### Motion Commands
- Single character: Direct execution
- Prefix commands: Build state (`g`, `d`, `f`)
- Count prefix: Accumulate digits until motion

## Audio Integration

**Principles:**
- Audio is OPTIONAL - game must function without it
- Sound manager initialized once, shared via context
- All sound calls are fire-and-forget
- Use channels for complex timing, not timers

## Concurrency Model

- Main game loop: Single-threaded ECS updates
- Input events: Goroutine → channel → main loop
- Audio: Separate goroutine, read-only game state access
- Use `sync.RWMutex` for all shared state

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
5. **Trail Lifecycle**: Trail intensity decreases monotonically until destruction

## Error Handling Strategy

- **User Input**: Flash error cursor, reset heat
- **System Errors**: Log warning, continue with degraded functionality
- **Fatal Errors**: Clean shutdown with screen.Fini()