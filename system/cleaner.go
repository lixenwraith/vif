package system

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
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

	case event.EventGameReset:
		s.Init()
		return
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (s *CleanerSystem) Update() {
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

	dtSeconds := s.res.Time.DeltaTime.Seconds()
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
		c.PreciseX += c.VelocityX * dtSeconds
		c.PreciseY += c.VelocityY * dtSeconds

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
					s.checkAndDestroyAtPositionExcluding(oldPos.X, y, entity)
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
					s.checkAndDestroyAtPositionExcluding(x, oldPos.Y, entity)
				}
			}
		}

		// Trail Update & Grid Sync: Update trail ring buffer and sync PositionStore if cell changed
		newGridX := int(c.PreciseX)
		newGridY := int(c.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			// Update trail: add new grid position to ring buffer
			c.TrailHead = (c.TrailHead + 1) % constant.CleanerTrailLength
			c.TrailRing[c.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if c.TrailLen < constant.CleanerTrailLength {
				c.TrailLen++
			}

			// Sync grid position to PositionStore
			s.world.Positions.Add(entity, component.PositionComponent{X: newGridX, Y: newGridY})
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
			s.world.DestroyEntity(entity)
		} else {
			s.cleanerStore.Add(entity, c)
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

	redRows := s.scanRedCharacterRows()

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

	gameWidth := float64(config.GameWidth)
	duration := constant.CleanerAnimationDuration.Seconds()
	baseSpeed := gameWidth / duration
	trailLen := float64(constant.CleanerTrailLength)

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
		var trailRing [constant.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.CleanerComponent{
			PreciseX:  startX,
			PreciseY:  float64(row),
			VelocityX: velX,
			VelocityY: 0,
			TargetX:   targetX,
			TargetY:   float64(row),
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Char:      constant.CleanerChar,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.Add(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.cleanerStore.Add(entity, comp)
		s.protStore.Add(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// checkAndDestroyAtPositionExcluding handles collision logic with self-exclusion
func (s *CleanerSystem) checkAndDestroyAtPositionExcluding(x, y int, selfEntity core.Entity) {
	// Query all entities at position (includes cleaner itself due to PositionStore registration)
	targetEntities := s.world.Positions.GetAllAt(x, y)

	var toDestroy []core.Entity

	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue // Self-exclusion: skip cleaner entity to prevent self-destruction
		}
		// Only destroy Red typeable entities
		if typeable, ok := s.typeableStore.Get(e); ok {
			if typeable.Type == component.TypeRed {
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) == 0 {
		return
	}

	// Determine effect based on energy polarity
	effectEvent := event.EventFlashRequest
	cursorEntity := s.res.Cursor.Entity
	if energyComp, ok := s.energyStore.Get(cursorEntity); ok {
		if energyComp.Current.Load() < 0 {
			effectEvent = event.EventDecaySpawnOne
		}
	}

	// Batch death with effect
	event.EmitDeathBatch(s.res.Events.Queue, effectEvent, toDestroy, s.res.Time.FrameNumber)
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position
func (s *CleanerSystem) spawnDirectionalCleaners(originX, originY int) {
	config := s.res.Config

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	trailLen := float64(constant.CleanerTrailLength)

	horizontalSpeed := gameWidth / constant.CleanerAnimationDuration.Seconds()
	verticalSpeed := gameHeight / constant.CleanerAnimationDuration.Seconds()

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
		var trailRing [constant.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.CleanerComponent{
			PreciseX:  dir.startX,
			PreciseY:  dir.startY,
			VelocityX: dir.velocityX,
			VelocityY: dir.velocityY,
			TargetX:   dir.targetX,
			TargetY:   dir.targetY,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Char:      constant.CleanerChar,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.Add(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.cleanerStore.Add(entity, comp)
		// TODO: centralize protection via entity factory
		s.protStore.Add(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// scanRedCharacterRows finds all rows containing Red typeables using query builder
func (s *CleanerSystem) scanRedCharacterRows() []int {
	config := s.res.Config
	redRows := make(map[int]bool)
	gameHeight := config.GameHeight

	// Query entities with both Typeable and Positions
	entities := s.world.Query().
		With(s.typeableStore).
		With(s.world.Positions).
		Execute()

	for _, entity := range entities {
		typeable, ok := s.typeableStore.Get(entity)
		if !ok {
			continue
		}

		if typeable.Type != component.TypeRed {
			continue
		}

		// Retrieve Position
		pos, hasPos := s.world.Positions.Get(entity)
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