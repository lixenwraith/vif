package system

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// pendingSwarmFuse tracks an in-progress drain→swarm fusion
type pendingSwarmFuse struct {
	targetX int
	targetY int
	timer   time.Duration
}

// FuseSystem orchestrates drain-to-quasar and drain-to-swarm transformations
type FuseSystem struct {
	world *engine.World

	// Quasar fusion state (single active)
	fusing    bool
	fuseTimer time.Duration
	targetX   int
	targetY   int

	// Swarm fusion state (multiple concurrent)
	pendingSwarmFusions []pendingSwarmFuse

	enabled bool
}

// NewFuseSystem creates a new fuse system
func NewFuseSystem(world *engine.World) engine.System {
	s := &FuseSystem{
		world: world,
	}

	s.pendingSwarmFusions = make([]pendingSwarmFuse, 0)

	s.Init()
	return s
}

func (s *FuseSystem) Init() {
	s.fusing = false
	s.fuseTimer = 0
	s.targetX = 0
	s.targetY = 0
	s.pendingSwarmFusions = s.pendingSwarmFusions[:0]
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
		event.EventQuasarFuseRequest,
		event.EventSwarmFuseRequest,
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

	switch ev.Type {
	case event.EventQuasarFuseRequest:
		if !s.fusing {
			s.handleQuasarFuse()
		}

	case event.EventSwarmFuseRequest:
		if payload, ok := ev.Payload.(*event.SwarmFuseRequestPayload); ok {
			s.handleSwarmFuse(payload.DrainA, payload.DrainB)
		}
	}
}

func (s *FuseSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	// Process quasar fusion timer
	if s.fusing {
		s.fuseTimer -= dt
		if s.fuseTimer <= 0 {
			s.completeQuasarFuse()
		}
	}

	// Process swarm fusion timers
	s.processSwarmFusions(dt)
}

// handleQuasarFuse performs the drain-to-quasar transformation
func (s *FuseSystem) handleQuasarFuse() {
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
	s.fuseTimer = constant.SpiritAnimationDuration + constant.SpiritSafetyBuffer
}

// completeQuasarFuse finalizes the transformation after timer expires
func (s *FuseSystem) completeQuasarFuse() {
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

// handleSwarmFuse initiates drain→swarm fusion
func (s *FuseSystem) handleSwarmFuse(drainA, drainB core.Entity) {
	// Get positions before destruction
	posA, okA := s.world.Positions.GetPosition(drainA)
	posB, okB := s.world.Positions.GetPosition(drainB)
	if !okA || !okB {
		return
	}

	// Calculate midpoint
	midX := (posA.X + posB.X) / 2
	midY := (posA.Y + posB.Y) / 2

	// Clamp to valid spawn area
	midX, midY = s.clampSwarmSpawnPosition(midX, midY)

	// Destroy both drains silently
	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, []core.Entity{drainA, drainB})

	// Spawn spirits converging to midpoint
	s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
		StartX:    posA.X,
		StartY:    posA.Y,
		TargetX:   midX,
		TargetY:   midY,
		Char:      constant.DrainChar,
		BaseColor: component.SpiritCyan,
	})
	s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
		StartX:    posB.X,
		StartY:    posB.Y,
		TargetX:   midX,
		TargetY:   midY,
		Char:      constant.DrainChar,
		BaseColor: component.SpiritCyan,
	})

	// Track pending fusion
	s.pendingSwarmFusions = append(s.pendingSwarmFusions, pendingSwarmFuse{
		targetX: midX,
		targetY: midY,
		timer:   constant.SwarmFuseAnimationDuration,
	})
	// s.world.DebugPrint(fmt.Sprintf("%d", len(s.pendingSwarmFusions)))
}

// processSwarmFusions decrements timers and completes ready fusions
func (s *FuseSystem) processSwarmFusions(dt time.Duration) {
	s.world.DebugPrint(fmt.Sprintf("%d", len(s.pendingSwarmFusions)))
	if len(s.pendingSwarmFusions) == 0 {
		return
	}

	// Process in reverse to allow safe removal
	for i := len(s.pendingSwarmFusions) - 1; i >= 0; i-- {
		s.pendingSwarmFusions[i].timer -= dt

		if s.pendingSwarmFusions[i].timer <= 0 {
			s.completeSwarmFuse(s.pendingSwarmFusions[i].targetX, s.pendingSwarmFusions[i].targetY)

			// Remove completed fusion (swap with last)
			s.pendingSwarmFusions[i] = s.pendingSwarmFusions[len(s.pendingSwarmFusions)-1]
			s.pendingSwarmFusions = s.pendingSwarmFusions[:len(s.pendingSwarmFusions)-1]
			// s.pendingSwarmFusions = append(s.pendingSwarmFusions[:i], s.pendingSwarmFusions[i+1:]...)
		}
	}
}

