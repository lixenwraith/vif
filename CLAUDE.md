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

## CURRENT TASK: Game Mechanics Update

### Objective
Implement new Heat/Energy/Shield/Drain mechanics as specified in `GAME_MECHANICS_UPDATE.md`.

### Reference Document
`GAME_MECHANICS_UPDATE.md` at repo root - **read this first**, update phase checkboxes as you complete each phase.

### Key Mechanics Summary
| System | Rule |
|--------|------|
| Heat | 0-100 range, `floor(Heat/10)` = drain count |
| Energy | Can go negative, funds Shield defense |
| Shield | Active when `Sources != 0 AND Energy > 0` |
| Drains | Spawn on Heat, despawn on Heat drop / collision / energy-zero (if !shield) |

### Implementation Approach
1. **Bitmask for Shield Sources**: `ShieldComponent.Sources uint8` replaces boolean `Active`
2. **Shield Active Check**: Helper function `isShieldActive()` encapsulates `Sources != 0 && Energy > 0`
3. **Collision Priority**: Drain-Drain → Cursor (shield check) → Entity collisions
4. **Energy Costs**: Shield zone (100/drain/tick), Passive (1/sec), Cursor collision when shielded (100/tick)

### Files to Modify
| Phase | Files |
|-------|-------|
| 1 | `constants/gameplay.go`, `components/shield.go` |
| 2 | `systems/boost.go`, `render/renderers/shields.go`, `render/context.go` |
| 3-6 | `systems/drain.go` |

### Critical Patterns
- **State Access**: Use `GameState` atomics (`GetHeat()`, `GetEnergy()`, `AddEnergy()`)
- **Time**: Use `engine.MustGetResource[*engine.TimeResource](world.Resources).GameTime`
- **Position Queries**: `world.Positions.GetAllAt(x, y)` returns all entities at cell
- **Component Updates**: Always `world.Drains.Add(entity, drain)` after mutation

## VERIFICATION
- **DO NOT TRY TO TEST OR BUILD ANY PART OF THE CODE IF THE SCOPE IS DOCUMENTATION UPDATE**
- `go build .` must succeed after each phase
- Delegate to user manual test on first network error.

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