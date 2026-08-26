package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// QuasarSystem manages quasar boss entity lifecycle
// Quasar is a 3x5 composite that tracks cursor, zaps when cursor exits range
type QuasarSystem struct {
	world *engine.World

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive    *atomic.Bool
	statCount     *atomic.Int64
	statProtected *atomic.Int64
	lifecycle     lifecycleTelemetry
	motion        bounceTelemetry

	enabled bool
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	s := &QuasarSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("quasar.active")
	s.statCount = world.Resources.Status.Ints.Get("quasar.count")
	s.statProtected = world.Resources.Status.Ints.Get("quasar.protected_rejects")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "quasar")
	s.motion = newBounceTelemetry(world.Resources.Status, "quasar")

	s.Init()
	return s
}

func (s *QuasarSystem) Init() {
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statProtected.Store(0)
	s.lifecycle.Reset()
	s.motion.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *QuasarSystem) Name() string {
	return "quasar"
}

func (s *QuasarSystem) Priority() int {
	return parameter.PriorityQuasar
}

func (s *QuasarSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventQuasarSpawnRequest,
		event.EventQuasarCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesQuasar {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventQuasarSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventQuasarSpawnRequest:
		if payload, ok := ev.Payload.(*event.QuasarSpawnRequestPayload); ok {
			s.spawnQuasar(payload.X, payload.Y)
		}

	case event.EventQuasarCancelRequest:
		// Cancel all quasars
		entities := s.world.Components.Quasar.Entities()
		s.lifecycle.despawned.Add(int64(len(entities)))
		for _, entity := range entities {
			s.terminateQuasar(entity)
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if s.world.Components.Quasar.HasEntity(payload.HeaderEntity) {
				s.lifecycle.despawned.Add(1)
				s.terminateQuasar(payload.HeaderEntity)
			}
		}
	}
}

func (s *QuasarSystem) Update() {
	if !s.enabled {
		return
	}

	quasars := s.world.Components.Quasar
	quasarEntities := quasars.Entities()
	activeCount := 0

	// Zap range calculations, dynamic resize based on map
	width := s.world.Resources.Config.MapWidth
	height := s.world.Resources.Config.MapHeight
	currentRadius := float64(max(width/2, height))

	dt := s.world.Resources.Time.DeltaTime

	for _, headerEntity := range quasarEntities {
		// Conditional state transitions have distinct commit points; keep the
		// component detached so death/stun exits preserve their prior semantics.
		quasarComp, ok := quasars.GetComponent(headerEntity)
		if !ok {
			s.lifecycle.despawned.Add(1)
			s.terminateQuasar(headerEntity)
			continue
		}

		if quasarComp.ZapRadius != currentRadius {
			quasarComp.ZapRadius = currentRadius
		}

		// Combat sync
		combatComp, ok := s.world.Components.Combat.GetPtr(headerEntity)
		if !ok {
			continue
		}

		// Hitpoint check
		if combatComp.HitPoints <= 0 {
			killX, killY := -1, -1
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				killX, killY = headerPos.X, headerPos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:       headerEntity,
				KillerEntity: combatComp.LastDamagedBy,
				Species:      component.SpeciesQuasar,
				X:            killX,
				Y:            killY,
			})
			s.terminateQuasar(headerEntity)
			continue
		}

		// Stun check: skip movement, reset charging state
		if combatComp.StunnedRemaining > 0 {
			if quasarComp.IsCharging {
				quasarComp.IsCharging = false
				quasarComp.ChargeRemaining = 0
				quasars.SetComponent(headerEntity, quasarComp)

				s.world.PushEvent(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{
					AnchorEntity: headerEntity,
				})
			}
			// Note: IsZapping + IsShielded prevents stun, so no zap handling needed, until unshielded zap is implemented
			activeCount++
			continue
		}

		// Check if cursor is within zap range
		cursorInRange := s.isCursorInZapRange(headerEntity, &quasarComp)

		// State machine: InRange ←→ Charging → Zapping
		if cursorInRange {
			// Cursor in range: cancel any active state, return to homing
			if quasarComp.IsZapping {
				s.stopZapping(headerEntity, &quasarComp)
			}
			// Cancel charging when cursor re-enters range
			if quasarComp.IsCharging {
				quasarComp.IsCharging = false
				quasarComp.ChargeRemaining = 0
				quasars.SetComponent(headerEntity, quasarComp)

				s.world.PushEvent(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{
					AnchorEntity: headerEntity,
				})
			}

			s.updateKineticMovement(headerEntity, &quasarComp)
			quasars.SetComponent(headerEntity, quasarComp)

		} else if quasarComp.IsZapping {
			// Already zapping: continue zap, update target
			cursor := s.updateZapTarget(headerEntity)
			s.applyZapDamage(cursor)
			quasars.SetComponent(headerEntity, quasarComp)

		} else if quasarComp.IsCharging {
			// Charging: decrement timer, check completion
			quasarComp.ChargeRemaining -= dt

			if quasarComp.ChargeRemaining <= 0 {
				s.startZapping(headerEntity, &quasarComp)
			} else {
				// Continue homing during charge
				s.updateKineticMovement(headerEntity, &quasarComp)
				quasars.SetComponent(headerEntity, quasarComp)
			}

		} else {
			// Cursor out of range, not charging, not zapping: start charging
			s.startCharging(headerEntity, &quasarComp)
		}

		// Shield and cursor interaction (all states)
		s.handleInteractions(headerEntity)

		// Combat update: enraged state blocks kinetic via combat system
		isActiveState := quasarComp.IsCharging || quasarComp.IsZapping
		combatComp.IsEnraged = isActiveState

		// Damage immunity requires explicit refresh (not handled by IsEnraged)
		if quasarComp.IsShielded {
			combatComp.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
		}

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

