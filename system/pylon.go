package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// pylonCacheEntry holds cached entity position for soft collision
type pylonCacheEntry struct {
	entity core.Entity
	x, y   int
}

// PylonSystem manages pylon enemy entity lifecycle
// Pylon is a stationary ablative composite that acts as damage sponge
// Pushes other enemies away via soft collision
type PylonSystem struct {
	world *engine.World

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewPylonSystem creates a new pylon system
func NewPylonSystem(world *engine.World) engine.System {
	s := &PylonSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("pylon.active")
	s.statCount = world.Resources.Status.Ints.Get("pylon.count")

	s.Init()
	return s
}

func (s *PylonSystem) Init() {
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

func (s *PylonSystem) Name() string {
	return "pylon"
}

func (s *PylonSystem) Priority() int {
	return parameter.PriorityPylon
}

func (s *PylonSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventPylonSpawnRequest,
		event.EventPylonCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *PylonSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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
	case event.EventPylonSpawnRequest:
		if payload, ok := ev.Payload.(*event.PylonSpawnRequestPayload); ok {
			s.spawnPylon(payload)
		}

	case event.EventPylonCancelRequest:
		s.terminateAll()

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorPylon && payload.RemainingCount == 0 {
				s.handlePylonDeath(payload.HeaderEntity)
			}
		}
	}
}

func (s *PylonSystem) Update() {
	if !s.enabled {
		return
	}

	pylonEntities := s.world.Components.Pylon.GetAllEntities()
	if len(pylonEntities) == 0 {
		s.statActive.Store(false)
		s.statCount.Store(0)
		return
	}

	activeCount := 0

	for _, headerEntity := range pylonEntities {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Process member combat (HP <= 0 detection)
		// Deaths routed through CompositeSystem; IntegrityBreach triggers handlePylonDeath
		s.processAblativeCombat(headerEntity, &headerComp)

		// Cursor/shield interaction
		s.handleInteractions(headerEntity)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// handleInteractions processes shield drain and cursor collision
func (s *PylonSystem) handleInteractions(headerEntity core.Entity) {
	cursorEntity := s.world.Resources.Player.Entity

	overlap := CheckCursorOverlap(s.world, headerEntity)

	if len(overlap.ShieldMembers) > 0 {
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: parameter.PylonShieldDrain,
		})

		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackShield,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: headerEntity,
			HitEntities:  overlap.ShieldMembers,
		})
	} else if overlap.OnCursor && !overlap.ShieldActive {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Delta: -parameter.PylonDamageHeat,
		})
	}
}

func (s *PylonSystem) spawnPylon(payload *event.PylonSpawnRequestPayload) {
	radiusX := payload.RadiusX
	radiusY := payload.RadiusY
	if radiusX <= 0 {
		radiusX = parameter.PylonDefaultRadiusX
	}
	if radiusY <= 0 {
		radiusY = parameter.PylonDefaultRadiusY
	}

	minHP := payload.MinHP
	maxHP := payload.MaxHP
	if minHP <= 0 {
		minHP = parameter.CombatInitialHPPylonMin
	}
	if maxHP <= 0 {
		maxHP = parameter.CombatInitialHPPylonMax
	}
	if minHP > maxHP {
		minHP = maxHP
	}

	var centerX, centerY int

	if payload.X == 0 && payload.Y == 0 {
		// Random placement
		var ok bool
		centerX, centerY, ok = s.findRandomPylonPosition(radiusX, radiusY)
		if !ok {
			s.world.PushEvent(event.EventPylonSpawnFailed, nil)
			return
		}
	} else {
		// Explicit placement — validate
		centerX, centerY = payload.X, payload.Y
		if !s.validatePylonPosition(centerX, centerY, radiusX, radiusY) {
			s.world.PushEvent(event.EventPylonSpawnFailed, nil)
			return
		}
	}

	// Create header entity
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: centerX, Y: centerY})

	// Header is protected from destruction except explicit death
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Pylon component
	s.world.Components.Pylon.SetComponent(headerEntity, component.PylonComponent{
		SpawnX:  centerX,
		SpawnY:  centerY,
		RadiusX: radiusX,
		RadiusY: radiusY,
		MinHP:   minHP,
		MaxHP:   maxHP,
	})

	// Combat component on header (HP=0 for ablative, damage routes to members)
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityPylon,
		HitPoints:        0,
	})

	// Generate disc members
	members := s.createDiscMembers(headerEntity, centerX, centerY, radiusX, radiusY, minHP, maxHP)

	// Header component
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:         component.BehaviorPylon,
		Type:             component.CompositeTypeAblative,
		MemberEntries:    members,
		SkipPositionSync: true,
	})

	// Emit creation events
	s.world.PushEvent(event.EventEnemyCreated, &event.EnemyCreatedPayload{
		Entity:  headerEntity,
		Species: component.SpeciesPylon,
	})

	s.world.PushEvent(event.EventPylonSpawned, &event.PylonSpawnedPayload{
		HeaderEntity: headerEntity,
		MemberCount:  len(members),
		X:            centerX,
		Y:            centerY,
	})
}

