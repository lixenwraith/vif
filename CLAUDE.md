# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go currently undergoing a major architectural migration from a reflection-based ECS to a compile-time Generics-based ECS (Go 1.18+). The codebase uses a hybrid architecture combining real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
Go Toolchain: Go 1.24.7

## MIGRATION OBJECTIVE: GENERICS-BASED ECS
**GOAL**: Eliminate all `reflect` usage in the hot path (Update/Render) to reduce GC pressure and enforce type safety.

### 1. DATA STRUCTURES
**PATTERN**: Use explicit, typed stores instead of dynamic maps.
- **Old**: `world.GetComponent(e, reflect.TypeOf(Position{}))`
- **New**: `world.Positions.Get(e)`

**World Structure**:
```go
type World struct {
    // Explicit Stores (Public for Systems)
    Positions      *PositionStore // Specialized store with spatial index
    Characters     *Store[components.CharacterComponent]
    Sequences      *Store[components.SequenceComponent]
    // ... explicit fields for all 9 components
    
    // Lifecycle Interface (For DestroyEntity)
    allStores []AnyStore
}
```

### 2. QUERY PATTERN
**PATTERN**: Use the Query Builder for component intersection.
- **Optimization**: Intersection logic must start with the smallest store.
```go
// ✅ CORRECT: Type-safe, zero-allocation query
entities := world.Query().
    With(world.Positions).
    With(world.Characters).
    Execute()

for _, e := range entities {
    pos, _ := world.Positions.Get(e)
    char, _ := world.Characters.Get(e)
    // ...
}
```

### 3. ENTITY CREATION (BUILDER)
**PATTERN**: Use the Entity Builder for atomic creation.
- **Transactional**: ID is reserved, but components are committed only on `Build()`.
```go
// ✅ CORRECT
world.NewEntity().
    With(world.Positions, pos).
    With(world.Characters, char).
    Build()
```

### 4. SPATIAL INDEXING
**RULE**: The `SpatialIndex` is internal to `PositionStore`.
- **Read**: `world.Positions.GetEntityAt(x, y)`
- **Write**: Only via `world.Positions.Add()` or `world.Positions.Move()`.
- **Batch**: Use `world.Positions.BeginBatch()` for multi-entity spawns to ensure atomicity and collision detection.

## ARCHITECTURAL PILLARS

### 1. EVENT-DRIVEN COMMUNICATION
**PATTERN**: Systems decouple via `GameContext.EventQueue`.
- **Producers**: Push events to queue (e.g., `ScoreSystem` pushes `EventCleanerRequest`).
- **Consumers**: Poll events in `Update()` (e.g., `CleanerSystem` consumes `EventCleanerRequest`).

### 2. ATOMIC STATE PREFERENCE
**DIRECTIVE**: Prefer lock-free atomics over Mutexes for high-frequency data.
- **Counters/Flags**: Use `atomic.Int64`, `atomic.Bool`.
- **Snapshots**: Read multiple atomics sequentially for "consistent enough" real-time views.

### 3. RENDERER DECOUPLING
**RULE**: Renderer reads **DATA**, never queries **LOGIC**.
- Renderer queries `World` stores directly (`world.Positions.Get(e)`).
- **Thread Safety**: Deep-copy mutable reference types (slices/maps) from components within the Store's read lock scope.

## CODING DIRECTIVES

### 1. MIGRATION SAFETY
**MANDATORY**: Do not break the build.
- Use the **Parallel World** pattern: Implement new Generic stores alongside the old Reflection maps until all systems are migrated.
- Use `world.SyncToGeneric()` temporarily to keep states in sync during the transition.

### 2. CONSTANT MANAGEMENT
**MANDATORY**: No magic numbers. Define in `constants/*.go`.

### 3. NO REFLECTION
**STRICT PROHIBITION**: Do not import `reflect`. Use type assertions or generic constraints only if absolutely necessary, but prefer concrete types in Stores.

## IMPLEMENTATION PATTERNS

### Generic System Update
```go
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    // Direct Store Access (Fastest)
    if pos, ok := world.Positions.Get(myEntity); ok {
        pos.X += 1
        world.Positions.Add(myEntity, pos)
    }
    
    // Complex Query (Readable)
    entities := world.Query().With(world.Positions).With(world.Tags).Execute()
}
```

### Position Store Transaction
```go
batch := world.Positions.BeginBatch()
batch.Add(e1, pos1)
batch.Add(e2, pos2)
if err := batch.Commit(); err != nil {
    // Handle collision
}
```

## VERIFICATION CHECKLIST
- [ ] **Compilation**: `go build ./...` must pass after every step.
- [ ] **No Reflection**: Verify no `reflect` usage in modified files.
- [ ] **Type Safety**: Ensure `Store[T]` prevents storing wrong component types.
- [ ] **Spatial Integrity**: Verify `PositionStore` updates the internal map on Add/Remove.
- [ ] **Locking**: Ensure Stores use internal RWMutex correctly (RLock for Reads).

## FILE ORGANIZATION (TARGET)
```
vi-fighter/
├── engine/
│   ├── store.go         # Generic Store[T] implementation
│   ├── position_store.go # Specialized store with Spatial Index
│   ├── query.go         # Query Builder
│   ├── entity_builder.go # Entity Builder
│   ├── world.go         # The new World struct with explicit stores
│   └── events.go        # Event Queue
├── systems/             # Logic (Generic implementations)
├── render/              # Visualization (Generic readers)
└── components/          # Data Structs
```