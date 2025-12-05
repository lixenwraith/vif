// FILE: CLAUDE.md
# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.24+).

## ARCHITECTURE OVERVIEW

### Core Systems
- **ECS**: Generics-based `World` with `Store[T]` and `PositionStore` (spatial hash).
- **Game Loop**: Fixed 50ms tick (`ClockScheduler`) decoupled from rendering.
- **Render Pipeline**: `RenderOrchestrator` coordinates `SystemRenderer` implementations. Frame update 16ms.
- **Input**: `InputHandler` processes `tcell` events, managing state transitions between Modes.

### Resources
- **Context**: `GameContext` acts as the root state container.
- **Resources**: `TimeResource`, `ConfigResource`, `InputResource` stored in `World.Resources`.

### Render Architecture
- **Orchestrator**: `RenderOrchestrator` manages render pipeline lifecycle.
- **Buffer**: `RenderBuffer` is a dense grid for compositing; zero-alloc after init.
- **Renderers**: Individual `SystemRenderer` implementations in `render/renderers/`.
- **Priority**: `RenderPriority` constants determine render order (lower first).

## DEVELOPMENT NOTES

When implementing new features or modifying existing systems, always:
- Follow strict ECS principles (entities = IDs, components = data, systems = logic)
- Use the Resource System for global shared data access
- Maintain thread safety with atomics for real-time state, mutexes for clock-tick state
- Respect the render pipeline architecture and priority ordering
- Test with `go build .` after each significant change

## VERIFICATION
- **DO NOT TRY TO TEST OR BUILD ANY PART OF THE CODE IF THE SCOPE IS DOCUMENTATION UPDATE**
- `go build .` must succeed after each phase
- Delegate to user manual test on first network error.

## ENVIRONMENT

vi-fighter uses **pure Go** with no CGO dependencies.

**Prerequisites:**
- Go 1.24 or later
- Terminal with color support (truecolor with 256-color mix/fallback)
- (Optional) System audio backend for sound effects:
    - Linux: PulseAudio (`pacat`), PipeWire (`pw-cat`), or ALSA (`aplay`)
    - FreeBSD: PulseAudio or OSS (`/dev/dsp`)
    - Fallback: SoX (`play`) or FFmpeg (`ffplay`)
    - Game runs silently if no audio backend is available

**Build:**
```bash
go build -o vi-fighter ./cmd/vi-fighter
```

**Run:**
```bash
./vi-fighter
```

**Audio Notes:**
- Audio system auto-detects available backends at runtime
- No compilation flags or external libraries required
- Silent mode fallback ensures game works without audio


## CURRENT TASK

**Prompt can override Environment and Verification section instructions**
**Refer to prompt for complete implementation code**

### Objective
Implement stencil-based post-processing pipeline for selective visual effects (dim, grayscale) on render buffer.

### Architecture
```
Renderers → SetWriteMask() → RenderBuffer (cells[] + masks[])
                                    ↓
                            Post-Processors (MutateDim/MutateGrayscale)
                                    ↓
                            FlushToTerminal
```

### Implementation Approach

**Phase 1: Core Infrastructure**
- Create `render/mask.go` with bitmask constants
- Extend `RenderBuffer` with `masks []uint8`, `currentMask uint8`
- Add `Grayscale()` and `Lerp()` to `render/rgb.go`
- Add `MutateDim()` and `MutateGrayscale()` mutation methods

**Phase 2: State & Logic**
- Add `GrayoutActive`, `GrayoutStartTime` atomics to `GameState`
- Modify `CleanerSystem.spawnCleaners()` for phantom trigger

**Phase 3: Integration**
- Inject `SetWriteMask()` calls into ALL existing renderers
- Remove hardcoded pause dim from `CharactersRenderer`
- Create `DimRenderer` and `GrayoutRenderer` post-processors
- Register post-processors in `main.go` at priorities 390/395

### Critical Patterns

1. **Fg/Bg Granularity**: `touched=true` → mutate both; `touched=false, mask!=0` → mutate Fg only (preserve `RgbBackground`)
2. **Mask Assignment**: Every draw method (`Set`, `SetFgOnly`, `SetBgOnly`, `SetWithBg`) writes `currentMask` to `masks[idx]`
3. **Mutation Loop**: Iterate `b.cells`, check `masks[i]&targetMask != 0`, NOT `touched[i]`

### Files Summary

| File | Action |
|------|--------|
| `render/mask.go` | CREATE |
| `render/renderers/post_process.go` | CREATE |
| `render/rgb.go` | ADD `Grayscale`, `Lerp` |
| `render/buffer.go` | REPLACE |
| `engine/game_state.go` | ADD grayout fields/methods |
| `systems/cleaner.go` | MODIFY `spawnCleaners` |
| `render/renderers/*.go` | ADD `SetWriteMask` to all |
| `cmd/vi-fighter/main.go` | ADD post-processor registrations |

### Verification
1. `go build .` must pass
2. Manual test: `:` enters pause → game content dims, UI unchanged
3. Manual test: Cleaner phase with no red targets → entities flash grayscale