# ECS & Core Paradigms

## Entity-Component-System (ECS)

### Strict Rules

- **Entities** are ONLY identifiers (uint64)
- **Components** contain ONLY data, NO logic
- **Systems** contain ALL logic, operate on component sets
- **World** is the single source of truth for all game state

### Why ECS?

- **Separation of Concerns**: Data (components) separated from behavior (systems)
- **Composition Over Inheritance**: Entities composed of components, not class hierarchies
- **Performance**: Data-oriented design enables cache-friendly iteration
- **Testability**: Systems can be tested in isolation with mock components

## Component Hierarchy

```
Component (marker interface)
├── PositionComponent {X, Y int}
├── CharacterComponent {Rune, Style}
├── SequenceComponent {ID, Index, Type, Level}
├── GoldSequenceComponent {Active, SequenceID, StartTime, CharSequence, CurrentIndex}
├── NuggetComponent {ID, SpawnTime}
├── DrainComponent {X, Y, LastMoveTime, LastDrainTime, IsOnCursor}
├── FallingDecayComponent {Column, YPosition, Speed, Char, LastChangeRow}
├── CleanerComponent {Row, XPosition, Speed, Direction, TrailPositions, TrailMaxAge}
└── RemovalFlashComponent {X, Y, Char, StartTime, Duration}
```

### Component Design Principles

**PositionComponent**
- Required for all visible entities
- Enables spatial indexing
- Always has integer coordinates

**CharacterComponent**
- Defines visual representation (rune + style)
- Style includes foreground/background colors
- Required for rendering

**SequenceComponent**
- Marks typing sequences (Green, Blue, Red)
- Includes sequence ID for tracking
- Level determines score multiplier (Bright=3x, Normal=2x, Dark=1x)

**NuggetComponent**
- Marks collectible nugget entities
- Spawns randomly every 5 seconds
- Single nugget invariant (at most one active)
- Provides heat bonus when collected

**DrainComponent**
- Marks the drain entity (hostile pressure mechanic)
- Tracks position, movement timing, and drain timing
- Single drain invariant (only one drain entity at a time)
- Spawns when score > 0, despawns when score ≤ 0
- Pursues cursor using 8-directional pathfinding
- Drains 10 score/second when positioned on cursor
- Destroys entities on collision (sequences, nuggets, gold)

## Sequence Types

### Green Sequences
- **Spawning**: Generated from Go source code in `assets/data.txt`
- **Scoring**: Positive points (Heat × Level Multiplier × 1)
- **Decay Path**: Bright → Normal → Dark → **Red (Bright)**
- **Purpose**: Primary positive scoring source

### Blue Sequences
- **Spawning**: Generated from Go source code in `assets/data.txt`
- **Scoring**: Positive points (Heat × Level Multiplier × 1)
- **Decay Path**: Bright → Normal → Dark → **Green (Bright)**
- **Special**: Boost color matching provides 2× heat multiplier

### Red Sequences
- **Spawning**: NEVER spawned directly - only created through Green decay
- **Scoring**: Negative points (penalties)
- **Decay Path**: Bright → Normal → Dark → **Destroyed**
- **Behavior**: Typing resets heat to zero

### Gold Sequences
- **Spawning**: Random position after decay animation
- **Content**: 10 random alphanumeric characters
- **Duration**: 10 seconds timeout
- **Reward**: Fills heat meter to maximum
- **Bonus**: Triggers cleaners if heat already maxed

### Nugget Entities
- **Spawning**: Random position every 5 seconds
- **Appearance**: Orange alphanumeric character (randomly selected from a-z, A-Z, 0-9)
- **Collection**: Type the matching alphanumeric character shown at nugget position
- **Reward**: +10% of max heat (minimum 1)
- **Tab Jump**: Press Tab to jump cursor to nugget (costs 10 score)
- **Decay**: Falling decay entities destroy nuggets on contact
- **Visual**: Dark brown foreground when cursor overlaps
- **Invariant**: At most one nugget active at any time

## Spatial Indexing

### Primary Index
```go
World.spatialIndex[y][x] -> Entity
```

- Maps (x, y) coordinates to entity ID
- Enables O(1) position lookups
- Enforces one entity per position

