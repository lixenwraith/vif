package system

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	mu    sync.Mutex
	world *engine.World
	res   engine.Resources

	cleanerStore  *engine.Store[component.CleanerComponent]
	protStore     *engine.Store[component.ProtectionComponent]
	typeableStore *engine.Store[component.TypeableComponent]
	charStore     *engine.Store[component.CharacterComponent]
	energyStore   *engine.Store[component.EnergyComponent]

	spawned           map[int64]bool // Track which frames already spawned cleaners
	hasSpawnedSession bool           // Track if we spawned cleaners this session

	statActive  *atomic.Int64
	statSpawned *atomic.Int64

	enabled bool
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &CleanerSystem{
		world: world,
		res:   res,

		cleanerStore:  engine.GetStore[component.CleanerComponent](world),
		protStore:     engine.GetStore[component.ProtectionComponent](world),
		charStore:     engine.GetStore[component.CharacterComponent](world),
		typeableStore: engine.GetStore[component.TypeableComponent](world), energyStore: engine.GetStore[component.EnergyComponent](world),

		spawned: make(map[int64]bool),

		statActive:  res.Status.Ints.Get("cleaner.active"),
		statSpawned: res.Status.Ints.Get("cleaner.spawned"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *CleanerSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *CleanerSystem) initLocked() {
	clear(s.spawned)
	s.hasSpawnedSession = false
	s.enabled = true
}

// Priority returns the system's priority
func (s *CleanerSystem) Priority() int {
	return constant.PriorityCleaner
}

// EventTypes returns the event types CleanerSystem handles
func (s *CleanerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCleanerRequest,
		event.EventDirectionalCleanerRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes cleaner-related events from the router
func (s *CleanerSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	// Check if we already spawned for this frame (deduplication)
	if s.spawned[ev.Frame] {
		return
	}

	switch ev.Type {
	case event.EventCleanerRequest:
		s.spawnCleaners()
		s.spawned[ev.Frame] = true
		s.hasSpawnedSession = true

	case event.EventDirectionalCleanerRequest:
		if payload, ok := ev.Payload.(*event.DirectionalCleanerPayload); ok {
			s.spawnDirectionalCleaners(payload.OriginX, payload.OriginY)
			s.spawned[ev.Frame] = true
			s.hasSpawnedSession = true
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (s *CleanerSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.res.Config

	// Clean old entries from spawned map
	currentFrame := s.res.Time.FrameNumber
	for frame := range s.spawned {
		if currentFrame-frame > constant.CleanerDeduplicationWindow {
			delete(s.spawned, frame)
		}
	}

	entities := s.cleanerStore.All()
	s.statActive.Store(int64(len(entities)))

	// Push EventCleanerFinished when all cleaners have completed their animation
	if len(entities) == 0 && s.hasSpawnedSession {
		s.world.PushEvent(event.EventCleanerFinished, nil)
		s.hasSpawnedSession = false
		return
	}

	if len(entities) == 0 {
		return
	}

	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	gameWidth := config.GameWidth
	gameHeight := config.GameHeight

	for _, entity := range entities {
		c, ok := s.cleanerStore.Get(entity)
		if !ok {
			continue
		}

		// Read grid position from PositionStore (authoritative for spatial queries)
		oldPos, hasPos := s.world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Physics Update: Integrate velocity into float position (overlay state)
		prevPreciseX := c.PreciseX
		prevPreciseY := c.PreciseY
		c.Integrate(dtFixed)

		// Swept Collision Detection: Check all cells between previous and current position
		if c.VelY != 0 && c.VelX == 0 {
			// Vertical cleaner: sweep Y axis
			prevY := vmath.ToInt(prevPreciseY)
			currY := vmath.ToInt(c.PreciseY)
			startY, endY := prevY, currY
			if startY > endY {
				startY, endY = endY, startY
			}

			if startY < 0 {
				startY = 0
			}
			if endY >= gameHeight {
				endY = gameHeight - 1
			}

			// Check all traversed rows for collisions (with self-exclusion)
			if startY <= endY {
				for y := startY; y <= endY; y++ {
					s.checkCollisions(oldPos.X, y, entity)
				}
			}
		} else if c.VelX != 0 {
			// Horizontal cleaner: sweep X axis
			prevX := vmath.ToInt(prevPreciseX)
			currX := vmath.ToInt(c.PreciseX)
			startX, endX := prevX, currX
			if startX > endX {
				startX, endX = endX, startX
			}

			if startX < 0 {
				startX = 0
			}
			if endX >= gameWidth {
				endX = gameWidth - 1
			}

			// Check all traversed columns for collisions (with self-exclusion)
			if startX <= endX {
				for x := startX; x <= endX; x++ {
					s.checkCollisions(x, oldPos.Y, entity)
				}
			}
		}

		// Trail Update & Grid Sync: Update trail ring buffer and sync PositionStore if cell changed
		newGridX := vmath.ToInt(c.PreciseX)
		newGridY := vmath.ToInt(c.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			// Update trail: add new grid position to ring buffer
			c.TrailHead = (c.TrailHead + 1) % constant.CleanerTrailLength
			c.TrailRing[c.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if c.TrailLen < constant.CleanerTrailLength {
				c.TrailLen++
			}

			// Sync grid position to PositionStore
			s.world.Positions.Set(entity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		// Lifecycle Check: Destroy cleaner when it reaches target position
		shouldDestroy := false
		if c.VelX > 0 && c.PreciseX >= c.TargetX {
			shouldDestroy = true
		} else if c.VelX < 0 && c.PreciseX <= c.TargetX {
			shouldDestroy = true
		} else if c.VelY > 0 && c.PreciseY >= c.TargetY {
			shouldDestroy = true
		} else if c.VelY < 0 && c.PreciseY <= c.TargetY {
			shouldDestroy = true
		}

		if shouldDestroy {
			s.world.DestroyEntity(entity)
		} else {
			s.cleanerStore.Set(entity, c)
		}
	}

	entities = s.cleanerStore.All()
	// Push EventCleanerFinished when all cleaners have completed their animation
	if len(entities) == 0 && s.hasSpawnedSession {
		s.world.PushEvent(event.EventCleanerFinished, nil)
		s.hasSpawnedSession = false
	}
}

// spawnCleaners generates cleaner entities using generic stores
func (s *CleanerSystem) spawnCleaners() {
	config := s.res.Config

	redRows := s.scanTargetRows()

	spawnCount := len(redRows)
	// TODO: new phase trigger
	// Grayout: no targets to clean
	if spawnCount == 0 {
		s.res.State.State.TriggerGrayout(s.res.Time.GameTime)
		s.world.PushEvent(event.EventCleanerFinished, nil)
		return
	}
	s.statSpawned.Add(int64(spawnCount))

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	gameWidthFixed := vmath.FromInt(config.GameWidth)
	trailLenFixed := vmath.FromInt(constant.CleanerTrailLength)
	durationFixed := vmath.FromFloat(constant.CleanerAnimationDuration.Seconds())
	baseSpeed := vmath.Div(gameWidthFixed, durationFixed)

	// Spawn one cleaner per row with Red entities, alternating L→R and R→L direction
	for _, row := range redRows {
		var startX, targetX, velX int32
		rowFixed := vmath.FromInt(row)

		if row%2 != 0 {
			// Left to right
			startX = -trailLenFixed
			targetX = gameWidthFixed + trailLenFixed
			velX = baseSpeed
		} else {
			// Right to left
			startX = gameWidthFixed + trailLenFixed
			targetX = -trailLenFixed
			velX = -baseSpeed
		}

		startGridX := vmath.ToInt(startX)
		startGridY := row

		// Initialize trail ring buffer with starting position
		var trailRing [constant.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.CleanerComponent{
			KineticState: component.KineticState{
				PreciseX: startX,
				PreciseY: rowFixed,
				VelX:     velX,
				VelY:     0,
			},
			TargetX:   targetX,
			TargetY:   rowFixed,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Char:      constant.CleanerChar,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.Set(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.cleanerStore.Set(entity, comp)
		s.protStore.Set(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// checkCollisions handles collision logic with self-exclusion
func (s *CleanerSystem) checkCollisions(x, y int, selfEntity core.Entity) {
	// Query all entities at position (includes cleaner itself due to PositionStore registration)
	targetEntities := s.world.Positions.GetAllAt(x, y)
	if len(targetEntities) == 0 {
		return
	}

	// Determine mode based on energy polarity
	cursorEntity := s.res.Cursor.Entity
	negativeEnergy := false
	if energyComp, ok := s.energyStore.Get(cursorEntity); ok {
		negativeEnergy = energyComp.Current.Load() < 0
	}

	if negativeEnergy {
		s.processNegativeEnergy(x, y, targetEntities, selfEntity)
	} else {
		s.processPositiveEnergy(targetEntities, selfEntity)
	}
}

// processPositiveEnergy handles Red destruction with Blossom spawn
func (s *CleanerSystem) processPositiveEnergy(targetEntities []core.Entity, selfEntity core.Entity) {
	var toDestroy []core.Entity

	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}
		if typeable, ok := s.typeableStore.Get(e); ok {
			if typeable.Type == component.TypeRed {
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) == 0 {
		return
	}

	event.EmitDeathBatch(s.res.Events.Queue, event.EventBlossomSpawnOne, toDestroy, s.res.Time.FrameNumber)
}

// processNegativeEnergy handles Blue mutation to Green with Decay spawn
func (s *CleanerSystem) processNegativeEnergy(x, y int, targetEntities []core.Entity, selfEntity core.Entity) {
	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}

		typeable, ok := s.typeableStore.Get(e)
		if !ok || typeable.Type != component.TypeBlue {
			continue
		}

		// Mutate Blue → Green, preserving level
		s.mutateBlueToGreen(e, typeable)

		// Spawn decay at same position (particle skips starting cell via LastIntX/Y)
		s.world.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:             x,
			Y:             y,
			Char:          typeable.Char,
			SkipStartCell: true,
		})
	}
}

// mutateBlueToGreen transforms a Blue typeable to Green, preserving level
func (s *CleanerSystem) mutateBlueToGreen(entity core.Entity, typeable component.TypeableComponent) {
	// Update TypeableComponent
	typeable.Type = component.TypeGreen
	s.typeableStore.Set(entity, typeable)

	// Sync CharacterComponent for rendering
	if char, ok := s.charStore.Get(entity); ok {
		char.Type = component.CharacterGreen
		s.charStore.Set(entity, char)
	}
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position
func (s *CleanerSystem) spawnDirectionalCleaners(originX, originY int) {
	config := s.res.Config

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	gameWidthFixed := vmath.FromInt(config.GameWidth)
	gameHeightFixed := vmath.FromInt(config.GameHeight)
	trailLenFixed := vmath.FromInt(constant.CleanerTrailLength)
	durationFixed := vmath.FromFloat(constant.CleanerAnimationDuration.Seconds())

	horizontalSpeed := vmath.Div(gameWidthFixed, durationFixed)
	verticalSpeed := vmath.Div(gameHeightFixed, durationFixed)

	oxFixed := vmath.FromInt(originX)
	oyFixed := vmath.FromInt(originY)

	type dirDef struct {
		velX, velY       int32
		startX, startY   int32
		targetX, targetY int32
	}

	directions := []dirDef{
		{horizontalSpeed, 0, oxFixed, oyFixed, gameWidthFixed + trailLenFixed, oyFixed},
		{-horizontalSpeed, 0, oxFixed, oyFixed, -trailLenFixed, oyFixed},
		{0, verticalSpeed, oxFixed, oyFixed, oxFixed, gameHeightFixed + trailLenFixed},
		{0, -verticalSpeed, oxFixed, oyFixed, oxFixed, -trailLenFixed},
	}

	// Spawn 4 cleaners from origin, each traveling in a cardinal direction
	for _, dir := range directions {
		startGridX := vmath.ToInt(dir.startX)
		startGridY := vmath.ToInt(dir.startY)

		// Initialize trail ring buffer with starting position
		var trailRing [constant.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.CleanerComponent{
			KineticState: component.KineticState{
				PreciseX: dir.startX,
				PreciseY: dir.startY,
				VelX:     dir.velX,
				VelY:     dir.velY,
			},
			TargetX:   dir.targetX,
			TargetY:   dir.targetY,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Char:      constant.CleanerChar,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.Set(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.cleanerStore.Set(entity, comp)
		// TODO: centralize protection via entity factory
		s.protStore.Set(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// scanTargetRows finds rows containing target character type based on energy polarity
// Returns rows with TypeRed (energy >= 0) or TypeBlue (energy < 0)
func (s *CleanerSystem) scanTargetRows() []int {
	config := s.res.Config
	gameHeight := config.GameHeight

	// Determine target type based on energy polarity
	targetType := component.TypeRed
	cursorEntity := s.res.Cursor.Entity
	if energyComp, ok := s.energyStore.Get(cursorEntity); ok {
		if energyComp.Current.Load() < 0 {
			targetType = component.TypeBlue
		}
	}

	targetRows := make(map[int]bool)

	entities := s.world.Query().
		With(s.typeableStore).
		With(s.world.Positions).
		Execute()

	for _, entity := range entities {
		typeable, ok := s.typeableStore.Get(entity)
		if !ok || typeable.Type != targetType {
			continue
		}

		pos, hasPos := s.world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		if pos.Y >= 0 && pos.Y < gameHeight {
			targetRows[pos.Y] = true
		}
	}

	rows := make([]int, 0, len(targetRows))
	for row := range targetRows {
		rows = append(rows, row)
	}
	return rows
}

// // scanRedCharacterRows finds all rows containing Red typeables using query builder
// func (s *CleanerSystem) scanRedCharacterRows() []int {
// 	config := s.res.Config
// 	redRows := make(map[int]bool)
// 	gameHeight := config.GameHeight
//
// 	// Query entities with both Typeable and Positions
// 	entities := s.world.Query().
// 		With(s.typeableStore).
// 		With(s.world.Positions).
// 		Execute()
//
// 	for _, entity := range entities {
// 		typeable, ok := s.typeableStore.Get(entity)
// 		if !ok {
// 			continue
// 		}
//
// 		if typeable.Type != component.TypeRed {
// 			continue
// 		}
//
// 		// Retrieve Position
// 		pos, hasPos := s.world.Positions.Get(entity)
// 		if !hasPos {
// 			continue
// 		}
//
// 		// Set row if in bounds
// 		if pos.Y >= 0 && pos.Y < gameHeight {
// 			redRows[pos.Y] = true
// 		}
// 	}
//
// 	rows := make([]int, 0, len(redRows))
// 	for row := range redRows {
// 		rows = append(rows, row)
// 	}
// 	return rows
// }