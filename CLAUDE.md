# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.18+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

### Implementation Pattern


## ARCHITECTURE OVERVIEW

### 1. The World & Resources
The `World` struct contains a `ResourceStore` for global data.

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
Entities are `uint64`. Use the simple three-step creation pattern:

```go
// Step 1: Create entity
entity := world.CreateEntity()

// Step 2: Add components
world.Positions.Add(entity, components.PositionComponent{X: 10, Y: 5})
world.Protections.Add(entity, components.ProtectionComponent{Mask: components.ProtectAll})

// For collision-sensitive spawning, use batches:
batch := world.Positions.BeginBatch()
batch.Add(entity, pos)
if err := batch.Commit(); err != nil {
    world.DestroyEntity(entity)  // Cleanup on collision
    return false
}
// Then add other components after positions are committed
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

### 4. Common Pitfalls

## FILE STRUCTURE
