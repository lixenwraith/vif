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

## CURRENT TASK: Gold Bootstrap Decoupling

### Objective
Move game initialization delay from `GoldSystem.Update()` to `ClockScheduler` using a new `PhaseBootstrap` phase. Remove `FirstUpdateTime` and `InitialSpawnComplete` from GameState.

### Reference Document
- `engine/clock_scheduler.go` - GamePhase enum and processTick switch
- `engine/game_state.go` - GameState struct with phase management
- `systems/gold.go` - Current bootstrap logic to remove

### Key Types
```go
// Updated GamePhase enum
const (
    PhaseBootstrap GamePhase = iota  // NEW: Initial delay state
    PhaseNormal
    PhaseGoldActive
    PhaseGoldComplete
    PhaseDecayWait
    PhaseDecayAnimation
)

// GameState changes
type GameState struct {
    // ADD:
    GameStartTime time.Time
    
    // REMOVE:
    // FirstUpdateTime time.Time
    // InitialSpawnComplete bool
}

// New GameState methods
func (gs *GameState) GetGameStartTime() time.Time
func (gs *GameState) ResetGameStart(now time.Time)  // For :new command
func (gs *GameState) TransitionPhase(to GamePhase, now time.Time) bool
```

### Implementation Pattern
```go
// ClockScheduler.processTick - new case
case PhaseBootstrap:
    if gameNow.Sub(cs.ctx.State.GetGameStartTime()) >= constants.GoldInitialSpawnDelay {
        cs.ctx.State.TransitionPhase(PhaseNormal, gameNow)
    }

// GoldSystem.Update - simplified logic
func (s *GoldSystem) Update(world *engine.World, dt time.Duration) {
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
    now := timeRes.GameTime
    
    goldSnapshot := s.ctx.State.ReadGoldState(now)
    phaseSnapshot := s.ctx.State.ReadPhaseState(now)
    
    // Only spawn in Normal phase when no gold active
    if phaseSnapshot.Phase == engine.PhaseNormal && !goldSnapshot.Active {
        s.spawnGold(world)
    }
}

// :new command reset
ctx.State.ResetGameStart(ctx.PausableClock.Now())

// CanTransition update
case PhaseBootstrap:
    return to == PhaseNormal
```

### Phase Flow
```
PhaseBootstrap ──[delay]──> PhaseNormal ──[gold spawns]──> PhaseGoldActive
      ^                                                          │
      │                                                          v
      │                     PhaseDecayAnimation <── PhaseDecayWait <── PhaseGoldComplete
      │                            │
      └────────[:new cmd]──────────┘
```

### Files to Modify
1. `engine/clock_scheduler.go` - Add PhaseBootstrap const, case in processTick
2. `engine/game_state.go` - Add GameStartTime, remove old fields, add methods
3. `systems/gold.go` - Strip all bootstrap logic
4. `modes/commands.go` - Update handleNewCommand

## VERIFICATION
- `go build .` must succeed

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