# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go.
**Current Phase**: Refactoring and Cleanup (Q4 2025).
**Focus**: Standardization of naming conventions, centralization of visual definitions, and removal of dead code.

## ARCHITECTURE OVERVIEW

### Core Systems
- **ECS Architecture**: Generics-based (`engine/ecs.go`, `engine/world.go`).
- **Render Pipeline**: `tcell`-based, centralized in `render/`.
- **Game Loop**: Managed by `ClockScheduler` (ticks) and `PausableClock` (time).

### Naming Conventions (Strict)
- **Decay System**: Use `Decay` for all related structs, constants, and variables. (e.g., `DecayComponent`, `DecaySystem`). Avoid `FallingDecay`.
- **Colors**: All color definitions must reside in `render/colors.go`. No inline `tcell.NewRGBColor` allowed in logic files.

## CURRENT TASK: Refactoring & Standardization

### 1. Color Centralization
**Goal**: Move all RGB definitions to `render/colors.go`.
- Check `render/terminal_renderer.go` for inline colors (Ping, Heat Meter, Flash).
- Check systems for any ad-hoc style definitions.

### 2. Rename: FallingDecay -> Decay
**Goal**: Standardize naming for the decay mechanic.
- **Component**: `FallingDecayComponent` -> `DecayComponent`
- **Store**: `World.FallingDecays` -> `World.Decays`
- **Constants**: `FallingDecay...` -> `Decay...`
- **Methods**: `drawFallingDecay` -> `drawDecay`, `spawnFallingEntities` -> `spawnDecayEntities`.

### 3. Cleanup
**Goal**: Remove unused scheduler methods.
- Remove `GetTickCount`, `IsRunning`, `GetTickInterval` from `engine/clock_scheduler.go`.

## VERIFICATION COMMANDS
```bash
# Verify no inline colors remain (should return minimal results)
grep -r "NewRGBColor" . | grep -v "render/colors.go"

# Verify rename (should return 0 results)
grep -r -i "FallingDecay" .

# Build check
go build ./...