// validatePylonPosition checks if pylon bounding rect fits in bounds and is wall-free
func (s *PylonSystem) validatePylonPosition(centerX, centerY, radiusX, radiusY int) bool {
	topLeftX := centerX - radiusX
	topLeftY := centerY - radiusY
	width := 2*radiusX + 1
	height := 2*radiusY + 1

	return s.world.Positions.IsAreaFree(topLeftX, topLeftY, width, height, component.WallBlockSpawn)
}

// findRandomPylonPosition attempts random placement, then spiral, then cursor fallback
func (s *PylonSystem) findRandomPylonPosition(radiusX, radiusY int) (int, int, bool) {
	config := s.world.Resources.Config
	width := 2*radiusX + 1
	height := 2*radiusY + 1

	cursorPos, hasCursor := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)

	// Tier 1: Random attempts
	rng := vmath.NewFastRand(uint64(s.world.Resources.Time.GameTime.UnixNano()))

	// Valid center ranges: [radiusX, MapWidth-radiusX-1] and [radiusY, MapHeight-radiusY-1]
	minCX := radiusX
	maxCX := config.MapWidth - radiusX - 1
	minCY := radiusY
	maxCY := config.MapHeight - radiusY - 1

	if maxCX < minCX || maxCY < minCY {
		// Map too small for this pylon
		return 0, 0, false
	}

	rangeX := maxCX - minCX + 1
	rangeY := maxCY - minCY + 1

	lastCX, lastCY := config.MapWidth/2, config.MapHeight/2

	for attempt := 0; attempt < parameter.PylonSpawnMaxAttempts; attempt++ {
		cx := minCX + rng.Intn(rangeX)
		cy := minCY + rng.Intn(rangeY)
		lastCX, lastCY = cx, cy

		// Cursor exclusion
		if hasCursor {
			dx := cx - cursorPos.X
			dy := cy - cursorPos.Y
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			if dx <= parameter.CursorExclusionX && dy <= parameter.CursorExclusionY {
				continue
			}
		}

		if s.validatePylonPosition(cx, cy, radiusX, radiusY) {
			return cx, cy, true
		}
	}

	// Tier 2: Spiral from last random attempt center
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		lastCX, lastCY,
		width, height,
		radiusX, radiusY, // anchor offset = radius (center to top-left)
		component.WallBlockSpawn,
		parameter.PylonSpawnSpiralMaxRadius,
	)
	if found {
		// Convert top-left back to center
		return topLeftX + radiusX, topLeftY + radiusY, true
	}

	// Tier 3: Last resort — try cursor position
	if hasCursor && s.validatePylonPosition(cursorPos.X, cursorPos.Y, radiusX, radiusY) {
		return cursorPos.X, cursorPos.Y, true
	}

	return 0, 0, false
}

