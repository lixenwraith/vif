package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// FuseSystem orchestrates drain-to-quasar transformation
// Receives EventFuseDrains from CleanerSystem when no valid targets exist at max heat
// Destroys all drains and spawns a single Quasar composite entity
type FuseSystem struct {
	world *engine.World
	res   engine.Resources

	drainStore  *engine.Store[component.DrainComponent]
	quasarStore *engine.Store[component.QuasarComponent]
	headerStore *engine.Store[component.CompositeHeaderComponent]
	memberStore *engine.Store[component.MemberComponent]
	protStore   *engine.Store[component.ProtectionComponent]
	glyphStore  *engine.Store[component.GlyphComponent]

	enabled bool
}

// NewFuseSystem creates a new fuse system
func NewFuseSystem(world *engine.World) engine.System {
	s := &FuseSystem{
		world: world,
		res:   engine.GetResources(world),

		drainStore:  engine.GetStore[component.DrainComponent](world),
		quasarStore: engine.GetStore[component.QuasarComponent](world),
		headerStore: engine.GetStore[component.CompositeHeaderComponent](world),
		memberStore: engine.GetStore[component.MemberComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
		glyphStore:  engine.GetStore[component.GlyphComponent](world),
	}
	s.initLocked()
	return s
}

func (s *FuseSystem) Init() {
	s.initLocked()
}

func (s *FuseSystem) initLocked() {
	s.enabled = true
}

func (s *FuseSystem) Priority() int {
	return constant.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseDrains,
		event.EventGameReset,
	}
}

func (s *FuseSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventFuseDrains {
		s.executeFuse()
	}
}

func (s *FuseSystem) Update() {
	// FuseSystem is purely event-driven, no tick logic
}

// executeFuse performs the drain-to-quasar transformation
func (s *FuseSystem) executeFuse() {
	// 1. Signal DrainSystem to stop spawning
	s.world.PushEvent(event.EventDrainPause, nil)

	// 2. Destroy all existing drains (silent, no visual effect)
	s.destroyAllDrains()

	// 3. Calculate spawn position (center of game area)
	spawnX, spawnY := s.calculateSpawnPosition()

	// 4. Clear entities at spawn location
	s.clearSpawnArea(spawnX, spawnY)

	// 5. Create Quasar composite
	anchorEntity := s.createQuasarComposite(spawnX, spawnY)

	// 6. Notify QuasarSystem
	s.world.PushEvent(event.EventQuasarSpawned, &event.QuasarSpawnedPayload{
		AnchorEntity: anchorEntity,
		OriginX:      spawnX,
		OriginY:      spawnY,
	})
}

// destroyAllDrains removes all drain entities without visual effects
func (s *FuseSystem) destroyAllDrains() {
	drains := s.drainStore.All()
	if len(drains) == 0 {
		return
	}

	// Batch silent death (no effect event)
	event.EmitDeathBatch(s.res.Events.Queue, 0, drains, s.res.Time.FrameNumber)
}

// calculateSpawnPosition returns center of game area
func (s *FuseSystem) calculateSpawnPosition() (int, int) {
	config := s.res.Config

	// Center position adjusted for phantom head offset
	// Phantom head is at (2,1) within the 5x3 grid
	// So top-left of quasar = center - offset
	centerX := config.GameWidth / 2
	centerY := config.GameHeight / 2

	// Clamp to ensure quasar fits within bounds
	topLeftX := centerX - constant.QuasarAnchorOffsetX
	topLeftY := centerY - constant.QuasarAnchorOffsetY

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
	return topLeftX + constant.QuasarAnchorOffsetX, topLeftY + constant.QuasarAnchorOffsetY
}

// clearSpawnArea destroys all entities within the quasar footprint
func (s *FuseSystem) clearSpawnArea(anchorX, anchorY int) {
	// Calculate top-left from anchor position
	topLeftX := anchorX - constant.QuasarAnchorOffsetX
	topLeftY := anchorY - constant.QuasarAnchorOffsetY

	cursorEntity := s.res.Cursor.Entity
	var toDestroy []core.Entity

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
				// Check protection
				if prot, ok := s.protStore.Get(e); ok {
					if prot.Mask == component.ProtectAll {
						continue
					}
				}
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.res.Events.Queue, 0, toDestroy, s.res.Time.FrameNumber)
	}
}

// createQuasarComposite builds the 3x5 quasar entity structure
func (s *FuseSystem) createQuasarComposite(anchorX, anchorY int) core.Entity {
	now := s.res.Time.GameTime

	// Calculate top-left from anchor position
	topLeftX := anchorX - constant.QuasarAnchorOffsetX
	topLeftY := anchorY - constant.QuasarAnchorOffsetY

	// Create phantom head (controller entity)
	anchorEntity := s.world.CreateEntity()
	s.world.Positions.Set(anchorEntity, component.PositionComponent{X: anchorX, Y: anchorY})

	// Phantom head is indestructible through lifecycle
	s.protStore.Set(anchorEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// Set QuasarComponent for runtime state
	s.quasarStore.Set(anchorEntity, component.QuasarComponent{
		LastMoveTime: now,
		IsOnCursor:   false,
	})

	// Build member entities
	members := make([]component.MemberEntry, 0, constant.QuasarWidth*constant.QuasarHeight)

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			memberX := topLeftX + col
			memberY := topLeftY + row

			// Calculate offset from anchor
			offsetX := int8(col - constant.QuasarAnchorOffsetX)
			offsetY := int8(row - constant.QuasarAnchorOffsetY)

			entity := s.world.CreateEntity()
			s.world.Positions.Set(entity, component.PositionComponent{X: memberX, Y: memberY})

			// Quasar members are not typeable - they're obstacles, no GlyphComponent set

			// Members protected from decay/delete but not from death (composite manages lifecycle)
			s.protStore.Set(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete,
			})

			// Backlink to anchor
			s.memberStore.Set(entity, component.MemberComponent{
				AnchorID: anchorEntity,
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
	s.headerStore.Set(anchorEntity, component.CompositeHeaderComponent{
		BehaviorID: component.BehaviorQuasar,
		Members:    members,
		VelX:       0,
		VelY:       0,
	})

	return anchorEntity
}