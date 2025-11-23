package systems

import (
	"math"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	ctx          *engine.GameContext
	pendingSpawn atomic.Bool // Simple flag to trigger spawning in the main Update loop
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(ctx *engine.GameContext) *CleanerSystem {
	return &CleanerSystem{
		ctx: ctx,
	}
}

// Priority returns the system's priority (runs after decay system)
func (cs *CleanerSystem) Priority() int {
	return 22
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	// 1. Handle Pending Spawns
	if cs.pendingSpawn.CompareAndSwap(true, false) {
		cs.spawnCleaners(world)
	}

	// 2. Process Active Cleaners
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

	// If no cleaners and no pending spawn, we can skip processing
	if len(entities) == 0 {
		cs.cleanupExpiredFlashes(world)
		return
	}

	dtSec := dt.Seconds()
	gameWidth := cs.ctx.GameWidth

	for _, entity := range entities {
		compRaw, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			continue
		}
		c := compRaw.(components.CleanerComponent)

		// --- Physics Update ---
		prevPreciseX := c.PreciseX
		c.PreciseX += c.VelocityX * dtSec
		c.PreciseY += c.VelocityY * dtSec

		// --- Collision Detection (Swept Segment) ---
		// Check all integer grid points covered during this frame's movement
		// to prevent tunneling at high speeds.
		startX := int(math.Min(prevPreciseX, c.PreciseX))
		endX := int(math.Max(prevPreciseX, c.PreciseX))

		// Clamp check range to screen bounds
		checkStart := startX
		if checkStart < 0 {
			checkStart = 0
		}
		checkEnd := endX
		if checkEnd >= gameWidth {
			checkEnd = gameWidth - 1
		}

		if checkStart <= checkEnd {
			for x := checkStart; x <= checkEnd; x++ {
				cs.checkAndDestroyAtPosition(world, x, c.GridY)
			}
		}

		// --- Trail Update ---
		newGridX := int(c.PreciseX)
		newGridY := int(c.PreciseY)

		// Update trail only if we moved to a new integer grid cell
		if newGridX != c.GridX || newGridY != c.GridY {
			c.GridX = newGridX
			c.GridY = newGridY

			// Push new position to the front of the trail
			newPoint := core.Point{X: c.GridX, Y: c.GridY}
			// Prepend
			c.Trail = append([]core.Point{newPoint}, c.Trail...)

			// Truncate to max length
			if len(c.Trail) > constants.CleanerTrailLength {
				c.Trail = c.Trail[:constants.CleanerTrailLength]
			}
		}

		// --- Lifecycle Management ---
		// Destroy entity only when it passes the TargetX (clearing the screen)
		shouldDestroy := false
		if c.VelocityX > 0 && c.PreciseX >= c.TargetX {
			shouldDestroy = true
		} else if c.VelocityX < 0 && c.PreciseX <= c.TargetX {
			shouldDestroy = true
		}

		if shouldDestroy {
			world.DestroyEntity(entity)
		} else {
			// Save updated component state
			world.AddComponent(entity, c)
		}
	}

	// 3. Cleanup Effects
	cs.cleanupExpiredFlashes(world)
}

// ActivateCleaners flags the system to spawn cleaners on the next update
func (cs *CleanerSystem) ActivateCleaners(world *engine.World) {
	cs.pendingSpawn.Store(true)
}

// IsAnimationComplete checks if the animation is finished
func (cs *CleanerSystem) IsAnimationComplete() bool {
	// If spawn is pending, we are definitely not done
	if cs.pendingSpawn.Load() {
		return false
	}

	// Check if any active cleaner entities exist in the world
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := cs.ctx.World.GetEntitiesWith(cleanerType)
	return len(entities) == 0
}

// spawnCleaners generates cleaner entities for rows with Red characters
func (cs *CleanerSystem) spawnCleaners(world *engine.World) {
	redRows := cs.scanRedCharacterRows(world)
	if len(redRows) == 0 {
		return
	}

	// Play sound ONLY if spawning actual cleaners
	if cs.ctx.AudioEngine != nil {
		cs.ctx.AudioEngine.SendRealTime(audio.AudioCommand{
			Type:       audio.SoundWhoosh,
			Priority:   1,
			Generation: uint64(cs.ctx.State.GetFrameNumber()),
			Timestamp:  cs.ctx.TimeProvider.Now(),
		})
	}

	gameWidth := float64(cs.ctx.GameWidth)
	duration := constants.CleanerAnimationDuration.Seconds()
	// Calculate base speed to traverse width in duration
	baseSpeed := gameWidth / duration

	trailLen := float64(constants.CleanerTrailLength)

	for _, row := range redRows {
		entity := world.CreateEntity()

		var startX, targetX, velX float64

		if row%2 != 0 {
			// Odd Rows: Left -> Right
			// Start off-screen left, End off-screen right
			startX = -trailLen
			targetX = gameWidth + trailLen
			velX = baseSpeed
		} else {
			// Even Rows: Right -> Left
			// Start off-screen right, End off-screen left
			startX = gameWidth + trailLen
			targetX = -trailLen
			velX = -baseSpeed
		}

		// Initial trail point
		startGridX := int(startX)
		trail := []core.Point{{X: startGridX, Y: row}}

		comp := components.CleanerComponent{
			PreciseX:  startX,
			PreciseY:  float64(row),
			VelocityX: velX,
			VelocityY: 0, // Horizontal only
			TargetX:   targetX,
			TargetY:   float64(row),
			GridX:     startGridX,
			GridY:     row,
			Trail:     trail,
			Char:      constants.CleanerChar,
		}

		world.AddComponent(entity, comp)
	}
}