// createDiscMembers generates elliptical disc of member entities with HP falloff
func (s *PylonSystem) createDiscMembers(
	headerEntity core.Entity,
	centerX, centerY, radiusX, radiusY, minHP, maxHP int,
) []component.MemberEntry {
	var members []component.MemberEntry

	// Precompute inverse squared radii for ellipse containment
	rxFixed := vmath.FromInt(radiusX)
	ryFixed := vmath.FromInt(radiusY)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rxFixed, ryFixed)

	hpRange := maxHP - minHP

	for dy := -radiusY; dy <= radiusY; dy++ {
		for dx := -radiusX; dx <= radiusX; dx++ {
			// Ellipse containment check
			dxFixed := vmath.FromInt(dx)
			dyFixed := vmath.FromInt(dy)
			if !vmath.EllipseContains(dxFixed, dyFixed, invRxSq, invRySq) {
				continue
			}

			// Calculate HP based on normalized ellipse distance from center
			// normDistSq: 0 at center, Scale at edge (ellipse boundary)
			var hp int
			if hpRange > 0 {
				normDistSq := vmath.EllipseDistSq(dxFixed, dyFixed, invRxSq, invRySq)
				normDist := vmath.ToFloat(vmath.Sqrt(normDistSq))
				if normDist > 1.0 {
					normDist = 1.0
				}
				hp = maxHP - int(float64(hpRange)*normDist)
			} else {
				hp = maxHP
			}
			if hp < minHP {
				hp = minHP
			}

			memberX := centerX + dx
			memberY := centerY + dy

			memberEntity := s.world.CreateEntity()
			s.world.Positions.SetPosition(memberEntity, component.PositionComponent{
				X: memberX,
				Y: memberY,
			})

			// Protection from game mechanics, not combat
			s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromSpecies,
			})

			// Per-member combat component (ablative HP)
			s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
				OwnerEntity:      headerEntity,
				CombatEntityType: component.CombatEntityPylon,
				HitPoints:        hp,
			})

			// Backlink to header
			s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  memberEntity,
				OffsetX: dx,
				OffsetY: dy,
			})
		}
	}

	return members
}

// processAblativeCombat scans members for HP<=0 and handles death lifecycle
func (s *PylonSystem) processAblativeCombat(headerEntity core.Entity, headerComp *component.HeaderComponent) {
	var deadMembers []core.Entity
	livingCount := 0

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetComponent(member.Entity)
		if !ok {
			continue
		}

		if combatComp.HitPoints <= 0 {
			deadMembers = append(deadMembers, member.Entity)
		} else {
			livingCount++
		}
	}

	// Route deaths through CompositeSystem for proper lifecycle handling
	for _, memberEntity := range deadMembers {
		s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
			HeaderEntity: headerEntity,
			MemberEntity: memberEntity,
		})
	}

	// Self-destruct when no living members remain
	if livingCount == 0 && len(headerComp.MemberEntries) > 0 {
		s.handlePylonDeath(headerEntity)
	}
}

func (s *PylonSystem) handlePylonDeath(headerEntity core.Entity) {
	pylonComp, ok := s.world.Components.Pylon.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Remove component immediately to prevent re-entry from concurrent paths
	s.world.Components.Pylon.RemoveEntity(headerEntity)

	// Emit destroyed event
	s.world.PushEvent(event.EventPylonDestroyed, &event.PylonDestroyedPayload{
		HeaderEntity: headerEntity,
		X:            pylonComp.SpawnX,
		Y:            pylonComp.SpawnY,
	})

	// Emit enemy killed for loot/scoring
	s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
		Entity:  headerEntity,
		Species: component.SpeciesPylon,
		X:       pylonComp.SpawnX,
		Y:       pylonComp.SpawnY,
	})

	// Request composite destruction (header + remaining members)
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

func (s *PylonSystem) terminatePylon(headerEntity core.Entity) {
	// Guard: skip if already removed by handlePylonDeath
	if !s.world.Components.Pylon.HasEntity(headerEntity) {
		// Still request composite cleanup for orphaned headers
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
			HeaderEntity: headerEntity,
			Effect:       0,
		})
		return
	}

	s.world.Components.Pylon.RemoveEntity(headerEntity)
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

func (s *PylonSystem) terminateAll() {
	for _, entity := range s.world.Components.Pylon.GetAllEntities() {
		s.terminatePylon(entity)
	}
}