### Secondary Index
```go
World.componentsByType[Type] -> []Entity
```

- Groups entities by component type
- Enables efficient queries: "Get all entities with SequenceComponent"
- Updated automatically when components added/removed

### Index Maintenance Rules

**ALWAYS** update spatial index when:
- Creating entity with PositionComponent
- Changing entity position
- Destroying entity

### Spatial Transactions

**Purpose**: Atomic spatial index operations to prevent race conditions

The spatial transaction system ensures that all spatial index updates are:
- **Collision-detected**: Check for existing entities before placement
- **Atomic**: All operations commit under single lock
- **Safe**: Prevent phantom entities and spatial index inconsistencies

**Transaction Operations**:
```go
// Begin transaction
tx := world.BeginSpatialTransaction()

// Move entity (checks for collision)
result := tx.Move(entity, oldX, oldY, newX, newY)
if result.HasCollision {
    // Handle collision with result.CollidingEntity
}

// Spawn entity at position
result := tx.Spawn(entity, x, y)

// Destroy entity from position
tx.Destroy(entity, x, y)

// Commit all operations atomically
tx.Commit()
```

**Convenience Method**:
```go
// For simple moves, use MoveEntitySafe
result := world.MoveEntitySafe(entity, oldX, oldY, newX, newY)
if result.HasCollision {
    // Handle collision
}
```

**Example**:
```go
// ✅ GOOD: Use spatial transaction for safe spawn
entity := world.CreateEntity()
world.AddComponent(entity, PositionComponent{X: 10, Y: 5})

tx := world.BeginSpatialTransaction()
result := tx.Spawn(entity, 10, 5)
if result.HasCollision {
    // Handle existing entity at position
    handleCollision(result.CollidingEntity)
}
tx.Commit()

// ✅ GOOD: Use MoveEntitySafe for safe movement
result := world.MoveEntitySafe(entity, 10, 5, 15, 8)
if result.HasCollision {
    // Handle collision
}

// ⚠️ LEGACY: Direct UpdateSpatialIndex (no collision detection)
// Only use when you've already checked for collisions manually
world.UpdateSpatialIndex(entity, 10, 5)
```

### Spatial Index Validation

**Debug Helper**:
```go
// Validate spatial index consistency
inconsistencies := world.ValidateSpatialIndex()
if len(inconsistencies) > 0 {
    // Index has inconsistencies - log or panic
    for _, msg := range inconsistencies {
        log.Printf("Spatial index error: %s", msg)
    }
}
```

**Checks performed**:
- All entities in spatial index actually exist
- All entities with PositionComponent are in spatial index
- Spatial index positions match component positions

## Entity Lifecycle

### Creation
1. Call `world.CreateEntity()` - Returns new entity ID
2. Add components with `world.AddComponent(entity, component)`
3. Update spatial index if entity has position

### Modification
1. Read component: `comp, ok := world.GetComponent(entity, componentType)`
2. Modify component data
3. Update component: `world.UpdateComponent(entity, modifiedComponent)`
4. Update spatial index if position changed

### Destruction
1. Call `world.SafeDestroyEntity(entity)`
2. Automatically removes from spatial index
3. Automatically removes all components
4. Entity ID becomes invalid

## Extension Points

### Adding New Components

**Step 1: Define Component**
```go
// components/new_component.go
type NewComponent struct {
    Field1 int
    Field2 string
    // Data only - no methods!
}
```

**Step 2: Register in Relevant Systems**
```go
// In system's Update() method
newCompType := reflect.TypeOf(components.NewComponent{})
entities := world.GetEntitiesWithComponentType(newCompType)
for _, entity := range entities {
    comp, _ := world.GetComponent(entity, newCompType)
    newComp := comp.(components.NewComponent)
    // Process component...
}
```

**Step 3: Update Spatial Index if Position-Related**
```go
if hasPosition {
    world.UpdateSpatialIndex(entity, x, y)
}
```

### Adding New Systems

