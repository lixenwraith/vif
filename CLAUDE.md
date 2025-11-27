# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.25+).

## ARCHITECTURE OVERVIEW

### Core Systems
- **ECS**: Generics-based `World` with `Store[T]` and `PositionStore` (spatial hash).
- **Game Loop**: Fixed 50ms tick (`ClockScheduler`) decoupled from rendering (`TerminalRenderer`).
- **Input**: `InputHandler` processes `tcell` events, managing state transitions between Modes.

### Resources
- **Context**: `GameContext` acts as the root state container.
- **Resources**: `TimeResource`, `ConfigResource`, `InputResource` stored in `World.Resources`.

### Overlay Management
- **State**: `GameContext` holds `OverlayState` (Active, Title, Content).
- **Mode**: `ModeOverlay` hijacks input for modal interactions.
- **Render**: `TerminalRenderer` draws overlay on top of all other layers.

## TASK:

### 1. Feature: Interactive Debug/Help Overlay
**Goal**:
- Create a modal popup system triggered by commands.
- Support `:d`/`:debug` and `:h`/`:help` commands.
- Hijack input/render loop to show a bordered window covering 80% of the screen.

## FILE STRUCTURE
- File in scope of task
```
vi-fighter/
├── engine/
│   └── game_context.go    # MODIFY: Add ModeOverlay and Overlay state fields
├── render/
│   ├── colors.go          # MODIFY: Add overlay specific colors
│   └── terminal_renderer.go # MODIFY: Implement drawOverlay method
├── modes/
│   ├── input.go           # MODIFY: Add handleOverlayMode dispatch logic
│   └── commands.go        # MODIFY: Add handlers for debug/help commands
└── constants/
    └── ui.go              # MODIFY: Add Overlay layout constants
```

## VERIFICATION
**NO TEST FILES IN REPO**
- Check if the app compiles

## ENVIRONMENT

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
   GOPROXY="https://goproxy.io,direct" go test -race ./... -v
   ```