package systems

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	ctx               *engine.GameContext
	spawned           map[int64]bool // Track which frames already spawned cleaners
	hasSpawnedSession bool           // Track if we spawned cleaners this session
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(ctx *engine.GameContext) *CleanerSystem {
	return &CleanerSystem{
		ctx:     ctx,
		spawned: make(map[int64]bool),
	}
}

// Priority returns the system's priority
func (cs *CleanerSystem) Priority() int {
	return constants.PriorityCleaner
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// 1. Handle Event Queue - Consume cleaner request events
	events := cs.ctx.ConsumeEvents()
	for _, event := range events {
		if event.Type == engine.EventCleanerRequest {
			// Check if we already spawned for this frame
			if !cs.spawned[event.Frame] {
				cs.spawnCleaners(world)
				cs.spawned[event.Frame] = true
				cs.hasSpawnedSession = true
			}
		}
	}

	// 2. Clean old entries from spawned map (keep last CleanerDeduplicationWindow frames)
	currentFrame := cs.ctx.State.GetFrameNumber()
	for frame := range cs.spawned {
		if currentFrame-frame > constants.CleanerDeduplicationWindow {
			delete(cs.spawned, frame)
		}
	}

	// 3. Process Active Cleaners
	entities := world.Cleaners.All()

	// If no cleaners exist but we spawned this session, emit finished event
	if len(entities) == 0 && cs.hasSpawnedSession {
		cs.ctx.PushEvent(engine.EventCleanerFinished, nil, now)
		cs.hasSpawnedSession = false
		cs.cleanupExpiredFlashes(world)
		return
	}

	// If no cleaners, we can skip processing
	if len(entities) == 0 {
		cs.cleanupExpiredFlashes(world)
		return
	}

	dtSec := dt.Seconds()
	gameWidth := config.GameWidth

	for _, entity := range entities {
		c, ok := world.Cleaners.Get(entity)
		if !ok {
			continue
		}

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

			// Push new position to the front of the trail using strict copy-on-write
			// to prevent race conditions with the Renderer reading the trail
			newPoint := core.Point{X: c.GridX, Y: c.GridY}

			// Calculate new trail length (old trail + new point, capped at max)
			oldLen := len(c.Trail)
			newLen := oldLen + 1
			if newLen > constants.CleanerTrailLength {
				newLen = constants.CleanerTrailLength
			}

			// Allocate a new slice - this ensures Renderer can't access the same backing array
			newTrail := make([]core.Point, newLen)

			// Copy new point to front
			newTrail[0] = newPoint

			// Copy old trail points (up to max length - 1)
			copyLen := newLen - 1
			if copyLen > 0 {
				copy(newTrail[1:], c.Trail[:copyLen])
			}

			// Assign new slice to component (atomically replaces reference)
			c.Trail = newTrail
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
			world.Cleaners.Add(entity, c)
		}
	}

	// Check again after processing to see if all cleaners finished this frame
	entities = world.Cleaners.All()
	if len(entities) == 0 && cs.hasSpawnedSession {
		cs.ctx.PushEvent(engine.EventCleanerFinished, nil, now)
		cs.hasSpawnedSession = false
	}

	// 4. Cleanup Effects
	cs.cleanupExpiredFlashes(world)
}

// spawnCleaners generates cleaner entities using generic stores
func (cs *CleanerSystem) spawnCleaners(world *engine.World) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	// No-op if there are no red entities
	redRows := cs.scanRedCharacterRows(world)
	if len(redRows) == 0 {
		return
	}

	// Fetch TimeResource for audio timestamp
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Play sound ONLY if spawning actual cleaners
	if cs.ctx.AudioEngine != nil {
		cs.ctx.AudioEngine.SendRealTime(audio.AudioCommand{
			Type:       audio.SoundWhoosh,
			Priority:   1,
			Generation: uint64(cs.ctx.State.GetFrameNumber()),
			Timestamp:  timeRes.GameTime,
		})
	}

	gameWidth := float64(config.GameWidth)
	duration := constants.CleanerAnimationDuration.Seconds()
	// Calculate base speed to traverse width in duration
	baseSpeed := gameWidth / duration

	trailLen := float64(constants.CleanerTrailLength)

	for _, row := range redRows {
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

		// Use entity builder pattern
		entity := world.NewEntity().Build()
		world.Cleaners.Add(entity, comp)
	}
}

// scanRedCharacterRows finds all rows containing Red sequences using query builder
func (cs *CleanerSystem) scanRedCharacterRows(world *engine.World) []int {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	redRows := make(map[int]bool)
	gameHeight := config.GameHeight

	// Query entities with both Sequences and Positions
	entities := world.Query().
		With(world.Sequences).
		With(world.Positions).
		Execute()

	for _, entity := range entities {
		seq, hasSeq := world.Sequences.Get(entity)
		if !hasSeq {
			continue
		}

		if seq.Type != components.SequenceRed {
			continue
		}

		// Retrieve Position
		pos, hasPos := world.Positions.Get(entity)
		if !hasPos {
			continue
		}

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

// checkAndDestroyAtPosition handles collision logic using spatial index
func (cs *CleanerSystem) checkAndDestroyAtPosition(world *engine.World, x, y int) {
	targetEntity := world.Positions.GetEntityAt(x, y)
	if targetEntity == 0 {
		return
	}

	// Verify it's a Red character
	if seqComp, ok := world.Sequences.Get(targetEntity); ok {
		if seqComp.Type == components.SequenceRed {
			// Spawn flash effect
			cs.spawnRemovalFlash(world, targetEntity)
			// Destroy target
			world.DestroyEntity(targetEntity)
		}
	}
}

// spawnRemovalFlash creates a transient visual effect using generic stores
func (cs *CleanerSystem) spawnRemovalFlash(world *engine.World, targetEntity engine.Entity) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	if charComp, ok := world.Characters.Get(targetEntity); ok {
		if posComp, ok := world.Positions.Get(targetEntity); ok {
			flash := components.FlashComponent{
				X:         posComp.X,
				Y:         posComp.Y,
				Char:      charComp.Rune,
				StartTime: now,
				Duration:  constants.CleanerRemovalFlashDuration,
			}

			flashEntity := world.NewEntity().Build()
			world.Flashes.Add(flashEntity, flash)
		}
	}
}

// cleanupExpiredFlashes destroys expired removal flash entities using generic stores
func (cs *CleanerSystem) cleanupExpiredFlashes(world *engine.World) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Get and iterate on all flashes
	entities := world.Flashes.All()
	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		// Destroy flash if time expired
		if now.Sub(flash.StartTime).Milliseconds() >= int64(flash.Duration) {
			world.DestroyEntity(entity)
		}
	}
}