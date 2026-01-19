package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// FuseSystem orchestrates drain-to-quasar transformation, destroying all drains and spawning a single quasar composite entity
type FuseSystem struct {
	world *engine.World

	// Fusion state machine
	fusing    bool
	fuseTimer int64 // Remaining time in nanoseconds

	// Quasar spawn position (centroid of drains)
	targetX int
	targetY int

	enabled bool
}

// NewFuseSystem creates a new fuse system
func NewFuseSystem(world *engine.World) engine.System {
	s := &FuseSystem{
		world: world,
	}

	s.Init()
	return s
}

func (s *FuseSystem) Init() {
	s.fusing = false
	s.fuseTimer = 0
	s.targetX = 0
	s.targetY = 0
	s.enabled = true
}

// Name returns system's name
func (s *FuseSystem) Name() string {
	return "fuse"
}

func (s *FuseSystem) Priority() int {
	return constant.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseDrains,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *FuseSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		// Abort any in-progress fusion
		if s.fusing {
			s.world.PushEvent(event.EventSpiritDespawn, nil)
		}
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventFuseDrains {
		if !s.fusing {
			s.executeFuse()
		}
	}
}

func (s *FuseSystem) Update() {
	if !s.enabled || !s.fusing {
		return
	}

	// Decrement timer
	s.fuseTimer -= s.world.Resources.Time.DeltaTime.Nanoseconds()

	if s.fuseTimer <= 0 {
		s.completeFuse()
	}
}

// executeFuse performs the drain-to-quasar transformation
func (s *FuseSystem) executeFuse() {
	// 1. Signal DrainSystem to stop spawning
	s.world.PushEvent(event.EventDrainPause, nil)

	// 2. Collect active drains and their positions
	drains := s.world.Components.Drain.GetAllEntities()
	coords := make([]int, 0, len(drains)*2)
	validDrains := make([]core.Entity, 0, len(drains))

	for _, e := range drains {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			coords = append(coords, pos.X, pos.Y)
			validDrains = append(validDrains, e)
		}
	}

	// 3. Calculate Centroid
	cX, cY := vmath.CalculateCentroid(coords)

	// Fallback to center screen if no drains
	if len(coords) == 0 {
		config := s.world.Resources.Config
		cX = config.GameWidth / 2
		cY = config.GameHeight / 2
	}

	// 4. Determine Spawn Positions (clamped)
	s.targetX, s.targetY = s.clampSpawnPosition(cX, cY)

	// 5. Spawn Lightning Effects
	for i := range validDrains {
		// Drains are at coords[i*2], coords[i*2+1]
		originX := coords[i*2]
		originY := coords[i*2+1]

		// Spawn transient lightning entity
		s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
			StartX:    originX,
			StartY:    originY,
			TargetX:   s.targetX,
			TargetY:   s.targetY,
			Char:      constant.DrainChar,
			BaseColor: component.SpiritCyan,
		})
	}

	// 6. Cleanup Pending Materializers (Fixes artifact issue)
	mats := s.world.Components.Materialize.GetAllEntities()
	for _, e := range mats {
		if m, ok := s.world.Components.Materialize.GetComponent(e); ok && m.Type == component.SpawnTypeDrain {
			s.world.DestroyEntity(e)
		}
	}

	// 7. Destroy all existing drains (silent, no visual effect)
	s.destroyAllDrains()

	// 8. Start timer
	s.fusing = true
	s.fuseTimer = (constant.SpiritAnimationDuration + constant.SpiritSafetyBuffer).Nanoseconds()
}

// completeFuse finalizes the transformation after timer expires
func (s *FuseSystem) completeFuse() {
	// 1. Safety cleanup - despawn any remaining spirits
	s.world.PushEvent(event.EventSpiritDespawn, nil)

	// 2. Clear spawn area
	s.clearSpawnArea(s.targetX, s.targetY)

	// 3. Create Quasar composite
	headerEntity := s.createQuasarComposite(s.targetX, s.targetY)

	// 4. Notify QuasarSystem
	s.world.PushEvent(event.EventQuasarSpawned, &event.QuasarSpawnedPayload{
		HeaderEntity: headerEntity,
	})

	// 5. Reset state
	s.fusing = false
	s.fuseTimer = 0
}

// destroyAllDrains removes all drain entities without visual effects
func (s *FuseSystem) destroyAllDrains() {
	drains := s.world.Components.Drain.GetAllEntities()
	if len(drains) == 0 {
		return
	}

	// Batch silent death (no effect event)
	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, drains)
}

