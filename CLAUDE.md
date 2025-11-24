# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.18+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

## CURRENT MISSION: Pure ECS Migration & Protection Systems
**Objective:** Migrate cursor state and spawn tracking to a pure ECS model to improve consistency and support advanced mechanics.
**Core Change:** Replace Global Atomics with ECS Components.

### Implementation Phases
1.  **Infrastructure:** Create `ProtectionComponent`, `CursorComponent`, and associated stores. Update `World` to support protection (indestructible entities).
2.  **Cursor Migration:** Replace `GameState.CursorX/Y` with `PositionStore`. Refactor InputHandler to write directly to ECS (0-latency).
3.  **Spawn Census:** Replace atomic color counters with per-frame O(n) iteration of entities to enforce spawn limits without drift.

## ARCHITECTURE OVERVIEW

### 1. The World (Generics)
The `World` struct uses explicit, typed generic stores.

```go
type World struct {
    Positions      *PositionStore // Specialized (Spatial Index + Mutex)
    Characters     *Store[components.CharacterComponent]
    Cursors        *Store[components.CursorComponent]
    Protections    *Store[components.ProtectionComponent]
    // ...
}
```

### 2. Entity Management
Entities are `uint64`. Creation is transactional.

**Creation Pattern (Strict):**
Use standalone functions `With()` and `WithPosition()`, not method chaining on the builder itself.
```go
entity := With(
    WithPosition(world.NewEntity(), world.Positions, components.PositionComponent{X: 10, Y: 5}),
    world.Protections,
    components.ProtectionComponent{Mask: components.ProtectAll}, 
    ).Build()
```

**Destruction:**
`World.DestroyEntity(e)` cleans up from ALL stores.
*Critical:* It MUST respect `ProtectionComponent.Mask == ProtectAll` and refuse to destroy such entities.

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
*   **Update Loops:** Do not allocate memory (maps/slices) in `Update()`. Re-use struct fields.
*   **Component Pointers:** `Get()` returns a copy. You **MUST** call `Add()` to save changes back to the store.
    ```go
    val, _ := store.Get(e)
    val.YPosition += speed // Modification on local copy
    store.Add(e, val)      // <--- Mandatory write-back
    ```
*   **Latching Logic:** When implementing the Decay fix, ensure the Latch is updated *only* after a successful cell processing step, and ensure it blocks interaction even if `SpawnSystem` puts a new entity there in the same frame.

## FILE STRUCTURE
```
vi-fighter/
├── engine/
│   ├── store.go          # Generic Store[T]
│   ├── position_store.go # Spatial Index
│   └── world.go          # World struct, DestroyEntity
├── components/
│   ├── protection.go     # NEW: ProtectionComponent & Flags
│   └── cursor.go         # NEW: CursorComponent
├── systems/
│   └── spawn_system.go   # TARGET: Census implementation
```