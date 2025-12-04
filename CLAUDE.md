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
**Refer to prompt if this section is empty**

### Objective
Implement dual-mode color rendering for shield component with Redmean-based 256-color mapping.

### Implementation Approach

**Phase 1: Color Infrastructure**
- Replace `RGBTo256` in `terminal/color.go` with 262KB pre-computed Redmean LUT
- Remove existing cube-based implementation entirely
- LUT populated at `init()`, runtime is O(1) table lookup

**Phase 2: Shield Dual-Mode**
- Refactor `ShieldRenderer` to use callback injection pattern
- TrueColor: `BlendScreen` with quadratic falloff gradient
- 256-Color: 3-zone grayscale (inner dark, outer light, edge tinted)

### Files to Modify

| File | Action |
|------|--------|
| `terminal/color.go` | Replace `RGBTo256` implementation, add `lut256`, `computeRedmean256`, `redmeanDistance` |
| `render/renderers/shields.go` | Full rewrite with callback injection pattern |

### Critical Patterns

1. **LUT Index Calculation**: `int(c.R>>2)<<12 | int(c.G>>2)<<6 | int(c.B>>2)`
2. **Callback Type**: `type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64)`
3. **Mode Selection**: Once per frame via `s.gameCtx.Terminal.ColorMode()`
4. **256 Zones**: `dist > 0.9` edge (tinted), `dist > 0.6` outer (light gray 188,188,188), else inner (dark gray 68,68,68)

### Reference
Full implementation code provided in conversation. Apply as complete file replacements.

### Verification
1. `go build .` must pass
2. User will manually test visuals in both color modes