// clampSpawnPosition ensures the Quasar fits within bounds given a target center
// Input x, y is the desired center (or centroid)
// Returns the Phantom Head position (Quasar header)
func (s *FuseSystem) clampSpawnPosition(targetX, targetY int) (int, int) {
	config := s.world.Resources.Config

	// Phantom head is at (2,1) offset relative to Quasar top-left (0,0)
	// We want targetX, targetY to be roughly the center of the Quasar
	// TopLeft = Center - CenterOffset
	// Anchor = TopLeft + AnchorOffset
	// Simplified: Anchor = Target

	// However, we must ensure the entire 3x5 grid fits
	// TopLeft = Anchor - AnchorOffset
	topLeftX := targetX - constant.QuasarHeaderOffsetX
	topLeftY := targetY - constant.QuasarHeaderOffsetY

	if topLeftX < 0 {
		topLeftX = 0
	}
	if topLeftY < 0 {
		topLeftY = 0
	}
	if topLeftX+constant.QuasarWidth > config.GameWidth {
		topLeftX = config.GameWidth - constant.QuasarWidth
	}
	if topLeftY+constant.QuasarHeight > config.GameHeight {
		topLeftY = config.GameHeight - constant.QuasarHeight
	}

	// Return adjusted header position
	return topLeftX + constant.QuasarHeaderOffsetX, topLeftY + constant.QuasarHeaderOffsetY
}

// calculateSpawnPosition returns center of game area
func (s *FuseSystem) calculateSpawnPosition() (int, int) {
	config := s.world.Resources.Config

	// Center position adjusted for phantom head offset
	// Phantom head is at (2,1) within the 5x3 grid
	// So top-left of quasar = center - offset
	centerX := config.GameWidth / 2
	centerY := config.GameHeight / 2

	// Clamp to ensure quasar fits within bounds
	topLeftX := centerX - constant.QuasarHeaderOffsetX
	topLeftY := centerY - constant.QuasarHeaderOffsetY

	if topLeftX < 0 {
		topLeftX = 0
	}
	if topLeftY < 0 {
		topLeftY = 0
	}
	if topLeftX+constant.QuasarWidth > config.GameWidth {
		topLeftX = config.GameWidth - constant.QuasarWidth
	}
	if topLeftY+constant.QuasarHeight > config.GameHeight {
		topLeftY = config.GameHeight - constant.QuasarHeight
	}

	// Return phantom head position (center of quasar)
	return topLeftX + constant.QuasarHeaderOffsetX, topLeftY + constant.QuasarHeaderOffsetY
}

// clearSpawnArea destroys all entities within the quasar footprint
func (s *FuseSystem) clearSpawnArea(headerX, headerY int) {
	// Calculate top-left from header position
	topLeftX := headerX - constant.QuasarHeaderOffsetX
	topLeftY := headerY - constant.QuasarHeaderOffsetY

	cursorEntity := s.world.Resources.Cursor.Entity
	var toDestroy []core.Entity

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
				// Check protection
				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask == component.ProtectAll {
						continue
					}
				}
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, toDestroy)
	}
}

// createQuasarComposite builds the 3x5 quasar entity structure
func (s *FuseSystem) createQuasarComposite(headerX, headerY int) core.Entity {
	// Calculate top-left from header position
	topLeftX := headerX - constant.QuasarHeaderOffsetX
	topLeftY := headerY - constant.QuasarHeaderOffsetY

	// Create phantom head (controller entity)
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	// Phantom head is indestructible through lifecycle
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// Set quasar components
	s.world.Components.Quasar.SetComponent(headerEntity, component.QuasarComponent{
		SpeedMultiplier: vmath.Scale,
	})

	kinetic := core.Kinetic{
		PreciseX: vmath.FromInt(headerX),
		PreciseY: vmath.FromInt(headerY),
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{kinetic})

	// Build member entities
	members := make([]component.MemberEntry, 0, constant.QuasarWidth*constant.QuasarHeight)

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			memberX := topLeftX + col
			memberY := topLeftY + row

			// Calculate offset from header
			offsetX := int8(col - constant.QuasarHeaderOffsetX)
			offsetY := int8(row - constant.QuasarHeaderOffsetY)

			entity := s.world.CreateEntity()
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			// MemberEntries protected from decay/delete but not from death (composite manages lifecycle)
			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete,
			})

			// Backlink to header
			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: offsetX,
				OffsetY: offsetY,
				Layer:   component.LayerEffect,
			})
		}
	}

	// Set composite header on phantom head
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorQuasar,
		MemberEntries: members,
	})

	return headerEntity
}