**Step 1: Implement System Interface**
```go
type NewSystem struct {
    ctx *engine.GameContext
}

func NewNewSystem(ctx *engine.GameContext) *NewSystem {
    return &NewSystem{ctx: ctx}
}

func (s *NewSystem) Update(delta float64, world *engine.World) {
    // System logic here
}

func (s *NewSystem) Priority() int {
    return 25 // Choose unique priority
}
```

**Step 2: Register in main.go**
```go
newSystem := systems.NewNewSystem(ctx)
ctx.World.AddSystem(newSystem)
```

**Step 3: Wire Up Cross-System References**
```go
// If NewSystem needs to interact with other systems
newSystem.SetOtherSystem(otherSystem)
```

## Component Consistency Rules

### Required Component Combinations

**Typing Sequences** (Green, Blue, Red):
- MUST have: PositionComponent + CharacterComponent + SequenceComponent
- Without PositionComponent: Not renderable, not findable
- Without CharacterComponent: Not renderable
- Without SequenceComponent: Not typeable, not decay-able

**Gold Sequences**:
- MUST have: PositionComponent + CharacterComponent + GoldSequenceComponent
- Different from regular sequences (no SequenceComponent)

**Nuggets**:
- MUST have: PositionComponent + CharacterComponent + NuggetComponent
- CharacterComponent: Orange alphanumeric character (from constants.AlphanumericRunes)
- Single instance enforced by NuggetSystem

**Drain Entity**:
- MUST have: PositionComponent + DrainComponent
- Single instance enforced by DrainSystem
- Lifecycle tied to score (active when score > 0)
- Position synchronized with GameState atomics for rendering

**Falling Decay Entities**:
- MUST have: FallingDecayComponent
- Optional: CharacterComponent (for visual)
- NOT in spatial index (pass through other entities)

**Cleaners**:
- MUST have: CleanerComponent
- NOT in spatial index (pass through other entities)
- Multiple instances per row

### Validation

Systems should validate component combinations:
```go
// ✅ GOOD: Validate before processing
if !world.HasComponent(entity, posType) {
    continue // Skip entity without position
}

// ❌ BAD: Assume component exists
pos := world.GetComponent(entity, posType) // Panics if missing!
```

## Performance Considerations

### Entity Query Patterns

**Cache Queries Per Frame**:
```go
// ✅ GOOD: Query once, iterate many times
entities := world.GetEntitiesWithComponentType(sequenceType)
for _, entity := range entities {
    // Process each entity
}

// ❌ BAD: Query inside loop
for i := 0; i < 100; i++ {
    entities := world.GetEntitiesWithComponentType(sequenceType) // Wasteful!
}
```

**Use Spatial Index for Position Lookups**:
```go
// ✅ GOOD: O(1) lookup
entity := world.GetEntityAtPosition(x, y)

// ❌ BAD: O(n) iteration
for _, entity := range allEntities {
    pos := world.GetComponent(entity, posType)
    if pos.X == x && pos.Y == y { // Slow!
        // Found it
    }
}
```

### Memory Management

**Pool Temporary Slices**:
```go
// Reuse slice allocations
type System struct {
    entityBuffer []engine.Entity
}

func (s *System) Update(delta float64, world *engine.World) {
    s.entityBuffer = s.entityBuffer[:0] // Reset
    // Fill buffer...
}
```

**Batch Operations**:
```go
// ✅ GOOD: Collect entities, destroy at end
toDestroy := []engine.Entity{}
for _, entity := range entities {
    if shouldDestroy {
        toDestroy = append(toDestroy, entity)
    }
}
for _, entity := range toDestroy {
    world.SafeDestroyEntity(entity)
}

// ❌ BAD: Destroy while iterating
for _, entity := range entities {
    if shouldDestroy {
        world.SafeDestroyEntity(entity) // Modifies collection!
    }
}
```

## Invariants to Maintain

1. **One Entity Per Position**: Enforced by spatial index
2. **Component Consistency**: Typing sequences have Position + Character + Sequence
3. **No Orphaned Components**: SafeDestroyEntity removes all components
4. **Spatial Index Accuracy**: Always matches actual entity positions
5. **Single Nugget**: At most one nugget entity active (atomic CAS enforcement)
6. **Single Drain**: At most one drain entity active (controlled by score-based lifecycle)

---

[← Back to Architecture Index](architecture-index.md)
