package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	matStore    *engine.Store[component.MaterializeComponent]
	spiritStore *engine.Store[component.SpiritComponent]
	timerStore  *engine.Store[component.TimerComponent]

	// Fusion state machine
	fusing    bool
	fuseTimer int64 // Remaining time in nanoseconds
	targetX   int   // Quasar spawnLightning position (centroid)
	targetY   int

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
		matStore:    engine.GetStore[component.MaterializeComponent](world),
		spiritStore: engine.GetStore[component.SpiritComponent](world),
		timerStore:  engine.GetStore[component.TimerComponent](world),
	}
	s.initLocked()
	return s
}

func (s *FuseSystem) Init() {
	s.initLocked()
}

func (s *FuseSystem) initLocked() {
	s.fusing = false
	s.fuseTimer = 0
	s.targetX = 0
	s.targetY = 0
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
		// Abort any in-progress fusion
		if s.fusing {
			s.world.PushEvent(event.EventSpiritDespawn, nil)
		}
		s.Init()
		return
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
	s.fuseTimer -= s.res.Time.DeltaTime.Nanoseconds()

	if s.fuseTimer <= 0 {
		s.completeFuse()
	}
}

// executeFuse performs the drain-to-quasar transformation
func (s *FuseSystem) executeFuse() {
	// 1. Signal DrainSystem to stop spawning
	s.world.PushEvent(event.EventDrainPause, nil)

	// 2. Collect active drains and their positions
	drains := s.drainStore.All()
	coords := make([]int, 0, len(drains)*2)
	validDrains := make([]core.Entity, 0, len(drains))

	for _, e := range drains {
		if pos, ok := s.world.Positions.Get(e); ok {
			coords = append(coords, pos.X, pos.Y)
			validDrains = append(validDrains, e)
		}
	}

	// 3. Calculate Centroid
	cX, cY := vmath.CalculateCentroid(coords)

	// Fallback to center screen if no drains
	if len(coords) == 0 {
		config := s.res.Config
		cX = config.GameWidth / 2
		cY = config.GameHeight / 2
	}

	// 4. Determine Spawn Position (clamped)
	s.targetX, s.targetY = s.clampSpawnPosition(cX, cY)

	// 5. Spawn Lightning Effects
	for i := range validDrains {
		// Drains are at coords[i*2], coords[i*2+1]
		originX := coords[i*2]
		originY := coords[i*2+1]

		// Spawn transient lightning entity
		s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnPayload{
			StartX:  originX,
			StartY:  originY,
			TargetX: s.targetX,
			TargetY: s.targetY,
			Char:    constant.DrainChar,
			// TODO: this is bad, only system needing colors, find a way not to import terminal or render
			BaseColor:  terminal.RGB(render.RgbDrain),
			BlinkColor: terminal.RGB{R: 255, G: 255, B: 0}, // Yellow blink
		})
	}

	// 6. Cleanup Pending Materializers (Fixes artifact issue)
	mats := s.matStore.All()
	for _, e := range mats {
		if m, ok := s.matStore.Get(e); ok && m.Type == component.SpawnTypeDrain {
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
	// 1. Safety cleanup - despawnLightning any remaining spirits
	s.world.PushEvent(event.EventSpiritDespawn, nil)

	// 2. Clear spawnLightning area
	s.clearSpawnArea(s.targetX, s.targetY)

	// 3. Create Quasar composite
	anchorEntity := s.createQuasarComposite(s.targetX, s.targetY)

	// 4. Notify QuasarSystem
	s.world.PushEvent(event.EventQuasarSpawned, &event.QuasarSpawnedPayload{
		AnchorEntity: anchorEntity,
		OriginX:      s.targetX,
		OriginY:      s.targetY,
	})

	// 5. Reset state
	s.fusing = false
	s.fuseTimer = 0
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

// clampSpawnPosition ensures the Quasar fits within bounds given a target center
// Input x, y is the desired center (or centroid)
// Returns the Phantom Head position (Quasar anchor)
func (s *FuseSystem) clampSpawnPosition(targetX, targetY int) (int, int) {
	config := s.res.Config

	// Phantom head is at (2,1) offset relative to Quasar top-left (0,0)
	// We want targetX, targetY to be roughly the center of the Quasar
	// TopLeft = Center - CenterOffset
	// Anchor = TopLeft + AnchorOffset
	// Simplified: Anchor = Target

	// However, we must ensure the entire 3x5 grid fits
	// TopLeft = Anchor - AnchorOffset
	topLeftX := targetX - constant.QuasarAnchorOffsetX
	topLeftY := targetY - constant.QuasarAnchorOffsetY

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

	// Return adjusted anchor position
	return topLeftX + constant.QuasarAnchorOffsetX, topLeftY + constant.QuasarAnchorOffsetY
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
		TicksSinceLastMove:  0,
		TicksSinceLastSpeed: 0,
		IsOnCursor:          false,
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