func (s *QuasarSystem) spawnQuasar(targetX, targetY int) {
	// Trust fuse-validated position, cheap verification only
	headerX, headerY := targetX, targetY
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	// O(15) wall overlap check - fuse already validated, this catches edge cases
	if s.world.Positions.HasBlockingWallInArea(
		topLeftX, topLeftY,
		parameter.QuasarWidth, parameter.QuasarHeight,
		component.WallBlockSpawn,
	) {
		// Rare: wall appeared during animation, fallback to spiral
		var found bool
		topLeftX, topLeftY, found = s.world.Positions.FindFreeAreaSpiral(
			headerX, headerY,
			parameter.QuasarWidth, parameter.QuasarHeight,
			parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
			component.WallBlockSpawn,
			0,
		)
		if !found {
			s.lifecycle.spawnFailures.Add(1)
			return
		}
		headerX = topLeftX + parameter.QuasarHeaderOffsetX
		headerY = topLeftY + parameter.QuasarHeaderOffsetY
	}

	// Clear area and create composite
	s.clearQuasarSpawnArea(headerX, headerY)
	s.createQuasarComposite(headerX, headerY)
	s.lifecycle.spawned.Add(1)
}

// clearQuasarSpawnArea destroys all entities within the quasar footprint
func (s *QuasarSystem) clearQuasarSpawnArea(headerX, headerY int) {
	// Calculate top-left from header position
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	var toDestroy []core.Entity
	var entities [parameter.MaxEntitiesPerCell]core.Entity

	for row := range parameter.QuasarHeight {
		for col := range parameter.QuasarWidth {
			x := topLeftX + col
			y := topLeftY + row

			// TODO(phase4.2b): shared footprint only. Player entities under a new
			// shared species must be evicted by a player-domain consumer.
			count := s.world.Positions.GetEntitiesAtInto(x, y, engine.ScopeShared, entities[:])
			for i := range count {
				e := entities[i]
				if e == 0 || s.world.Components.Cursor.HasEntity(e) {
					continue
				}
				// Skip walls - they block, not get cleared
				if s.world.Components.Wall.HasEntity(e) {
					continue
				}
				// Check protection
				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask&component.ProtectFromSpecies != 0 {
						s.statProtected.Add(1)
						continue
					}
				}
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, toDestroy...)
	}
}

