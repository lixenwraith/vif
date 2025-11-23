# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a hybrid ECS architecture. It combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.

## ARCHITECTURAL PILLARS

### 1. EVENT-DRIVEN COMMUNICATION
**PATTERN**: Systems decouple via `GameContext.EventQueue`.
- **Producers**: Push events to queue (e.g., `ScoreSystem` pushes `EventCleanerRequest`).
- **Consumers**: Poll events in `Update()` (e.g., `CleanerSystem` consumes `EventCleanerRequest`).
- **State**: Events replace direct method calls or shared boolean flags for triggering one-shot mechanics.
```go
// ✅ CORRECT
ctx.PushEvent(engine.EventCleanerRequest, nil)

// ❌ WRONG
ctx.State.RequestCleaners() // Direct state mutation for triggers
otherSystem.Activate()      // Direct coupling
```

### 2. ATOMIC STATE PREFERENCE
**DIRECTIVE**: Prefer lock-free atomics over Mutexes for high-frequency data.
- **Counters/Flags**: Use `atomic.Int64`, `atomic.Bool`, `atomic.Uint64`.
- **Snapshots**: Read multiple atomics sequentially for "consistent enough" real-time views.
- **Mutexes**: Reserved for complex structural changes (maps/slices) or strict transactional logic (Spawn/Phase transitions).
```go
// ✅ CORRECT
atomic.AddInt64(&gs.BlueCountBright, 1)

// ❌ WRONG
gs.mu.Lock(); gs.BlueCountBright++; gs.mu.Unlock() // Unnecessary contention
```

### 3. RENDERER DECOUPLING
**RULE**: Renderer reads **DATA**, never queries **LOGIC**.
- Renderer must not hold references to Systems.
- Renderer queries `World` components directly.
- **Thread Safety**: Renderer must deep-copy mutable reference types (slices/maps) from components within the World's read lock scope to prevent race conditions with Update threads.

### 4. SPATIAL TRANSACTIONS
**PATTERN**: All position changes use the transaction system.
```go
tx := world.BeginSpatialTransaction()
tx.Spawn(entity, x, y) // or Move/Destroy
tx.Commit()
```

## CODING DIRECTIVES

### 1. CONSTANT MANAGEMENT
**MANDATORY**: No magic numbers.
- Define in `constants/*.go`.
- Use descriptive names (`CleanerTrailLength`, not `10`).

### 2. SINGLE SOURCE OF TRUTH
- **Phase State**: `GameState` is the authority on Phases (Normal, Gold, Decay).
- **Component Data**: `World` components are the authority on Entity state.
- **Events**: The `EventQueue` is the authority on transient triggers.

### 3. DOCUMENTATION
- Update `doc/architecture.md` immediately upon design changes.
- Ensure `architecture.md` reflects the current Event Queue structure.

## IMPLEMENTATION PATTERNS

### System Event Polling
```go
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    events := s.ctx.ConsumeEvents() // Atomically claims events
    for _, e := range events {
        if e.Type == engine.EventMyTrigger {
            s.handleTrigger(e)
        }
    }
    // ... rest of logic
}
```

### Renderer Component Query
```go
func (r *Renderer) draw(world *engine.World) {
    // World.GetEntitiesWith uses RLock internally
    entities := world.GetEntitiesWith(myType)
    for _, e := range entities {
        if c, ok := world.GetComponent(e, myType); ok {
            comp := c.(MyComponent)
            // DEEP COPY SLICES if they might change in Update()
            trail := make([]Point, len(comp.Trail))
            copy(trail, comp.Trail)
            r.render(trail)
        }
    }
}
```

## VERIFICATION CHECKLIST
- [ ] **Race Detection**: Run tests with `go test -race ./...`
- [ ] **Event Loop**: Ensure events are consumed, not just peeked (unless intended).
- [ ] **Frame Alignment**: Events include Frame ID to prevent processing old triggers.
- [ ] **Atomic Safety**: No mixing of atomic and non-atomic access on the same field.
- [ ] **Documentation**: `CLAUDE.md` and `architecture.md` updated.

## FILE ORGANIZATION
```
vi-fighter/
├── engine/
│   ├── events.go        # NEW: Event definitions and Ring Buffer
│   ├── game_state.go    # Atomic & Shared State
│   └── game.go          # Context & Event wiring
├── systems/             # Logic (Event Consumers)
├── render/              # Visualization (Data Readers)
└── constants/
```