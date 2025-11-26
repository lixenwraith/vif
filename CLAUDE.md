```markdown
# vi-fighter Development Guide for Claude Code

## PROJECT CONTEXT
vi-fighter is a terminal-based typing game in Go using a compile-time Generics-based ECS (Go 1.18+). The architecture combines real-time lock-free updates (atomics) for input/rendering with a discrete clock-tick system for game logic.
**Go Version:** 1.24+

## CURRENT TASK: Materialize System Implementation

### Overview
Introduce a **Materialize System** that provides visual telegraph before drain entity spawns. Four spawner entities converge from screen edges to a target position before the actual drain materializes.

### Design Decisions

| Aspect | Decision |
|--------|----------|
| Animation entities | 4 `MaterializeComponent` entities (top, bottom, left, right) with float physics |
| Trail rendering | Reuse cleaner-style gradient trail with cyan colors |
| Target lock | Spawn location captured at trigger time, stored in spawner component |
| Lifecycle | Spawners destroyed when converging; drain created after all 4 converge |
| Duration | 1 second convergence (matches `CleanerAnimationDuration`) |
| System responsibility | DrainSystem manages materialize lifecycle internally |

### Files to Modify

| File | Action | Description |
|------|--------|-------------|
| `components/materialize.go` | **NEW** | MaterializeComponent struct |
| `constants/materialize.go` | **NEW** | Spawn animation constants |
| `engine/world.go` | **MODIFY** | Add `Materializers` store |
| `systems/drain.go` | **MODIFY** | Refactor spawn to use materialize animation |
| `render/colors.go` | **MODIFY** | Add materializer gradient color |
| `render/terminal_renderer.go` | **MODIFY** | Add gradient, draw method, integrate in RenderFrame |

---

## NEW FILES TO CREATE

### FILE: components/materialize.go

```go
package components

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// MaterializeDirection indicates which screen edge the spawner originates from
type MaterializeDirection int

const (
	MaterializeFromTop MaterializeDirection = iota
	MaterializeFromBottom
	MaterializeFromLeft
	MaterializeFromRight
)

// MaterializeComponent represents a converging spawn animation entity.
// Four spawners (one per edge) converge to a target position before
// the actual drain entity materializes.
type MaterializeComponent struct {
	// Physics state (sub-pixel precision)
	PreciseX float64
	PreciseY float64

	// Movement vector (units per second)
	VelocityX float64
	VelocityY float64

	// Target coordinates where spawner converges
	TargetX int
	TargetY int

	// Trail history for rendering (integer grid coordinates)
	// Index 0 is the current head position
	Trail []core.Point

	// Current grid position (for detecting cell changes)
	GridX int
	GridY int

	// Direction this spawner came from
	Direction MaterializeDirection

	// Character used to render the spawner block
	Char rune

	// Arrived flag - set when spawner reaches target
	Arrived bool
}
```

### FILE: constants/materialize.go

```go
package constants

import "time"

// Materialize System Constants
const (
	// MaterializeAnimationDuration is the time for spawners to converge (1 second)
	MaterializeAnimationDuration = 1 * time.Second

	// MaterializeTrailLength is the number of trail positions for fade effect
	MaterializeTrailLength = 8

	// MaterializeChar is the character used for spawn animation blocks
	MaterializeChar = '█'
)
```

---

## MODIFICATION INSTRUCTIONS

### 1. Modify engine/world.go

**Location:** World struct definition and NewWorld() function

**Changes:**

1. In World struct, add after `Drains` field:
```go
Materializers *Store[components.MaterializeComponent]
```

2. In NewWorld() function, add after Drains initialization:
```go
Materializers: NewStore[components.MaterializeComponent](),
```

3. In allStores slice assignment, add `w.Materializers` after `w.Drains`

---

### 2. Modify render/colors.go

**Location:** After RgbDrain definition

**Add:**
```go
RgbMaterialize = tcell.NewRGBColor(0, 220, 220) // Bright cyan for materialize head
```

---

### 3. Modify render/terminal_renderer.go

**Changes:**

1. Add field to TerminalRenderer struct (after cleanerGradient):
```go
materializeGradient []tcell.Color
```

2. Add gradient build call in NewTerminalRenderer (after `r.buildCleanerGradient()`):
```go
r.buildMaterializeGradient()
```

3. Add new method (pattern identical to buildCleanerGradient):
```go
func (r *TerminalRenderer) buildMaterializeGradient() {
	length := constants.MaterializeTrailLength
	r.materializeGradient = make([]tcell.Color, length)
	red, green, blue := RgbMaterialize.RGB()

	for i := 0; i < length; i++ {
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		rVal := int32(float64(red) * opacity)
		gVal := int32(float64(green) * opacity)
		bVal := int32(float64(blue) * opacity)
		r.materializeGradient[i] = tcell.NewRGBColor(rVal, gVal, bVal)
	}
}
```

4. Add new draw method (pattern identical to drawCleaners):
```go
func (r *TerminalRenderer) drawMaterializers(world *engine.World, defaultStyle tcell.Style) {
	entities := world.Materializers.All()
	if len(entities) == 0 {
		return
	}

	gradientLen := len(r.materializeGradient)
	maxGradientIdx := gradientLen - 1

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		// Deep copy trail to avoid race conditions
		trailCopy := make([]core.Point, len(mat.Trail))
		copy(trailCopy, mat.Trail)

		for i, point := range trailCopy {
			if point.X < 0 || point.X >= r.gameWidth || point.Y < 0 || point.Y >= r.gameHeight {
				continue
			}

			screenX := r.gameX + point.X
			screenY := r.gameY + point.Y

			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			color := r.materializeGradient[gradientIndex]
			style := defaultStyle.Foreground(color)
			r.screen.SetContent(screenX, screenY, mat.Char, nil, style)
		}
	}
}
```

5. In RenderFrame method, add BEFORE `r.drawDrain(ctx.World, defaultStyle)`:
```go
// Draw materialize animation if active - BEFORE drain
r.drawMaterializers(ctx.World, defaultStyle)
```

---

### 4. Modify systems/drain.go

**Changes:**

1. Add fields to DrainSystem struct:
```go
materializeActive  bool
materializeTargetX int
materializeTargetY int
```

2. Modify Update() method - replace current lifecycle check:

**Current:**
```go
if energy > 0 && !drainActive {
    s.spawnDrain(world)
}
```

**New:**
```go
materializersExist := world.Materializers.Count() > 0