// GetCleanerSnapshots returns a thread-safe snapshot of active cleaners for rendering
func (cs *CleanerSystem) GetCleanerSnapshots() []render.CleanerSnapshot {
	world := cs.ctx.World
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)

	snapshots := make([]render.CleanerSnapshot, 0, len(entities))

	for _, entity := range entities {
		if compRaw, ok := world.GetComponent(entity, cleanerType); ok {
			c := compRaw.(components.CleanerComponent)
			// Deep copy trail to avoid race conditions during rendering
			trailCopy := make([]core.Point, len(c.Trail))
			copy(trailCopy, c.Trail)

			snapshots = append(snapshots, render.CleanerSnapshot{
				Row:   c.GridY,
				Trail: trailCopy,
				Char:  c.Char,
			})
		}
	}
	return snapshots
}

// scanRedCharacterRows finds all rows containing Red sequences
func (cs *CleanerSystem) scanRedCharacterRows(world *engine.World) []int {
	redRows := make(map[int]bool)
	gameHeight := cs.ctx.GameHeight

	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		seqRaw, hasSeq := world.GetComponent(entity, seqType)
		if !hasSeq {
			continue // Entity might have been destroyed concurrently
		}

		// Type assertion
		seq := seqRaw.(components.SequenceComponent)
		if seq.Type != components.SequenceRed {
			continue
		}

		// Retrieve Position
		posRaw, hasPos := world.GetComponent(entity, posType)
		if !hasPos {
			continue
		}

		pos := posRaw.(components.PositionComponent)

		// Add row if in bounds
		if pos.Y >= 0 && pos.Y < gameHeight {
			redRows[pos.Y] = true
		}
	}

	rows := make([]int, 0, len(redRows))
	for row := range redRows {
		rows = append(rows, row)
	}
	return rows
}

// checkAndDestroyAtPosition handles collision logic and flash spawning
func (cs *CleanerSystem) checkAndDestroyAtPosition(world *engine.World, x, y int) {
	targetEntity := world.GetEntityAtPosition(x, y)
	if targetEntity == 0 {
		return
	}

	// Verify it's a Red character
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if seqComp, ok := world.GetComponent(targetEntity, seqType); ok {
		if seqComp.(components.SequenceComponent).Type == components.SequenceRed {
			// Spawn flash effect
			cs.spawnRemovalFlash(world, targetEntity)
			// Destroy target
			world.DestroyEntity(targetEntity)
		}
	}
}

// spawnRemovalFlash creates a transient visual effect at the position of the destroyed entity
func (cs *CleanerSystem) spawnRemovalFlash(world *engine.World, targetEntity engine.Entity) {
	charType := reflect.TypeOf(components.CharacterComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	if charComp, ok := world.GetComponent(targetEntity, charType); ok {
		if posComp, ok := world.GetComponent(targetEntity, posType); ok {
			char := charComp.(components.CharacterComponent)
			pos := posComp.(components.PositionComponent)

			flashEntity := world.CreateEntity()
			flash := components.RemovalFlashComponent{
				X:         pos.X,
				Y:         pos.Y,
				Char:      char.Rune,
				StartTime: cs.ctx.TimeProvider.Now(),
				Duration:  constants.CleanerRemovalFlashDuration,
			}
			world.AddComponent(flashEntity, flash)
		}
	}
}

// cleanupExpiredFlashes destroys removal flash entities that have exceeded their duration
func (cs *CleanerSystem) cleanupExpiredFlashes(world *engine.World) {
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	entities := world.GetEntitiesWith(flashType)
	now := cs.ctx.TimeProvider.Now()

	for _, entity := range entities {
		flashRaw, ok := world.GetComponent(entity, flashType)
		if !ok {
			continue
		}

		flash := flashRaw.(components.RemovalFlashComponent)

		if now.Sub(flash.StartTime).Milliseconds() >= int64(flash.Duration) {
			world.DestroyEntity(entity)
		}
	}
}