// createQuasarComposite builds the 3x5 quasar entity structure
func (s *QuasarSystem) createQuasarComposite(headerX, headerY int) core.Entity {
	// Calculate top-left from header position
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	// Create phantom head (controller entity)
	headerEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	// Phantom head is indestructible through lifecycle
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Set quasar components
	s.world.Components.Quasar.SetComponent(headerEntity, component.QuasarComponent{
		SpeedMultiplier: 1.0,
	})

	// Add ShieldComponent (inactive by default), uses pre-calculated config
	cfg := &visual.ShieldConfigs[component.ShieldTypeQuasar]
	s.world.Components.Shield.SetComponent(headerEntity, component.ShieldComponent{
		Active:  false,
		Type:    component.ShieldTypeQuasar,
		RadiusX: cfg.RadiusX,
		RadiusY: cfg.RadiusY,
		InvRxSq: cfg.InvRxSq,
		InvRySq: cfg.InvRySq,
	})

	// Set combat component
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityQuasar,
		HitPoints:        parameter.CombatInitialHPQuasar,
	})

	// Set kinetic component with centered position
	preciseX, preciseY := vmath.Point{X: headerX, Y: headerY}.CenterF()
	kinetic := physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{Kinetic: kinetic})

	// Navigation component for flow field guidance around obstacles
	s.world.Components.Navigation.SetComponent(headerEntity, component.NavigationComponent{
		Width:         parameter.QuasarWidth,
		Height:        parameter.QuasarHeight,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	})

	// Build member entities
	members := make([]component.MemberEntry, 0, parameter.QuasarWidth*parameter.QuasarHeight)

	for row := range parameter.QuasarHeight {
		for col := range parameter.QuasarWidth {
			memberX := topLeftX + col
			memberY := topLeftY + row

			// Calculate offset from header
			offsetX := col - parameter.QuasarHeaderOffsetX
			offsetY := row - parameter.QuasarHeaderOffsetY

			entity := s.world.CreateEntity(core.DomainShared)
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			// MemberEntries are not from death, composite system manages lifecycle
			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromSpecies,
			})

			// Backlink to header
			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: offsetX,
				OffsetY: offsetY,
			})
		}
	}

	// Set composite header on phantom head
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorQuasar,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
	})

	// Announce the fully initialized species instance.
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      headerEntity,
		Species:     component.SpeciesQuasar,
		X:           headerX,
		Y:           headerY,
		MemberCount: len(members),
	})

	return headerEntity
}

// startCharging initiates the charge phase before zapping
func (s *QuasarSystem) startCharging(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	quasarComp.IsCharging = true
	quasarComp.ChargeRemaining = parameter.QuasarChargeDuration
	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)

	s.world.PushEvent(event.EventSplashTimerRequest, &event.SplashTimerRequestPayload{
		AnchorEntity: headerEntity,
		Color:        visual.RgbSplashCyan,
		MarginRight:  parameter.QuasarHeaderOffsetX + 1, // Accounting for anchor column
		MarginLeft:   parameter.QuasarHeaderOffsetX,
		MarginTop:    parameter.QuasarHeaderOffsetY,
		MarginBottom: parameter.QuasarHeaderOffsetY + 1, // Accounting for anchor row
		Duration:     parameter.QuasarChargeDuration,
	})
}

