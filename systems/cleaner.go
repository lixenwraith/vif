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

// EventTypes returns the event types CleanerSystem handles
func (cs *CleanerSystem) EventTypes() []engine.EventType {
	return []engine.EventType{
		engine.EventCleanerRequest,
		engine.EventDirectionalCleanerRequest,
	}
}

// HandleEvent processes cleaner-related events from the router
func (cs *CleanerSystem) HandleEvent(world *engine.World, event engine.GameEvent) {
	// Check if we already spawned for this frame (deduplication)
	if cs.spawned[event.Frame] {
		return
	}

	switch event.Type {
	case engine.EventCleanerRequest:
		cs.spawnCleaners(world)
		cs.spawned[event.Frame] = true
		cs.hasSpawnedSession = true

	case engine.EventDirectionalCleanerRequest:
		if payload, ok := event.Payload.(*engine.DirectionalCleanerPayload); ok {
			cs.spawnDirectionalCleaners(world, payload.OriginX, payload.OriginY)
			cs.spawned[event.Frame] = true
			cs.hasSpawnedSession = true
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// 1. Clean old entries from spawned map (keep last CleanerDeduplicationWindow frames)
	currentFrame := cs.ctx.State.GetFrameNumber()
	for frame := range cs.spawned {
		if currentFrame-frame > constants.CleanerDeduplicationWindow {
			delete(cs.spawned, frame)
		}
	}

	// 2. Process Active Cleaners
	entities := world.Cleaners.All()

	// If no cleaners exist but we spawned this session, emit finished event
	if len(entities) == 0 && cs.hasSpawnedSession {
		cs.ctx.PushEvent(engine.EventCleanerFinished, nil, now)
		cs.hasSpawnedSession = false
		return
	}

	// If no cleaners, we can skip processing
	if len(entities) == 0 {
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
		prevPreciseY := c.PreciseY
		c.PreciseX += c.VelocityX * dtSec
		c.PreciseY += c.VelocityY * dtSec

		// --- Collision Detection (Swept Segment) ---
		// Check all integer grid points covered during this frame's movement to prevent tunneling at high speeds
		// Vertical cleaners sweep Y positions, horizontal cleaners sweep X positions
		if c.VelocityY != 0 && c.VelocityX == 0 {
			// Vertical cleaner - sweep Y positions at fixed X
			startY := int(math.Min(prevPreciseY, c.PreciseY))
			endY := int(math.Max(prevPreciseY, c.PreciseY))

			// Clamp check range to screen bounds
			gameHeight := config.GameHeight
			checkStart := startY
			if checkStart < 0 {
				checkStart = 0
			}
			checkEnd := endY
			if checkEnd >= gameHeight {
				checkEnd = gameHeight - 1
			}

			if checkStart <= checkEnd {
				for y := checkStart; y <= checkEnd; y++ {
					cs.checkAndDestroyAtPosition(world, c.GridX, y)
				}
			}
		} else if c.VelocityX != 0 {
			// Horizontal cleaner - sweep X positions at fixed Y
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

			// Assign new slice to component
			c.Trail = newTrail
		}

		// --- Lifecycle Management ---
		// Destroy entity when it passes its target (clearing the screen + trail)
		shouldDestroy := false
		if c.VelocityX > 0 && c.PreciseX >= c.TargetX {
			shouldDestroy = true
		} else if c.VelocityX < 0 && c.PreciseX <= c.TargetX {
			shouldDestroy = true
		} else if c.VelocityY > 0 && c.PreciseY >= c.TargetY {
			shouldDestroy = true
		} else if c.VelocityY < 0 && c.PreciseY <= c.TargetY {
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

		// Create cleaner entity and add to store
		entity := world.CreateEntity()
		world.Cleaners.Add(entity, comp)
	}
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position (up/down/left/right)
func (cs *CleanerSystem) spawnDirectionalCleaners(world *engine.World, originX, originY int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Play whoosh sound once for all 4 cleaners
	if cs.ctx.AudioEngine != nil {
		cs.ctx.AudioEngine.SendRealTime(audio.AudioCommand{
			Type:       audio.SoundWhoosh,
			Priority:   1,
			Generation: uint64(cs.ctx.State.GetFrameNumber()),
			Timestamp:  timeRes.GameTime,
		})
	}

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	trailLen := float64(constants.CleanerTrailLength)

	// Calculate speeds based on animation duration
	horizontalSpeed := gameWidth / constants.CleanerAnimationDuration.Seconds()
	verticalSpeed := gameHeight / constants.CleanerAnimationDuration.Seconds()

	// Origin as floats
	ox := float64(originX)
	oy := float64(originY)

	// Spawn 4 cleaners: right, left, down, up
	directions := []struct {
		velocityX, velocityY float64
		startX, startY       float64
		targetX, targetY     float64
		gridX, gridY         int
	}{
		// Right: horizontal cleaner moving right
		{
			velocityX: horizontalSpeed,
			velocityY: 0,
			startX:    ox,
			startY:    oy,
			targetX:   gameWidth + trailLen,
			targetY:   oy,
			gridX:     originX,
			gridY:     originY,
		},
		// Left: horizontal cleaner moving left
		{
			velocityX: -horizontalSpeed,
			velocityY: 0,
			startX:    ox,
			startY:    oy,
			targetX:   -trailLen,
			targetY:   oy,
			gridX:     originX,
			gridY:     originY,
		},
		// Down: vertical cleaner moving down
		{
			velocityX: 0,
			velocityY: verticalSpeed,
			startX:    ox,
			startY:    oy,
			targetX:   ox,
			targetY:   gameHeight + trailLen,
			gridX:     originX,
			gridY:     originY,
		},
		// Up: vertical cleaner moving up
		{
			velocityX: 0,
			velocityY: -verticalSpeed,
			startX:    ox,
			startY:    oy,
			targetX:   ox,
			targetY:   -trailLen,
			gridX:     originX,
			gridY:     originY,
		},
	}

	for _, dir := range directions {
		trail := []core.Point{{X: dir.gridX, Y: dir.gridY}}

		comp := components.CleanerComponent{
			PreciseX:  dir.startX,
			PreciseY:  dir.startY,
			VelocityX: dir.velocityX,
			VelocityY: dir.velocityY,
			TargetX:   dir.targetX,
			TargetY:   dir.targetY,
			GridX:     dir.gridX,
			GridY:     dir.gridY,
			Trail:     trail,
			Char:      constants.CleanerChar,
		}

		entity := world.CreateEntity()
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
	// Get all entities at this position
	targetEntities := world.Positions.GetAllAt(x, y)

	// Create a copy or iterate carefully since we might destroy entities
	// Destroying modifies the grid, so standard range loop on the slice
	// returned by GetAllAt (which is a view of backing array) is unsafe if the backing array shifts
	// However, PositionStore.Remove modifies the array in place
	// Safer to collect candidates first
	var toDestroy []engine.Entity

	for _, e := range targetEntities {
		if e == 0 {
			continue
		}
		// Verify it's a Red character
		if seqComp, ok := world.Sequences.Get(e); ok {
			if seqComp.Type == components.SequenceRed {
				toDestroy = append(toDestroy, e)
			}
		}
	}

	// Spawn flash effect and destroy all marked to destroy
	for _, e := range toDestroy {
		cs.spawnRemovalFlash(world, e)
		world.DestroyEntity(e)
	}
}

// spawnRemovalFlash creates a transient visual effect using generic stores
func (cs *CleanerSystem) spawnRemovalFlash(world *engine.World, targetEntity engine.Entity) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	if charComp, ok := world.Characters.Get(targetEntity); ok {
		if posComp, ok := world.Positions.Get(targetEntity); ok {
			SpawnDestructionFlash(world, posComp.X, posComp.Y, charComp.Rune, now)
		}
	}
}