// completeSwarmFuse creates swarm composite at target position
func (s *FuseSystem) completeSwarmFuse(targetX, targetY int) {
	// Clear spawn area
	s.clearSwarmSpawnArea(targetX, targetY)

	// Create swarm composite
	headerEntity := s.createSwarmComposite(targetX, targetY)

	// Notify SwarmSystem
	s.world.PushEvent(event.EventSwarmSpawned, &event.SwarmSpawnedPayload{
		HeaderEntity: headerEntity,
		SpawnX:       targetX,
		SpawnY:       targetY,
	})
}

// clampSwarmSpawnPosition ensures swarm fits within bounds
func (s *FuseSystem) clampSwarmSpawnPosition(targetX, targetY int) (int, int) {
	config := s.world.Resources.Config

	// Header at (1,0) offset, so top-left = header - offset
	topLeftX := targetX - constant.SwarmHeaderOffsetX
	topLeftY := targetY - constant.SwarmHeaderOffsetY

	if topLeftX < 0 {
		topLeftX = 0
	}
	if topLeftY < 0 {
		topLeftY = 0
	}
	if topLeftX+constant.SwarmWidth > config.GameWidth {
		topLeftX = config.GameWidth - constant.SwarmWidth
	}
	if topLeftY+constant.SwarmHeight > config.GameHeight {
		topLeftY = config.GameHeight - constant.SwarmHeight
	}

	return topLeftX + constant.SwarmHeaderOffsetX, topLeftY + constant.SwarmHeaderOffsetY
}

// clearSwarmSpawnArea destroys entities within swarm footprint
func (s *FuseSystem) clearSwarmSpawnArea(headerX, headerY int) {
	topLeftX := headerX - constant.SwarmHeaderOffsetX
	topLeftY := headerY - constant.SwarmHeaderOffsetY

	cursorEntity := s.world.Resources.Cursor.Entity
	var toDestroy []core.Entity

	for row := 0; row < constant.SwarmHeight; row++ {
		for col := 0; col < constant.SwarmWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
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

// createSwarmComposite builds the 4×2 swarm entity structure
func (s *FuseSystem) createSwarmComposite(headerX, headerY int) core.Entity {
	topLeftX := headerX - constant.SwarmHeaderOffsetX
	topLeftY := headerY - constant.SwarmHeaderOffsetY

	// Create phantom head
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	// Phantom head is indestructible
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// Initialize swarm component
	s.world.Components.Swarm.SetComponent(headerEntity, component.SwarmComponent{
		State:                   component.SwarmStateChase,
		PatternIndex:            0,
		PatternRemaining:        constant.SwarmPatternDuration,
		ChargeIntervalRemaining: constant.SwarmChargeInterval,
		ChargesCompleted:        0,
	})

	// Initialize kinetic
	kinetic := core.Kinetic{
		PreciseX: vmath.FromInt(headerX),
		PreciseY: vmath.FromInt(headerY),
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{Kinetic: kinetic})

	// Initialize combat
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntitySwarm,
		HitPoints:        constant.CombatInitialHPSwarm,
	})

	// TODO
	// // Lifetime timer for automatic despawn
	// s.world.Components.Timer.SetComponent(headerEntity, component.TimerComponent{
	// 	Remaining: constant.SwarmLifetime,
	// })

	// Build member entities (pre-allocate all 8 positions)
	members := make([]component.MemberEntry, 0, constant.SwarmWidth*constant.SwarmHeight)

	for row := 0; row < constant.SwarmHeight; row++ {
		for col := 0; col < constant.SwarmWidth; col++ {
			memberX := topLeftX + col
			memberY := topLeftY + row

			offsetX := col - constant.SwarmHeaderOffsetX
			offsetY := row - constant.SwarmHeaderOffsetY

			entity := s.world.CreateEntity()
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete,
			})

			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			// Layer determined by pattern visibility (LayerGlyph = active, LayerEffect = inactive)
			layer := component.LayerGlyph
			if !component.SwarmPatternActive[0][row][col] {
				layer = component.LayerEffect
			}

			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: offsetX,
				OffsetY: offsetY,
				Layer:   layer,
			})
		}
	}

	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorSwarm,
		MemberEntries: members,
	})

	return headerEntity
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
			offsetX := col - constant.QuasarHeaderOffsetX
			offsetY := row - constant.QuasarHeaderOffsetY

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