// updateKineticMovement handles continuous kinetic quasar movement toward cursor
func (s *QuasarSystem) updateKineticMovement(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	config := s.world.Resources.Config
	now := s.world.Resources.Time.GameTime

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
	if !ok {
		return
	}

	// Cap delta to prevent tunneling
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	// Periodic speed scaling with cap (game logic, not physics)
	speedIncreaseInterval := time.Duration(parameter.QuasarSpeedIncreaseTicks) * parameter.GameUpdateInterval
	if now.Sub(quasarComp.LastSpeedIncreaseAt) >= speedIncreaseInterval {
		quasarComp.SpeedMultiplier = min(
			quasarComp.SpeedMultiplier*(1.0+parameter.QuasarSpeedIncreasePercent),
			parameter.QuasarSpeedMultiplierMax)
		quasarComp.LastSpeedIncreaseAt = now
	}

	// Group-based target resolution + navigation routing
	// (direct path vs flow field vs stuck fallback)
	targetX, targetY, usingDirectPath := ResolveMovementTarget(s.world, headerEntity, kineticComp)

	// Cornering drag
	turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
		+parameter.NavCorneringThreshold, 1.0)

	// Homing with arrival steering
	// Disable homing drag when navigating via flow field - cornering drag handles turns
	settled := physics.ApplyHomingScaled(
		&kineticComp.Kinetic,
		targetX, targetY,
		&profile.QuasarHoming,
		quasarComp.SpeedMultiplier,
		dtSec,
		usingDirectPath, // Only apply homing drag on direct path
	)

	// Apply cornering drag
	if turnSeverity > 0 {
		physics.ApplyLinearDrag(&kineticComp.Kinetic,
			turnSeverity*parameter.NavCorneringBrake, dtSec)
	}

	if settled {
		// Snap to exact target center
		baseX, baseY, baseOK := resolveBaseTarget(s.world, headerEntity)
		if baseOK {
			kineticComp.PreciseX, kineticComp.PreciseY = vmath.Point{X: baseX, Y: baseY}.CenterF()
			kineticComp.VelX = 0
			kineticComp.VelY = 0
			// Sync grid position if snap crossed cell boundary
			if headerPos.X != baseX || headerPos.Y != baseY {
				s.processCollisionsAtNewPositions(headerEntity, baseX, baseY)
				s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: baseX, Y: baseY})
			}
		}
		return
	}

	// Cap velocity before integration to prevent runaway from cumulative dust hits
	kineticComp.VelX, kineticComp.VelY = physics.CapSpeed(kineticComp.VelX, kineticComp.VelY, parameter.QuasarMaxSpeed)

	// Wall query to capture the specific mask and dimensions
	wallCheck := func(topLeftX, topLeftY int) bool {
		return s.world.Positions.HasBlockingWallInArea(
			topLeftX, topLeftY,
			parameter.QuasarWidth, parameter.QuasarHeight,
			component.WallBlockKinetic,
		)
	}

	// Calculate Valid Header Bounds (Header must stay within these grid coordinates)
	// Min: OffsetX
	// Max: Width - (QuasarWidth - OffsetX)
	minHeaderX := parameter.QuasarHeaderOffsetX
	maxHeaderX := config.MapWidth - (parameter.QuasarWidth - parameter.QuasarHeaderOffsetX)
	minHeaderY := parameter.QuasarHeaderOffsetY
	maxHeaderY := config.MapHeight - (parameter.QuasarHeight - parameter.QuasarHeaderOffsetY)

	// Integrate with Bounce
	newX, newY, motion := physics.IntegrateWithBounceStats(
		&kineticComp.Kinetic,
		dtSec,
		parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.QuasarRestitution,
		wallCheck,
	)
	s.motion.Record(motion)

	// Update header position if cell changed
	if newX != headerPos.X || newY != headerPos.Y {
		s.processCollisionsAtNewPositions(headerEntity, newX, newY)
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
	}

}

// isCursorInZapRange checks if cursor is within zap ellipse centered on quasar
func (s *QuasarSystem) isCursorInZapRange(headerEntity core.Entity, quasarComp *component.QuasarComponent) bool {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return true // Failsafe: don't zap if can't determine
	}

	_, cursorX, cursorY, ok := ClosestCursor(s.world, headerPos.X, headerPos.Y)
	if !ok {
		return true
	}

	// Inside visual circle = in range (no zap)
	dx := float64(cursorX - headerPos.X)
	dyCirc := vmath.ScaleToCircularF(float64(cursorY - headerPos.Y)) // Aspect correction
	dist := vmath.MagnitudeF(dx, dyCirc)
	return dist <= quasarComp.ZapRadius
}

// startZapping spawns tracked lightning aimed at the nearest cursor.
func (s *QuasarSystem) startZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}
	_, cursorX, cursorY, ok := ClosestCursor(s.world, headerPos.X, headerPos.Y)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:        headerEntity,
		OriginX:      headerPos.X,
		OriginY:      headerPos.Y,
		TargetX:      cursorX,
		TargetY:      cursorY,
		OriginEntity: headerEntity,
		ColorType:    component.LightningCyan,
		Duration:     parameter.QuasarZapDuration,
		Tracked:      true,
	})

	quasarComp.ChargeRemaining = 0
	quasarComp.IsCharging = false
	quasarComp.IsZapping = true
	quasarComp.IsShielded = true

	// Activate visual shield component
	if shield, ok := s.world.Components.Shield.GetPtr(headerEntity); ok {
		shield.Active = true
	}
	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// stopZapping despawns lightning
func (s *QuasarSystem) stopZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	s.world.PushEvent(event.EventLightningDespawnRequest, &event.LightningDespawnRequestPayload{Owner: headerEntity})

	quasarComp.IsZapping = false
	quasarComp.IsShielded = false

	if shield, ok := s.world.Components.Shield.GetPtr(headerEntity); ok {
		shield.Active = false
	}
	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// updateZapTarget tracks the nearest cursor and returns it for damage attribution.
