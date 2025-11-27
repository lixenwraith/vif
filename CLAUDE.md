# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.24+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.

**Go Version:** 1.24+

## ARCHITECTURE OVERVIEW

### Event-Driven Communication
The game uses an **EventRouter** pattern for decoupled system communication:
```
InputHandler ──push──▶ EventQueue ──consume──▶ EventRouter ──dispatch──▶ Systems
                                                    │
                                                    ├──▶ EnergySystem (typing, transactions)
                                                    └──▶ CleanerSystem (spawn requests)
```

**Key Principle**: Systems do NOT call each other directly. They communicate via:
1. **Events** (async, via EventQueue/EventRouter)
2. **GameState** (shared atomics for cross-system state)
3. **World queries** (read component data)

### System Execution Order
```
1. ClockScheduler.processTick()
   ├── EventRouter.DispatchAll()     # Events processed FIRST
   │   ├── EnergySystem.HandleEvent()
   │   └── CleanerSystem.HandleEvent()
   └── World.Update()                # Systems run in priority order
       ├── BoostSystem      (5)
       ├── EnergySystem     (10)     # Time-based logic only
       ├── SpawnSystem      (15)
       ├── NuggetSystem     (18)
       ├── GoldSystem       (20)
       ├── CleanerSystem    (22)     # Physics only, no event consumption
       ├── DrainSystem      (25)
       ├── DecaySystem      (30)
       └── FlashSystem      (35)
```

### Event Types

| Event | Producer | Consumer | Payload |
|-------|----------|----------|---------|
| `EventCharacterTyped` | InputHandler | EnergySystem | `*CharacterTypedPayload{Char, X, Y}` |
| `EventEnergyTransaction` | InputHandler | EnergySystem | `*EnergyTransactionPayload{Amount, Source}` |
| `EventCleanerRequest` | EnergySystem | CleanerSystem | `nil` |
| `EventDirectionalCleanerRequest` | InputHandler, EnergySystem | CleanerSystem | `*DirectionalCleanerPayload{OriginX, OriginY}` |
| `EventCleanerFinished` | CleanerSystem | (observers) | `nil` |
| `EventGoldSpawned` | GoldSystem | (observers) | `nil` |
| `EventGoldComplete` | EnergySystem | (observers) | `nil` |

### Shared State (GameState)

| Field | Type | Purpose |
|-------|------|---------|
| `Energy` | `atomic.Int64` | Player score |
| `Heat` | `atomic.Int64` | Combo meter (0-100) |
| `ActiveNuggetID` | `atomic.Uint64` | Current nugget entity ID |
| `BoostEnabled` | `atomic.Bool` | Speed boost active |
| `FrameNumber` | `atomic.Int64` | Current frame |
| `GameTicks` | `atomic.Uint64` | Total clock ticks since start |
| `CurrentAPM` | `atomic.Uint64` | Actions Per Minute (calculated) |

---

## MODIFICATION PATTERNS

### Adding a New Event Type

1. **Define in `engine/events.go`**:
```go
const (
    // ... existing ...
    EventMyNewEvent
)

type MyNewEventPayload struct {
    Field1 int
    Field2 string
}

// Update String() method
```

2. **Push from producer**:
```go
payload := &engine.MyNewEventPayload{Field1: 42}
ctx.PushEvent(engine.EventMyNewEvent, payload, ctx.PausableClock.Now())
```

3. **Consume in system** (implement EventHandler):
```go
func (s *MySystem) EventTypes() []engine.EventType {
    return []engine.EventType{engine.EventMyNewEvent}
}

func (s *MySystem) HandleEvent(world *engine.World, event engine.GameEvent) {
    if payload, ok := event.Payload.(*engine.MyNewEventPayload); ok {
        // Process
    }
}
```

4. **Register with router** (in `main.go`):
```go
clockScheduler.RegisterEventHandler(mySystem)
```

