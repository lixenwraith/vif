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



## CURRENT TASK

**Phase: Splash Font Asset Generation**

### Objective
Generate procedural bitmap font data for the Splash visual feedback system.

### Prerequisite
- assets directory is currently being used to hold content files.
- rename the folder to `data`
- modify `assets` reference in `content/manager.go` and anywhere else to point to `data` before starting.
- `assets` will be a new package.
- Update `README.md` in the new `data` dir, repo readme, doc/architecture.md, and game.md at the end of the task.

### Requirements
- File: `assets/splash_font.go`
- Format: `var SplashFont = [95][12]uint16{...}`
- Coverage: ASCII 32-126 (space through tilde)
- Dimensions: 16 columns × 12 rows per character
- Bit order: MSB-first (bit 15 = column 0/leftmost)
- Style: Sans-serif block glyphs, readable at terminal scale

### Character Priority
1. **Critical**: A-Z, a-z, 0-9 (typing feedback)
2. **Important**: Common punctuation (.,;:'"!?-_)
3. **Standard**: Remaining printable ASCII

### Technical Constraints
- Each row is `uint16` where set bit = filled pixel
- Index calculation: `SplashFont[rune - 32]`
- No external font files; pure Go literal data
- Visually distinct glyphs; avoid ambiguity (0/O, 1/l/I)

### Design Guidelines
- Block style: thick strokes (2-3 pixels wide)
- Consistent baseline and cap height
- Adequate inter-character whitespace in glyph design
- Alphanumerics should be recognizable at 50% opacity

### Output
Single file `assets/splash_font.go` with complete bitmap data for all 95 characters.

### Verification
- `go build .` must pass
- Spot-check: 'A' bitmap should show recognizable letter shape