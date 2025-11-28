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

## CURRENT TASK: Renderer Migration

### Objective
Migrate from monolithic `TerminalRenderer` to System Render Interface pattern where each system owns its rendering logic.

### Reference Document
`RENDER_MIGRATION.md` at repo root contains:
- Phase status tracking
- Core type signatures
- Legacy function mapping to new renderers
- Migration rules and patterns

### Key Types
```go
type RenderPriority int  // 0=Background, 100=Grid, 200=Entities, 300=Effects, 350=Drain, 400=UI, 500=Overlay

type RenderContext struct {
    GameTime, FrameNumber, DeltaTime, IsPaused
    CursorX, CursorY, GameX, GameY, GameWidth, GameHeight, Width, Height
}

type SystemRenderer interface {
    Render(ctx RenderContext, world *engine.World, buf *RenderBuffer)
}

type VisibilityToggle interface {
    IsVisible() bool
}
```

### Migration Pattern
1. Create renderer struct in `render/renderers/`
2. Implement `SystemRenderer.Render()` method
3. Register with orchestrator at appropriate priority
4. Comment out corresponding legacy draw call
5. Verify visual correctness

## FILE STRUCTURE
```
vi-fighter/
├── render/
│   ├── priority.go        # RenderPriority type and constants
│   ├── context.go         # RenderContext struct
│   ├── cell.go            # RenderCell type
│   ├── buffer.go          # RenderBuffer implementation
│   ├── interface.go       # SystemRenderer, VisibilityToggle interfaces
│   ├── orchestrator.go    # RenderOrchestrator
│   ├── buffer_screen.go   # BufferScreen migration shim
│   ├── legacy_adapter.go  # LegacyAdapter for transition
│   ├── terminal_renderer.go # Legacy renderer (being deprecated)
│   ├── colors.go          # Color constants
│   └── renderers/         # New SystemRenderer implementations
│       ├── heat_meter.go
│       ├── line_numbers.go
│       ├── column_indicators.go
│       ├── status_bar.go
│       ├── ping_grid.go
│       ├── shields.go
│       ├── characters.go
│       ├── effects.go
│       ├── drain.go
│       ├── cursor.go
│       └── overlay.go
├── engine/
│   ├── world.go           # ECS World with component stores
│   ├── game_context.go    # GameContext root state
│   └── resources.go       # TimeResource, ConfigResource
├── main.go                # Game loop, orchestrator wiring
└── RENDER_MIGRATION.md    # Migration reference document
```

## VERIFICATION
- `go build .` must succeed
- Visual verification: game renders correctly
- No panics on resize or mode transitions

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