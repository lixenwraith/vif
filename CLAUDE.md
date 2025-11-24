# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.18+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

## ARCHITECTURE OVERVIEW

### 1. The World (Generics)
The `World` struct no longer uses maps of `interface{}`. It uses explicit, typed generic stores.

```go
type World struct {
    // Explicit Stores
    Positions      *PositionStore // Specialized (Spatial Index + Mutex)
    Characters     *Store[components.CharacterComponent] // Generic RWMutex Store
    Sequences      *Store[components.SequenceComponent]
    // ...
}
```

### 2. Entity Management
Entities are `uint64`. Creation is transactional via the Builder pattern to ensure thread safety.

**Creation:**
```go
// ✅ CORRECT PATTERN
entity := world.NewEntity().
    WithPosition(world.Positions, components.PositionComponent{X: 10, Y: 5}).
    With(world.Characters, components.CharacterComponent{Rune: 'A'}).
    Build()
```

**Destruction:**
```go
world.DestroyEntity(entity) // Automatically cleans up from ALL stores
```

### 3. Querying
Iterate over entities using the `QueryBuilder`. It optimizes intersection by starting with the smallest store.

```go
// ✅ CORRECT PATTERN
entities := world.Query().
    With(world.Positions).
    With(world.Characters).
    Execute()

for _, e := range entities {
    // Stores are thread-safe (RLock internal)
    if pos, ok := world.Positions.Get(e); ok {
        // Logic...
    }
}
```

## TESTING & TROUBLESHOOTING

### 1. Environment Setup (CRITICAL)
This project relies on `oto` and `beep` for audio, which requires CGO bindings to ALSA on Linux.
**If tests fail to compile due to missing `asoundlib.h` or linker errors:**
1.  **Do not skip tests.**
2.  Install dependencies: `sudo apt-get update && sudo apt-get install -y libasound2-dev`
3.  Run `go mod tidy`.

### 2. Running Tests
Always run with the race detector enabled, as the engine relies heavily on concurrency.
```bash
go test -race ./...
```

### 3. Test Helpers
Use `NewTestGameContext` to initialize a valid ECS world without spinning up the full TCell screen.
```go
func TestMovement(t *testing.T) {
    ctx := engine.NewTestGameContext(80, 24, 80)
    // Audio is mocked or disabled in TestContext by default
    // ...
}
```

### 4. Common Pitfalls
*   **Spatial Index:** Never manually manipulate `world.Positions.spatialIndex`. Use `world.Positions.Move()` or `Add()`.
*   **Component Pointers:** Stores hold values, not pointers. modifying a component returned by `Get()` does **not** update the store. You must call `Add()` again to save changes.
    ```go
    val, _ := store.Get(e)
    val.Count++
    store.Add(e, val) // <--- Mandatory write-back
    ```
*   **Race Conditions:** If the race detector fails on a map access, check if you are writing to a component map while another system is querying it. Ensure `world.Update` locks are respected or use Atomics for high-frequency data (like Score/Heat).

## FILE STRUCTURE
```
vi-fighter/
├── engine/
│   ├── store.go          # Generic Store[T] implementation
│   ├── position_store.go # Spatially indexed store
│   ├── world.go          # Registry of all stores
│   └── game_state.go     # Atomic-based real-time state (Score, Heat)
├── systems/              # Game Logic (Update loops)
├── components/           # Pure data structs
├── constants/            # Config and Magic Numbers
└── cmd/                  # Main entry point
```