if energy > 0 && !drainActive && !s.materializeActive && !materializersExist {
    s.startMaterialize(world)
}

if materializersExist {
    s.updateMaterializers(world, dt)
}
```

3. Add startMaterialize method:
```go
func (s *DrainSystem) startMaterialize(world *engine.World) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	// Lock target position
	s.materializeTargetX = cursorPos.X
	s.materializeTargetY = cursorPos.Y
	s.materializeActive = true

	// Clamp to bounds
	if s.materializeTargetX < 0 {
		s.materializeTargetX = 0
	}
	if s.materializeTargetX >= config.GameWidth {
		s.materializeTargetX = config.GameWidth - 1
	}
	if s.materializeTargetY < 0 {
		s.materializeTargetY = 0
	}
	if s.materializeTargetY >= config.GameHeight {
		s.materializeTargetY = config.GameHeight - 1
	}

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	targetX := float64(s.materializeTargetX)
	targetY := float64(s.materializeTargetY)
	duration := constants.MaterializeAnimationDuration.Seconds()

	type spawnerDef struct {
		startX, startY float64
		dir            components.MaterializeDirection
	}

	spawners := []spawnerDef{
		{targetX, -1, components.MaterializeFromTop},
		{targetX, gameHeight, components.MaterializeFromBottom},
		{-1, targetY, components.MaterializeFromLeft},
		{gameWidth, targetY, components.MaterializeFromRight},
	}

	for _, def := range spawners {
		velX := (targetX - def.startX) / duration
		velY := (targetY - def.startY) / duration

		comp := components.MaterializeComponent{
			PreciseX:  def.startX,
			PreciseY:  def.startY,
			VelocityX: velX,
			VelocityY: velY,
			TargetX:   s.materializeTargetX,
			TargetY:   s.materializeTargetY,
			GridX:     int(def.startX),
			GridY:     int(def.startY),
			Trail:     []core.Point{{X: int(def.startX), Y: int(def.startY)}},
			Direction: def.dir,
			Char:      constants.MaterializeChar,
			Arrived:   false,
		}

		entity := world.CreateEntity()
		world.Materializers.Add(entity, comp)
	}
}
```

4. Add updateMaterializers method:
```go
func (s *DrainSystem) updateMaterializers(world *engine.World, dt time.Duration) {
	dtSeconds := dt.Seconds()
	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	entities := world.Materializers.All()
	allArrived := true

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		if mat.Arrived {
			continue
		}

		mat.PreciseX += mat.VelocityX * dtSeconds
		mat.PreciseY += mat.VelocityY * dtSeconds

		arrived := false
		switch mat.Direction {
		case components.MaterializeFromTop:
			arrived = mat.PreciseY >= float64(mat.TargetY)
		case components.MaterializeFromBottom:
			arrived = mat.PreciseY <= float64(mat.TargetY)
		case components.MaterializeFromLeft:
			arrived = mat.PreciseX >= float64(mat.TargetX)
		case components.MaterializeFromRight:
			arrived = mat.PreciseX <= float64(mat.TargetX)
		}

		if arrived {
			mat.PreciseX = float64(mat.TargetX)
			mat.PreciseY = float64(mat.TargetY)
			mat.Arrived = true
		} else {
			allArrived = false
		}

		newGridX := int(mat.PreciseX)
		newGridY := int(mat.PreciseY)

		if newGridX != mat.GridX || newGridY != mat.GridY {
			mat.GridX = newGridX
			mat.GridY = newGridY

			newPoint := core.Point{X: mat.GridX, Y: mat.GridY}
			oldLen := len(mat.Trail)
			newLen := oldLen + 1
			if newLen > constants.MaterializeTrailLength {
				newLen = constants.MaterializeTrailLength
			}

			newTrail := make([]core.Point, newLen)
			newTrail[0] = newPoint
			if newLen > 1 {
				copy(newTrail[1:], mat.Trail[:newLen-1])
			}
			mat.Trail = newTrail
		}

		world.Materializers.Add(entity, mat)
	}

	if allArrived && len(entities) > 0 {
		for _, entity := range entities {
			world.DestroyEntity(entity)
		}
		s.materializeActive = false
		s.materializeDrain(world)
	}
}
```

5. Add materializeDrain method (adapted from current spawnDrain):
```go
func (s *DrainSystem) materializeDrain(world *engine.World) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	spawnX := s.materializeTargetX
	spawnY := s.materializeTargetY

	if spawnX < 0 {
		spawnX = 0
	}
	if spawnX >= config.GameWidth {
		spawnX = config.GameWidth - 1
	}
	if spawnY < 0 {
		spawnY = 0
	}
	if spawnY >= config.GameHeight {
		spawnY = config.GameHeight - 1
	}

	entity := world.CreateEntity()

	pos := components.PositionComponent{
		X: spawnX,
		Y: spawnY,
	}

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	drain := components.DrainComponent{
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    spawnX == cursorPos.X && spawnY == cursorPos.Y,
	}

	// Handle collisions at spawn position
	entitiesAtSpawn := world.Positions.GetAllAt(spawnX, spawnY)
	var toProcess []engine.Entity
	for _, e := range entitiesAtSpawn {
		if e != s.ctx.CursorEntity {
			toProcess = append(toProcess, e)
		}
	}
	for _, e := range toProcess {
		s.handleCollisionAtPosition(world, e)
	}

	world.Positions.Add(entity, pos)
	world.Drains.Add(entity, drain)
}
```

6. Remove or rename the original `spawnDrain()` method as it's replaced by `startMaterialize()` + `materializeDrain()`

7. Add import for `"github.com/lixenwraith/vi-fighter/core"` if not present

---

## VERIFICATION STEPS

After implementation:

1. **Compile check**: `go build ./...`
2. **Visual test**: 
   - Start game with 0 energy
   - Type characters to gain energy
   - Observe 4 cyan trails converging from screen edges
   - Drain should appear at convergence point after ~1 second
3. **Edge cases**:
   - Cursor at screen edge
   - Cursor at center
   - Energy drops to 0 during animation

---

## ARCHITECTURE REFERENCE

### Resource Access Pattern
```go
func (s *MySystem) Update(world *engine.World, dt time.Duration) {
    config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
    timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
    // Use config.GameWidth, timeRes.GameTime, etc.
}
```

### Trail Update Pattern (Copy-on-Write)
```go
newPoint := core.Point{X: gridX, Y: gridY}
oldLen := len(comp.Trail)
newLen := oldLen + 1
if newLen > constants.TrailLength {
    newLen = constants.TrailLength
}
newTrail := make([]core.Point, newLen)
newTrail[0] = newPoint
if newLen > 1 {
    copy(newTrail[1:], comp.Trail[:newLen-1])
}
comp.Trail = newTrail
```

---

## TESTING & TROUBLESHOOTING

### 1. Environment Setup (CRITICAL - PROVEN WORKING METHOD)
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
   GOPROXY="https://goproxy.io,direct" go test -race ./engine/... -v
   ```

### 2. Running Tests
Always run with the race detector enabled.
```bash
export GOPROXY="https://goproxy.io,direct"
go test -race ./...
```

## FILE STRUCTURE

```
vi-fighter/
├── components/
│   ├── materialize.go    # NEW
│   ├── cleaner.go        # Reference for physics pattern
│   └── drain.go
├── constants/
│   ├── materialize.go    # NEW
│   └── cleaners.go       # Reference for trail constants
├── engine/
│   └── world.go          # MODIFY: add Materializers store
├── render/
│   ├── colors.go         # MODIFY: add RgbMaterialize
│   └── terminal_renderer.go  # MODIFY: add gradient + draw method
├── systems/
│   └── drain.go          # MODIFY: add materialize lifecycle
└── core/
    └── point.go          # Point struct used in Trail
```
```