func (s *QuasarSystem) updateZapTarget(headerEntity core.Entity) core.Entity {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return 0
	}
	cursorEntity, cursorX, cursorY, ok := ClosestCursor(s.world, headerPos.X, headerPos.Y)
	if !ok {
		return 0
	}

	s.world.PushEvent(event.EventLightningUpdateRequest, &event.LightningUpdateRequestPayload{
		Owner:   headerEntity,
		TargetX: cursorX,
		TargetY: cursorY,
	})
	return cursorEntity
}

// applyZapDamage applies zap damage
func (s *QuasarSystem) applyZapDamage(cursorEntity core.Entity) {
	if cursorEntity == 0 {
		return
	}
	shield, ok := s.world.Components.Shield.GetPtr(cursorEntity)
	shieldActive := ok && shield.Active

	if shieldActive {
		// Drain energy through shield
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Entity: cursorEntity,
			Value:  parameter.QuasarShieldDrain,
		})
	} else {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Entity: cursorEntity,
			Delta:  -parameter.QuasarDamageHeat,
		})
	}
}

// processCollisionsAtNewPositions destroys entities at quasar's destination
func (s *QuasarSystem) processCollisionsAtNewPositions(headerEntity core.Entity, headerX, headerY int) {
	header, ok := s.world.Components.Header.GetPtr(headerEntity)
	if !ok {
		return
	}

	// Build set of member entity IDs for exclusion
	memberSet := make(map[core.Entity]bool, len(header.MemberEntries)+1)
	memberSet[headerEntity] = true
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			memberSet[m.Entity] = true
		}
	}

	var toDestroy []core.Entity
	var entities [parameter.MaxEntitiesPerCell]core.Entity

	// Check each cell the quasar will occupy
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	for row := range parameter.QuasarHeight {
		for col := range parameter.QuasarWidth {
			x := topLeftX + col
			y := topLeftY + row

			// TODO(phase4.2b): shared occupants only; player eviction is pending
			count := s.world.Positions.GetEntitiesAtInto(x, y, engine.ScopeShared, entities[:])
			for i := range count {
				entity := entities[i]
				if entity == 0 || s.world.Components.Cursor.HasEntity(entity) || memberSet[entity] {
					continue
				}

				// Check protection
				if protComp, ok := s.world.Components.Protection.GetPtr(entity); ok {
					if protComp.Mask&component.ProtectFromSpecies != 0 {
						s.statProtected.Add(1)
						continue
					}
				}

				// Handle nugget collision
				if s.world.Components.Nugget.HasEntity(entity) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: entity,
					})
				}

				toDestroy = append(toDestroy, entity)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, toDestroy...)
	}
}

// handleInteractions processes shield drain and cursor collision
func (s *QuasarSystem) handleInteractions(headerEntity core.Entity) {
	overlaps := CheckCursorOverlaps(s.world, headerEntity)
	for i := range overlaps.Count {
		overlap := &overlaps.Entries[i]
		// Apply shield knockback before exact cursor contact.
		if len(overlap.ShieldMembers) > 0 {
			s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
				AttackType:   component.CombatAttackShield,
				OwnerEntity:  overlap.Cursor,
				OriginEntity: overlap.Cursor,
				TargetEntity: headerEntity,
				HitEntities:  overlap.ShieldMembers,
			})
			s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Entity: overlap.Cursor,
				Value:  parameter.QuasarShieldDrain,
			})
		} else if overlap.OnCursor && !overlap.ShieldActive {
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: overlap.Cursor,
				Delta:  -parameter.QuasarDamageHeat,
			})
		}
	}
}

// terminateQuasar ends a specific quasar
func (s *QuasarSystem) terminateQuasar(headerEntity core.Entity) {
	if headerEntity == 0 {
		return
	}

	if !s.world.Components.Quasar.HasEntity(headerEntity) {
		return
	}

	// Stop zapping or tracked lightning lingers after quasar death
	s.world.PushEvent(event.EventLightningDespawnRequest, &event.LightningDespawnRequestPayload{
		Owner:        headerEntity,
		TargetEntity: 0, // 0 = all lightning from this owner
	})

	// Delegate composite destruction to CompositeSystem
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}
