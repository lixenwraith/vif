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

## CURRENT TASK:

### Objective


### Reference Document


### Key Types


### Implementation Pattern


### Phase Flow


### Files to Modify


## VERIFICATION
- **DO NOT TRY TO TEST OR BUILD ANY PART OF THE CODE IF THE SCOPE IS DOCUMENTATION UPDATE**
- `go build .` must succeed
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
