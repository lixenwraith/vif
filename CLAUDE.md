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

## Documentation

- After completion of each task, update `doc/architecture.md`, deprecating and deleting the old information.
- **DO NOT** include any migration or implementation steps.
- If implementation consists of multiple steps, update the documents at the last step.

## Directive Reference for Claude Code

These directives are meta-instructions placed in prompts to control Claude Code behavior:

| Directive | Meaning |
|-----------|---------|
| `[full]` | Output complete file content, not snippets |
| `[block]` | Output only modified functions/structs with context placeholders |
| `[diff]` | Output minimal change with surrounding context lines |
| `[verify]` | Run build/test commands after changes |
| `[[code]]` | Action directive: generate code for preceding requirements |

---

## CURRENT TASK

**Refer to prompt if this section is empty**

### Objective

### Reference Document

### Implementation Sequence

### Key Patterns

### Files to Modify

### Import Requirements

### Verification