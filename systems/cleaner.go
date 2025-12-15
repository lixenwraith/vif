package systems

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	world *engine.World
	res   engine.CoreResources

	spawned           map[int64]bool // Track which frames already spawned cleaners
	hasSpawnedSession bool           // Track if we spawned cleaners this session
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(world *engine.World) *CleanerSystem {
	return &CleanerSystem{
		world:   world,
		res:     engine.GetCoreResources(world),
		spawned: make(map[int64]bool),
	}
}

// Priority returns the system's priority
func (cs *CleanerSystem) Priority() int {
	return constants.PriorityCleaner
}

// EventTypes returns the event types CleanerSystem handles
func (cs *CleanerSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventCleanerRequest,
		events.EventDirectionalCleanerRequest,
	}
}

// HandleEvent processes cleaner-related events from the router
func (cs *CleanerSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	// Check if we already spawned for this frame (deduplication)
	if cs.spawned[event.Frame] {
		return
	}

	switch event.Type {
	case events.EventCleanerRequest:
		cs.spawnCleaners(world)
		cs.spawned[event.Frame] = true
		cs.hasSpawnedSession = true

	case events.EventDirectionalCleanerRequest:
		if payload, ok := event.Payload.(*events.DirectionalCleanerPayload); ok {
			cs.spawnDirectionalCleaners(world, payload.OriginX, payload.OriginY)
			cs.spawned[event.Frame] = true
			cs.hasSpawnedSession = true
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (cs *CleanerSystem) Update(world *engine.World, dt time.Duration) {
	config := cs.res.Config

	// Clean old entries from spawned map
	// TODO: change this
	currentFrame := cs.res.State.State.GetFrameNumber()
	for frame := range cs.spawned {
		if currentFrame-frame > constants.CleanerDeduplicationWindow {
			delete(cs.spawned, frame)
		}
	}

	entities := world.Cleaners.All()

	// Push EventCleanerFinished when all cleaners have completed their animation
	if len(entities) == 0 && cs.hasSpawnedSession {
		world.PushEvent(events.EventCleanerFinished, nil)
		cs.hasSpawnedSession = false
		return
	}

	if len(entities) == 0 {
		return
	}

	dtSec := dt.Seconds()
	gameWidth := config.GameWidth
	gameHeight := config.GameHeight

	for _, entity := range entities {
		c, ok := world.Cleaners.Get(entity)
		if !ok {
			continue
		}

		// Read grid position from PositionStore (authoritative for spatial queries)
		oldPos, hasPos := world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Physics Update: Integrate velocity into float position (overlay state)
		prevPreciseX := c.PreciseX
		prevPreciseY := c.PreciseY
		c.PreciseX += c.VelocityX * dtSec
		c.PreciseY += c.VelocityY * dtSec

		// Swept Collision Detection: Check all cells between previous and current position
		if c.VelocityY != 0 && c.VelocityX == 0 {
			// Vertical cleaner: sweep Y axis
			startY := int(math.Min(prevPreciseY, c.PreciseY))
			endY := int(math.Max(prevPreciseY, c.PreciseY))

			checkStart := startY
			if checkStart < 0 {
				checkStart = 0
			}
			checkEnd := endY
			if checkEnd >= gameHeight {
				checkEnd = gameHeight - 1
			}

			// Check all traversed rows for collisions (with self-exclusion)
			if checkStart <= checkEnd {
				for y := checkStart; y <= checkEnd; y++ {
					cs.checkAndDestroyAtPositionExcluding(world, oldPos.X, y, entity)
				}
			}
		} else if c.VelocityX != 0 {
			// Horizontal cleaner: sweep X axis
			startX := int(math.Min(prevPreciseX, c.PreciseX))
			endX := int(math.Max(prevPreciseX, c.PreciseX))

			checkStart := startX
			if checkStart < 0 {
				checkStart = 0
			}
			checkEnd := endX
			if checkEnd >= gameWidth {
				checkEnd = gameWidth - 1
			}

			// Check all traversed columns for collisions (with self-exclusion)
			if checkStart <= checkEnd {
				for x := checkStart; x <= checkEnd; x++ {
					cs.checkAndDestroyAtPositionExcluding(world, x, oldPos.Y, entity)
				}
			}
		}

		// Trail Update & Grid Sync: Update trail ring buffer and sync PositionStore if cell changed
		newGridX := int(c.PreciseX)
		newGridY := int(c.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			// Update trail: add new grid position to ring buffer
			c.TrailHead = (c.TrailHead + 1) % constants.CleanerTrailLength
			c.TrailRing[c.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if c.TrailLen < constants.CleanerTrailLength {
				c.TrailLen++
			}

			// Sync grid position to PositionStore
			world.Positions.Add(entity, components.PositionComponent{X: newGridX, Y: newGridY})
		}

		// Lifecycle Check: Destroy cleaner when it reaches target position
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
			world.Cleaners.Add(entity, c)
		}
	}

	entities = world.Cleaners.All()
	// Push EventCleanerFinished when all cleaners have completed their animation
	if len(entities) == 0 && cs.hasSpawnedSession {
		world.PushEvent(events.EventCleanerFinished, nil)
		cs.hasSpawnedSession = false
	}
}

// spawnCleaners generates cleaner entities using generic stores
func (cs *CleanerSystem) spawnCleaners(world *engine.World) {
	config := cs.res.Config

	redRows := cs.scanRedCharacterRows(world)

	// TODO: new boost trigger
	// Grayout: no targets to clean
	if len(redRows) == 0 {
		cs.res.State.State.TriggerGrayout(cs.res.Time.GameTime)
		world.PushEvent(events.EventCleanerFinished, nil)
		return
	}

	world.PushEvent(events.EventSoundRequest, &events.SoundRequestPayload{
		SoundType: audio.SoundWhoosh,
	})

	gameWidth := float64(config.GameWidth)
	duration := constants.CleanerAnimationDuration.Seconds()
	baseSpeed := gameWidth / duration
	trailLen := float64(constants.CleanerTrailLength)

	// Spawn one cleaner per row with Red entities, alternating L→R and R→L direction
	for _, row := range redRows {
		var startX, targetX, velX float64

		if row%2 != 0 {
			startX = -trailLen
			targetX = gameWidth + trailLen
			velX = baseSpeed
		} else {
			startX = gameWidth + trailLen
			targetX = -trailLen
			velX = -baseSpeed
		}

		startGridX := int(startX)
		startGridY := row

		// Initialize trail ring buffer with starting position
		var trailRing [constants.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := components.CleanerComponent{
			PreciseX:  startX,
			PreciseY:  float64(row),
			VelocityX: velX,
			VelocityY: 0,
			TargetX:   targetX,
			TargetY:   float64(row),
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Char:      constants.CleanerChar,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: startGridX, Y: startGridY})
		world.Cleaners.Add(entity, comp)
		world.Protections.Add(entity, components.ProtectionComponent{
			Mask: components.ProtectFromDrain | components.ProtectFromCull,
		})
	}
}

// checkAndDestroyAtPositionExcluding handles collision logic with self-exclusion
func (cs *CleanerSystem) checkAndDestroyAtPositionExcluding(world *engine.World, x, y int, selfEntity core.Entity) {
	// Query all entities at position (includes cleaner itself due to PositionStore registration)
	targetEntities := world.Positions.GetAllAt(x, y)

	var toDestroy []core.Entity

	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue // Self-exclusion: skip cleaner entity to prevent self-destruction
		}
		// Only destroy Red sequence entities
		if seqComp, ok := world.Sequences.Get(e); ok {
			if seqComp.Type == components.SequenceRed {
				toDestroy = append(toDestroy, e)
			}
		}
	}

	// Spawn flash effects and destroy marked entities
	for _, e := range toDestroy {
		cs.spawnRemovalFlash(world, e)
		world.DestroyEntity(e)
	}
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position
func (cs *CleanerSystem) spawnDirectionalCleaners(world *engine.World, originX, originY int) {
	config := cs.res.Config

	world.PushEvent(events.EventSoundRequest, &events.SoundRequestPayload{
		SoundType: audio.SoundWhoosh,
	})

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	trailLen := float64(constants.CleanerTrailLength)

	horizontalSpeed := gameWidth / constants.CleanerAnimationDuration.Seconds()
	verticalSpeed := gameHeight / constants.CleanerAnimationDuration.Seconds()

	ox := float64(originX)
	oy := float64(originY)

	// Define 4 directional cleaners: right, left, down, up
	directions := []struct {
		velocityX, velocityY float64
		startX, startY       float64
		targetX, targetY     float64
	}{
		{horizontalSpeed, 0, ox, oy, gameWidth + trailLen, oy},
		{-horizontalSpeed, 0, ox, oy, -trailLen, oy},
		{0, verticalSpeed, ox, oy, ox, gameHeight + trailLen},
		{0, -verticalSpeed, ox, oy, ox, -trailLen},
	}

	// Spawn 4 cleaners from origin, each traveling in a cardinal direction
	for _, dir := range directions {
		startGridX := int(dir.startX)
		startGridY := int(dir.startY)

		// Initialize trail ring buffer with starting position
		var trailRing [constants.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := components.CleanerComponent{
			PreciseX:  dir.startX,
			PreciseY:  dir.startY,
			VelocityX: dir.velocityX,
			VelocityY: dir.velocityY,
			TargetX:   dir.targetX,
			TargetY:   dir.targetY,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Char:      constants.CleanerChar,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: startGridX, Y: startGridY})
		world.Cleaners.Add(entity, comp)
		// TODO: centralize protection via entity factory
		world.Protections.Add(entity, components.ProtectionComponent{
			Mask: components.ProtectFromDrain | components.ProtectFromCull,
		})
	}
}

// scanRedCharacterRows finds all rows containing Red sequences using query builder
func (cs *CleanerSystem) scanRedCharacterRows(world *engine.World) []int {
	config := cs.res.Config
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
	var toDestroy []core.Entity

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
func (cs *CleanerSystem) spawnRemovalFlash(world *engine.World, targetEntity core.Entity) {
	if charComp, ok := world.Characters.Get(targetEntity); ok {
		if posComp, ok := world.Positions.Get(targetEntity); ok {
			world.PushEvent(events.EventFlashRequest, &events.FlashRequestPayload{
				X:    posComp.X,
				Y:    posComp.Y,
				Char: charComp.Rune,
			})
		}
	}
}