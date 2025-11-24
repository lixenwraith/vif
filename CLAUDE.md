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

**Creation (using standalone functions):**
```go
// ✅ CORRECT PATTERN - With() and WithPosition() are standalone functions
entity := With(
    WithPosition(world.NewEntity(), world.Positions, components.PositionComponent{X: 10, Y: 5}),
    world.Characters,
    components.CharacterComponent{Rune: 'A'},
).Build()

// Or for single component:
entity := With(world.NewEntity(), world.Characters, components.CharacterComponent{Rune: 'A'}).Build()
entity := WithPosition(world.NewEntity(), world.Positions, components.PositionComponent{X: 5, Y: 10}).Build()
```

**Note:** `With()` and `WithPosition()` are **standalone generic functions**, not methods on EntityBuilder. They take the builder as the first parameter and return it for chaining.

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

### 1. Environment Setup (CRITICAL - PROVEN WORKING METHOD)
This project relies on `oto` and `beep` for audio, which requires CGO bindings to ALSA on Linux.

**EXACT STEPS THAT WORK (follow in order):**

1. **Fix Go Module Proxy Issues** (if you see DNS/network failures):
   ```bash
   export GOPROXY="https://goproxy.io,direct"
   ```
   This bypasses issues with storage.googleapis.com and uses goproxy.io as primary, falling back to direct.

2. **Install ALSA Development Library** (required for audio CGO bindings):
   ```bash
   # Don't run apt-get update if it fails - just install directly
   apt-get install -y libasound2-dev
   ```
   If `apt-get update` fails with repository errors, skip it and install directly.

3. **Download Dependencies with Correct Proxy**:
   ```bash
   GOPROXY="https://goproxy.io,direct" go mod tidy
   ```

4. **Verify Installation**:
   ```bash
   GOPROXY="https://goproxy.io,direct" go test -race ./engine/... -v
   ```
   If this succeeds, the environment is ready.

**Common Failure Patterns & Solutions:**
- ❌ `Package alsa was not found` → Install `libasound2-dev` (step 2)
- ❌ `dial tcp: lookup storage.googleapis.com` → Use `GOPROXY="https://goproxy.io,direct"` (step 1)
- ❌ `403 Forbidden` on ppa.launchpadcontent.net → Skip `apt-get update`, install directly
- ❌ Complete network failure / DNS unavailable → Configure DNS (`echo "nameserver 8.8.8.8" > /etc/resolv.conf`) or work offline if dependencies are cached

**Network-Restricted Environments:**
If the environment has no external network access (all DNS lookups fail), you may need to:
1. Ensure dependencies are already cached in `$GOPATH/pkg/mod` from a previous session
2. Work on test code migration/fixes without running the build
3. Request a different environment with network access for dependency download

**DO NOT:**
- Modify production code to fix test environment issues
- Skip tests due to environment problems - fix the environment instead
- Give up on network errors - use the alternative proxy above or configure DNS

### 2. Running Tests
Always run with the race detector enabled, as the engine relies heavily on concurrency.
```bash
export GOPROXY="https://goproxy.io,direct"
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