### Reading Cross-System State
```go
// From InputHandler or any non-system code:
nuggetID := engine.Entity(h.ctx.State.GetActiveNuggetID())
if nuggetID != 0 {
    pos, ok := h.ctx.World.Positions.Get(nuggetID)
}

// From a System:
energy := s.ctx.State.GetEnergy()
heat := s.ctx.State.GetHeat()
```

### Pushing Events from InputHandler
```go
// Typing event
payload := &engine.CharacterTypedPayload{Char: r, X: x, Y: y}
h.ctx.PushEvent(engine.EventCharacterTyped, payload, h.ctx.PausableClock.Now())

// Energy cost
payload := &engine.EnergyTransactionPayload{Amount: -10, Source: "Jump"}
h.ctx.PushEvent(engine.EventEnergyTransaction, payload, h.ctx.PausableClock.Now())

// Directional cleaners
payload := &engine.DirectionalCleanerPayload{OriginX: x, OriginY: y}
h.ctx.PushEvent(engine.EventDirectionalCleanerRequest, payload, h.ctx.PausableClock.Now())
```

---

## DECOUPLING RULES

### InputHandler Independence
- **NEVER** hold System references (no `*EnergySystem`, `*NuggetSystem`, etc.)
- **NEVER** call System methods directly
- **DO** read from `GameState` for shared state
- **DO** query `World` for entity/component data
- **DO** push events for actions that affect game state

### System Independence
- **NEVER** call other System methods directly (except via DI for tightly coupled logic like GoldSystem↔EnergySystem)
- **DO** implement `EventHandler` interface for event consumption
- **DO** push events to trigger other systems
- **DO** read/write `GameState` for shared atomics

### Event vs Direct Call Decision

| Scenario | Use Event | Use Direct |
|----------|-----------|------------|
| Cross-system trigger | ✅ | ❌ |
| Atomic state change | ❌ | ✅ (`GameState`) |
| Input → System | ✅ | ❌ |
| System → System (same tick) | ✅ | ❌ |
| Reading component data | ❌ | ✅ (`World.Query()`) |

---

## CURRENT TASK CONTEXT

### Event Router Migration (Completed)
- CleanerSystem uses EventRouter for spawn events
- EnergySystem uses EventRouter for typing/transaction events
- InputHandler pushes events, no system dependencies

### Pending Cleanup
- Evaluate DrainSystem/DecaySystem nuggetSystem references
- Consider future event types for gold sequence completion

---

## VERIFICATION CHECKLIST

Trace the flow to confirm.

### Event Flow
- [ ] Typing in Insert mode triggers `EventCharacterTyped`
- [ ] Tab jump triggers `EventEnergyTransaction`
- [ ] Enter at heat≥10 triggers `EventDirectionalCleanerRequest`
- [ ] Gold completion at max heat triggers `EventCleanerRequest`
- [ ] Nugget at max heat triggers `EventDirectionalCleanerRequest`

### State Consistency
- [ ] `GameState.ActiveNuggetID` updates on nugget spawn/clear
- [ ] Energy deduction visible after ~50ms (event processing delay)
- [ ] Heat updates immediately on typing

---

## ENVIRONMENT
```bash
export GOPROXY="https://goproxy.io,direct"
apt-get install -y libasound2-dev
go mod tidy
go build ./...
```

## FILE STRUCTURE
```
engine/
├── ecs.go              # Entity, System interface
├── events.go           # EventType, payloads, EventQueue
├── event_router.go     # EventHandler interface, EventRouter
├── game_context.go     # GameContext (World, State, queues)
├── game_state.go       # GameState (atomics, snapshots)
├── clock_scheduler.go  # ClockScheduler (tick loop, event dispatch)
└── ...

systems/
├── energy.go           # EnergySystem (typing logic, EventHandler)
├── cleaner.go          # CleanerSystem (spawn/physics, EventHandler)
├── nugget.go           # NuggetSystem (spawn logic)
└── ...

modes/
├── input.go            # InputHandler (event producer, no system deps)
└── ...