## Updated CLAUDE.md

```markdown
# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.24+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

## CURRENT TASK: Flash System Expansion

### Overview
Expand the destruction flash effect (currently only on cleaner sweep) to all entity destruction events:
- Drain collisions (sequences, nuggets, falling decay, gold)
- Decay terminal destruction (Red at LevelDark)
- Nugget collisions from falling decay

### Design Decisions

| Aspect | Decision |
|--------|----------|
| Flash Duration | 300ms (increased from 150ms for visibility) |
| Centralization | New FlashSystem (Priority 35) handles cleanup |
| Spawn Helper | Package-level `SpawnDestructionFlash` in systems package |
| CleanerSystem | Delegates to helper; no longer manages flash lifecycle |

### Files to Modify

| File | Action | Description |
|------|--------|-------------|
| `constants/cleaners.go` | **MODIFY** | Add `DestructionFlashDuration = 300` |
| `constants/game.go` | **MODIFY** | Add `PriorityFlash = 35` |
| `systems/flash.go` | **NEW** | FlashSystem + SpawnDestructionFlash helper |
| `cmd/vi-fighter/main.go` | **MODIFY** | Register FlashSystem |
| `systems/cleaner.go` | **MODIFY** | Use helper, remove cleanup |
| `systems/decay.go` | **MODIFY** | Add flash on terminal decay + nugget hit |
| `systems/drain.go` | **MODIFY** | Add flash on all collision types |

---

## NEW FILE: systems/flash.go

```go
package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// FlashSystem manages the lifecycle of visual flash effects
type FlashSystem struct {
	ctx *engine.GameContext
}

func NewFlashSystem(ctx *engine.GameContext) *FlashSystem {
	return &FlashSystem{ctx: ctx}
}

func (s *FlashSystem) Priority() int {
	return constants.PriorityFlash
}

func (s *FlashSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	entities := world.Flashes.All()
	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		if now.Sub(flash.StartTime).Milliseconds() >= int64(flash.Duration) {
			world.DestroyEntity(entity)
		}
	}
}

// SpawnDestructionFlash creates a flash effect at the given position.
// Call from any system when destroying an entity with visual feedback.
func SpawnDestructionFlash(world *engine.World, x, y int, char rune, now time.Time) {
	flash := components.FlashComponent{
		X:         x,
		Y:         y,
		Char:      char,
		StartTime: now,
		Duration:  constants.DestructionFlashDuration,
	}

	entity := world.CreateEntity()
	world.Flashes.Add(entity, flash)
}
```

---

## MODIFICATION PATTERNS

### Spawn Flash Before Destroy
```go
// Pattern: get position/char before destruction
if pos, ok := world.Positions.Get(entity); ok {
    if char, ok := world.Characters.Get(entity); ok {
        timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
        SpawnDestructionFlash(world, pos.X, pos.Y, char.Rune, timeRes.GameTime)
    }
}
world.DestroyEntity(entity)
```

### Spawn Flash for FallingDecay
```go
// FallingDecay has position in component, not PositionStore
if decay, ok := world.FallingDecays.Get(entity); ok {
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
    SpawnDestructionFlash(world, decay.Column, int(decay.YPosition), decay.Char, timeRes.GameTime)
}
```

---

## VERIFICATION

```bash
go build ./...
```

Visual test:
1. Start game, let drain spawn and collide with sequences → flash
2. Let decay animation destroy Red/Dark characters → flash
3. Collect nugget with drain → flash
4. Let falling decay hit nugget → flash
5. Drain collides with gold sequence → all characters flash

---

## ARCHITECTURE REFERENCE

### Resource Access Pattern
```go
timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
now := timeRes.GameTime
```

### System Priorities
```
PriorityBoost   = 5
PriorityEnergy  = 10
PrioritySpawn   = 15
PriorityNugget  = 18
PriorityGold    = 20
PriorityCleaner = 22
PriorityDrain   = 25
PriorityDecay   = 30
PriorityFlash   = 35  // NEW - runs last to clean up expired flashes
```

---

## TESTING & TROUBLESHOOTING

### Environment Setup
```bash
export GOPROXY="https://goproxy.io,direct"
apt-get install -y libasound2-dev
go mod tidy
```

### Build Check
```bash
go build ./...
```

---

## FILE STRUCTURE

```
vi-fighter/
├── components/
│   └── flash.go          # FlashComponent (existing)
├── constants/
│   ├── cleaners.go       # MODIFY: add DestructionFlashDuration
│   └── game.go           # MODIFY: add PriorityFlash
├── systems/
│   ├── flash.go          # NEW: FlashSystem + SpawnDestructionFlash
│   ├── cleaner.go        # MODIFY: use helper, remove cleanup
│   ├── decay.go          # MODIFY: add flash spawning
│   └── drain.go          # MODIFY: add flash spawning
└── cmd/vi-fighter/
    └── main.go           # MODIFY: register FlashSystem
```