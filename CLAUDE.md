# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.25+).

## ARCHITECTURE OVERVIEW

### Core Systems
- **ECS**: Generics-based `World` with `Store[T]` and `PositionStore` (spatial hash).
- **Game Loop**: Fixed 50ms tick (`ClockScheduler`) decoupled from rendering.
- **Render Pipeline**: `RenderOrchestrator` coordinates `SystemRenderer` implementations.
- **Input**: `InputHandler` processes `tcell` events, managing state transitions between Modes.

### Resources
- **Context**: `GameContext` acts as the root state container.
- **Resources**: `TimeResource`, `ConfigResource`, `InputResource` stored in `World.Resources`.

### Render Architecture
- **Orchestrator**: `RenderOrchestrator` manages render pipeline lifecycle.
- **Buffer**: `RenderBuffer` is a dense grid for compositing; zero-alloc after init.
- **Renderers**: Individual `SystemRenderer` implementations in `render/renderers/`.
- **Priority**: `RenderPriority` constants determine render order (lower first).

## CURRENT TASK: Z-Index Engine Integration

### Objective
Activate and enhance `engine/z-index.go` to be the single source of truth for entity layering. Modify `PositionStore` to support multiple entities per cell with z-index-based selection.

### Reference Document
- `engine/z-index.go` - Existing stub with constants and `SelectTopEntity`
- `engine/position_store.go` - Current single-entity-per-cell implementation

### Key Types
```go
// Z-Index constants (higher = on top)
const (
    ZIndexBackground = 0
    ZIndexSpawnChar  = 100
    ZIndexNugget     = 200
    ZIndexDecay      = 300
    ZIndexDrain      = 400
    ZIndexShield     = 500
    ZIndexCursor     = 1000
)

// Enhanced Cell struct
type Cell struct {
    Entities  []Entity  // All entities at position
    TopEntity Entity    // Cached highest z-index
    TopZIndex int       // Cached z-index value
}

// New API methods
func GetZIndex(world *World, e Entity) int
func SelectTopEntity(entities []Entity, world *World) Entity
func SelectTopEntityFiltered(entities []Entity, world *World, filter func(Entity) bool) Entity
func IsInteractable(world *World, e Entity) bool

// PositionStore new methods
func (ps *PositionStore) GetAllEntitiesAt(x, y int) []Entity
func (ps *PositionStore) GetTopEntityFiltered(x, y int, world *World, filter func(Entity) bool) Entity
func (ps *PositionStore) SetWorld(w *World)
```

### Implementation Pattern
```go
// Z-index check order in GetZIndex (highest first for early exit)
if world.Cursors.Has(e)  { return ZIndexCursor }
if world.Shields.Has(e)  { return ZIndexShield }
if world.Drains.Has(e)   { return ZIndexDrain }
if world.Decays.Has(e)   { return ZIndexDecay }
if world.Nuggets.Has(e)  { return ZIndexNugget }
return ZIndexSpawnChar

// IsInteractable: only Characters with Sequence OR Nuggets
func IsInteractable(world *World, e Entity) bool {
    if world.Nuggets.Has(e) { return true }
    return world.Characters.Has(e) && world.Sequences.Has(e)
}

// Consumer pattern (EnergySystem)
entity := world.Positions.GetTopEntityFiltered(x, y, world, engine.IsInteractable)

// Consumer pattern (CursorRenderer)
entities := world.Positions.GetAllEntitiesAt(x, y)
displayEntity := engine.SelectTopEntityFiltered(entities, world, func(e engine.Entity) bool {
    return e != cursorEntity && world.Characters.Has(e)
})
```

### Files to Modify
1. `engine/z-index.go` - Add constants, enhance functions
2. `engine/position_store.go` - Multi-entity cells, new methods
3. `engine/world.go` - Wire PositionStore with World reference
4. `systems/energy.go` - Use GetTopEntityFiltered
5. `render/renderers/cursor.go` - Use z-index selection

## VERIFICATION
- `go build .` must succeed

## ENVIRONMENT

This project relies on `oto` and `beep` for audio, which requires CGO bindings to ALSA on Linux.

**Setup steps:**

1. **Fix Go Module Proxy Issues** (if DNS/network failures):
```bash
   export GOPROXY="https://goproxy.io,direct"
```

2. **Install ALSA Development Library**:
```bash
   apt-get install -y libasound2-dev
```

3. **Download Dependencies**:
```bash
   GOPROXY="https://goproxy.io,direct" go mod tidy
```

4. **Build**:
```bash
   GOPROXY="https://goproxy.io,direct" go build .
```