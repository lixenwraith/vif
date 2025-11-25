# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.18+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

## CURRENT MISSION: Pure ECS Phase 2 - Resource System
**Objective:** Decouple systems from the `GameContext` God Object by introducing a generic Resource System.
**Core Change:** Move global state (Time, Config, Input) from `GameContext` to `World.Resources`.

### Implementation Phases
1.  **Infrastructure:** Implement `ResourceStore` and add it to `World`. (Done)
2.  **Migration:** systematically update systems (`Decay`, `Cleaner`, `Drain`, `Spawn`) to fetch data from `World.Resources` instead of `ctx`.
3.  **Cleanup:** Eventually remove these fields from `GameContext` once all consumers are migrated.

## ARCHITECTURE OVERVIEW

### 1. The World & Resources
The `World` struct now contains a `ResourceStore` for global data that doesn't fit into Components.

```go
type World struct {
    Resources      *ResourceStore // Thread-safe global resources
    Positions      *PositionStore
    // ... component stores
}
```

**Resource Access Pattern:**
Resources are accessed via generics. Prefer `MustGetResource` for core dependencies.

```go
// ✅ CORRECT PATTERN
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    // Fetch dependencies at start of update
    config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

    // Use them
    width := config.GameWidth
    now := timeRes.GameTime
}
```

### 2. Available Resources
*   **`*engine.ConfigResource`**: Dimensions (`GameWidth`, `GameHeight`, `ScreenWidth`, `ScreenHeight`).
*   **`*engine.TimeResource`**: Time data (`GameTime`, `RealTime`, `DeltaTime`, `FrameNumber`).
*   **`*engine.InputResource`**: Input state (`GameMode`, `CommandText`, `IsPaused`).

### 3. Entity Management
Entities are `uint64`. Creation is transactional.

```go
entity := With(
    WithPosition(world.NewEntity(), world.Positions, components.PositionComponent{X: 10, Y: 5}),
    world.Protections,
    components.ProtectionComponent{Mask: components.ProtectAll}, 
    ).Build()
```

## TESTING & TROUBLESHOOTING

### 1. Environment Setup (CRITICAL - PROVEN WORKING METHOD)
This project relies on `oto` and `beep` for audio, which requires CGO bindings to ALSA on Linux.

**EXACT STEPS THAT WORK (follow in order):**

1. **Fix Go Module Proxy Issues** (if you see DNS/network failures):
   ```bash
   export GOPROXY="https://goproxy.io,direct"
   ```

2. **Install ALSA Development Library** (required for audio CGO bindings):
   ```bash
   # Don't run apt-get update if it fails - just install directly
   apt-get install -y libasound2-dev
   ```

3. **Download Dependencies**:
   ```bash
   GOPROXY="https://goproxy.io,direct" go mod tidy
   ```

4. **Verify Installation**:
   ```bash
   GOPROXY="https://goproxy.io,direct" go test -race ./engine/... -v
   ```

### 2. Running Tests
Always run with the race detector enabled.
```bash
export GOPROXY="https://goproxy.io,direct"
go test -race ./...
```

### 3. Test Helpers
Use `NewTestGameContext` to initialize a valid ECS world.
*Note:* When testing migrated systems, ensure you manually populate the `ResourceStore` in your test setup if `NewTestGameContext` doesn't cover the specific resource state you need.

### 4. Common Pitfalls
*   **Context Usage:** Do NOT use `s.ctx.GameWidth` or `s.ctx.TimeProvider` in updated systems. Use `world.Resources`.
*   **Resource Pointers:** Resources are stored as pointers (e.g., `*ConfigResource`). Always request the pointer type.
*   **Component Pointers:** `Get()` returns a copy. You **MUST** call `Add()` to save changes back to the store.

## FILE STRUCTURE
```
vi-fighter/
├── engine/
│   ├── resources.go      # NEW: ResourceStore & definitions
│   ├── world.go          # Updated with Resources field
│   └── game.go           # Context initialization
├── systems/
│   ├── decay_system.go   # Migration Target
│   ├── cleaner_system.go # Migration Target
│   └── drain_system.go   # Migration Target
```
