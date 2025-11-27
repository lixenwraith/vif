# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.24+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

## CURRENT TASK: Directional Cleaners & Input Remapping

### Overview
Expand cleaner system to support 4-directional spawning from cursor position. Remap Enter/ESC keys in Normal mode.

### Design Decisions

| Aspect | Decision |
|--------|----------|
| Direction Detection | Implicit via velocity (VelocityX==0 → vertical) |
| Position Lock | Row/column locked at spawn time in component |
| Event Payload | `DirectionalCleanerPayload{OriginX, OriginY int}` |
| Trigger Sources | Nugget at max heat, Enter key at heat≥10 |
| Animation Duration | Same as existing cleaner (`CleanerAnimationDuration`) |

### Files to Modify

| File | Action | Description |
|------|--------|-------------|
| `engine/events.go` | **MODIFY** | Add `EventDirectionalCleanerRequest` + payload struct |
| `systems/cleaner.go` | **MODIFY** | Add `spawnDirectionalCleaners()`, vertical collision, lifecycle |
| `render/terminal.go` | **MODIFY** | Fix `drawCleaners()` for vertical trail rendering |
| `modes/input.go` | **MODIFY** | Remap Enter→cleaners, ESC→ping |
| `systems/energy.go` | **MODIFY** | Nugget max heat→directional cleaners |

---

## MODIFICATION PATTERNS

### Event Payload Pattern
```go
// In engine/events.go
type DirectionalCleanerPayload struct {
    OriginX int
    OriginY int
}

// Push event with payload
payload := &DirectionalCleanerPayload{OriginX: cursorX, OriginY: cursorY}
ctx.PushEvent(engine.EventDirectionalCleanerRequest, payload, now)
```

### Directional Cleaner Spawn Pattern
```go
// 4 cleaners: right, left, down, up from origin
// Each locks its row (horizontal) or column (vertical) at spawn
// VelocityX!=0 → horizontal (row locked)
// VelocityY!=0 → vertical (column locked)
```

### Vertical Collision Sweep
```go
// Detect direction from velocity
if c.VelocityY != 0 {
    // Vertical: sweep Y, fixed X
    startY := int(math.Min(prevPreciseY, c.PreciseY))
    endY := int(math.Max(prevPreciseY, c.PreciseY))
    // Clamp and iterate Y
} else {
    // Horizontal: sweep X, fixed Y (existing logic)
}
```

### Trail Rendering (Both Directions)
```go
// Use point.Y instead of cleaner.GridY for screen position
for i, point := range trailCopy {
    if point.X < 0 || point.X >= r.gameWidth || point.Y < 0 || point.Y >= r.gameHeight {
        continue
    }
    screenX := r.gameX + point.X
    screenY := r.gameY + point.Y  // NOT cleaner.GridY
    r.screen.SetContent(screenX, screenY, cleaner.Char, nil, style)
}
```

### Lifecycle Destruction (Both Directions)
```go
shouldDestroy := false
// Horizontal
if c.VelocityX > 0 && c.PreciseX >= c.TargetX { shouldDestroy = true }
if c.VelocityX < 0 && c.PreciseX <= c.TargetX { shouldDestroy = true }
// Vertical
if c.VelocityY > 0 && c.PreciseY >= c.TargetY { shouldDestroy = true }
if c.VelocityY < 0 && c.PreciseY <= c.TargetY { shouldDestroy = true }
```

---

## ARCHITECTURE REFERENCE

### Resource Access Pattern
```go
timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
```

### Event Types (Updated)
```go
EventCleanerRequest            // Horizontal sweep on red rows
EventDirectionalCleanerRequest // 4-way from cursor position
EventCleanerFinished
```

### System Priorities
```
PriorityBoost   = 5
PriorityEnergy  = 10
PrioritySpawn   = 15
PriorityNugget  = 18
PriorityGold    = 20
PriorityCleaner = 22  // Handles both cleaner types
PriorityDrain   = 25
PriorityDecay   = 30
PriorityFlash   = 35
```

### Heat Constants
```go
MaxHeat            = 100  // Full heat bar
NuggetHeatIncrease = 10   // Per nugget
// Enter key costs 10 heat (10%)
```

---

## TESTING CHECKLIST

### Directional Cleaners
- [ ] Nugget collection at max heat spawns 4 cleaners from cursor
- [ ] Cleaners maintain locked row/column as cursor moves
- [ ] Vertical cleaners destroy entities in column
- [ ] Horizontal cleaners destroy entities in row
- [ ] Trail renders correctly for vertical movement
- [ ] All 4 cleaners animate simultaneously
- [ ] Cleaners despawn after clearing screen edge + trail

### Input Changes
- [ ] Enter at heat<10 does nothing
- [ ] Enter at heat≥10 reduces heat by 10, spawns cleaners
- [ ] ESC in Normal mode activates ping grid
- [ ] ESC in Insert/Search/Command unchanged

---

## ENVIRONMENT
```bash
export GOPROXY="https://goproxy.io,direct"
apt-get install -y libasound2-dev
go mod tidy
go build ./...
```