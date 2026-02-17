package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// quasarCacheEntry holds cached quasar data for soft collision
type quasarCacheEntry struct {
	entity core.Entity
	x, y   int // Grid position of header
}

// QuasarSystem manages quasar boss entity lifecycle
// Quasar is a 3x5 composite that tracks cursor, zaps when cursor exits range
type QuasarSystem struct {
	world *engine.World

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Per-tick cache for soft collision
	swarmCache  []swarmCacheEntry
	quasarCache []quasarCacheEntry

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	s := &QuasarSystem{
		world: world,
	}

	s.swarmCache = make([]swarmCacheEntry, 0, 10)
	s.quasarCache = make([]quasarCacheEntry, 0, 4)

	s.statActive = world.Resources.Status.Bools.Get("quasar.active")
	s.statCount = world.Resources.Status.Ints.Get("quasar.count")

	s.Init()
	return s
}

func (s *QuasarSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	clear(s.swarmCache)
	clear(s.quasarCache)
	s.statActive.Store(false)
	s.statCount.Store(0)
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
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventQuasarSpawnRequest:
		if payload, ok := ev.Payload.(*event.QuasarSpawnRequestPayload); ok {
			s.spawnQuasar(payload.SpawnX, payload.SpawnY)
		}

	case event.EventQuasarCancelRequest:
		// Cancel all quasars
		for _, entity := range s.world.Components.Quasar.GetAllEntities() {
			s.terminateQuasar(entity)
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if s.world.Components.Quasar.HasEntity(payload.HeaderEntity) {
				s.terminateQuasar(payload.HeaderEntity)
			}
		}
	}
}

func (s *QuasarSystem) Update() {
	if !s.enabled {
		return
	}

	// Cache combat entities for soft collision
	s.cacheCombatEntities()

	quasarEntities := s.world.Components.Quasar.GetAllEntities()
	activeCount := 0

	// Zap range calculations, dynamic resize based on map
	width := s.world.Resources.Config.MapWidth
	height := s.world.Resources.Config.MapHeight
	currentRadius := vmath.FromInt(max(width/2, height))

	for _, headerEntity := range quasarEntities {
		// Verify composite still exists
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			s.terminateQuasar(headerEntity)
			continue
		}

		quasarComp, ok := s.world.Components.Quasar.GetComponent(headerEntity)
		if !ok {
			s.terminateQuasar(headerEntity)
			continue
		}

		if quasarComp.ZapRadius != currentRadius {
			quasarComp.ZapRadius = currentRadius
		}

		// Combat sync
		combatComp, ok := s.world.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Hitpoint check
		if combatComp.HitPoints <= 0 {
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
					Entity:  headerEntity,
					Species: component.SpeciesQuasar,
					X:       headerPos.X,
					Y:       headerPos.Y,
				})
			}
			s.terminateQuasar(headerEntity)
			continue
		}

		// Stun check: skip movement, reset charging state
		if combatComp.StunnedRemaining > 0 {
			if quasarComp.IsCharging {
				quasarComp.IsCharging = false
				quasarComp.ChargeRemaining = 0
				s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)

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
				s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)

				s.world.PushEvent(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{
					AnchorEntity: headerEntity,
				})
			}

			s.updateKineticMovement(headerEntity, &quasarComp)
			s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)

		} else if quasarComp.IsZapping {
			// Already zapping: continue zap, update target
			s.updateZapTarget(headerEntity)
			s.applyZapDamage()
			s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)

		} else if quasarComp.IsCharging {
			// Charging: decrement timer, check completion
			quasarComp.ChargeRemaining -= s.world.Resources.Time.DeltaTime

			if quasarComp.ChargeRemaining <= 0 {
				s.startZapping(headerEntity, &quasarComp)
			} else {
				// Continue homing during charge
				s.updateKineticMovement(headerEntity, &quasarComp)
				s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)
			}

		} else {
			// Cursor out of range, not charging, not zapping: start charging
			s.startCharging(headerEntity, &quasarComp)
		}

		// Shield and cursor interaction (all states)
		s.handleInteractions(headerEntity, &headerComp)

		// Combat update: enraged state blocks kinetic via combat system
		isActiveState := quasarComp.IsCharging || quasarComp.IsZapping
		combatComp.IsEnraged = isActiveState

		// Damage immunity requires explicit refresh (not handled by IsEnraged)
		if quasarComp.IsShielded {
			combatComp.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
		}

		s.world.Components.Combat.SetComponent(headerEntity, combatComp)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// cacheCombatEntities populates cache for soft collision detection
func (s *QuasarSystem) cacheCombatEntities() {
	s.swarmCache = s.swarmCache[:0]
	s.quasarCache = s.quasarCache[:0]

	// Cache swarms
	swarmEntities := s.world.Components.Swarm.GetAllEntities()
	for _, entity := range swarmEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}
		s.swarmCache = append(s.swarmCache, swarmCacheEntry{
			entity: entity,
			x:      pos.X,
			y:      pos.Y,
		})
	}

	// Cache quasars
	quasarEntities := s.world.Components.Quasar.GetAllEntities()
	for _, entity := range quasarEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}
		s.quasarCache = append(s.quasarCache, quasarCacheEntry{
			entity: entity,
			x:      pos.X,
			y:      pos.Y,
		})
	}
}

func (s *QuasarSystem) spawnQuasar(targetX, targetY int) {
	// Find valid spawn position via spiral search
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		targetX, targetY,
		parameter.QuasarWidth, parameter.QuasarHeight,
		parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		return // No valid position, abort
	}

	// Header position from found top-left
	headerX := topLeftX + parameter.QuasarHeaderOffsetX
	headerY := topLeftY + parameter.QuasarHeaderOffsetY

	// Clear area and create composite
	s.clearQuasarSpawnArea(headerX, headerY)
	headerEntity := s.createQuasarComposite(headerX, headerY)

	s.world.PushEvent(event.EventQuasarSpawned, &event.QuasarSpawnedPayload{
		HeaderEntity: headerEntity,
	})
}

// clearQuasarSpawnArea destroys all entities within the quasar footprint
func (s *QuasarSystem) clearQuasarSpawnArea(headerX, headerY int) {
	// Calculate top-left from header position
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	cursorEntity := s.world.Resources.Player.Entity
	var toDestroy []core.Entity

	for row := 0; row < parameter.QuasarHeight; row++ {
		for col := 0; col < parameter.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
				// Skip walls - they block, not get cleared
				if s.world.Components.Wall.HasEntity(e) {
					continue
				}
				// Check protection
				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask&component.ProtectFromSpecies != 0 {
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
func (s *QuasarSystem) createQuasarComposite(headerX, headerY int) core.Entity {
	// Calculate top-left from header position
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	// Create phantom head (controller entity)
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	// Phantom head is indestructible through lifecycle
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Set quasar components
	s.world.Components.Quasar.SetComponent(headerEntity, component.QuasarComponent{
		SpeedMultiplier: vmath.Scale,
	})

	// Add ShieldComponent (inactive by default), uses pre-calculated config
	cfg := visual.QuasarShieldConfig
	s.world.Components.Shield.SetComponent(headerEntity, component.ShieldComponent{
		Active:        false,
		Color:         cfg.Color,
		Palette256:    cfg.Palette256,
		GlowColor:     cfg.GlowColor,
		GlowIntensity: cfg.GlowIntensity,
		GlowPeriod:    cfg.GlowPeriod,
		MaxOpacity:    cfg.MaxOpacity,
		RadiusX:       cfg.RadiusX,
		RadiusY:       cfg.RadiusY,
		InvRxSq:       cfg.InvRxSq,
		InvRySq:       cfg.InvRySq,
	})

	// Set combat component
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityQuasar,
		HitPoints:        parameter.CombatInitialHPQuasar,
	})

	// Set kinetic component with centered position
	preciseX, preciseY := vmath.CenteredFromGrid(headerX, headerY)
	kinetic := core.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{kinetic})

	// Build member entities
	members := make([]component.MemberEntry, 0, parameter.QuasarWidth*parameter.QuasarHeight)

	for row := 0; row < parameter.QuasarHeight; row++ {
		for col := 0; col < parameter.QuasarWidth; col++ {
			memberX := topLeftX + col
			memberY := topLeftY + row

			// Calculate offset from header
			offsetX := col - parameter.QuasarHeaderOffsetX
			offsetY := row - parameter.QuasarHeaderOffsetY

			entity := s.world.CreateEntity()
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			// MemberEntries are not from death, composite system manages lifecycle
			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromSpecies,
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

	// Emit quasar creation
	s.world.PushEvent(event.EventEnemyCreated, &event.EnemyCreatedPayload{
		Entity:  headerEntity,
		Species: component.SpeciesQuasar,
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
	cursorEntity := s.world.Resources.Player.Entity
	now := s.world.Resources.Time.GameTime

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	// Cap delta to prevent tunneling
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Periodic speed scaling with cap (game logic, not physics)
	speedIncreaseInterval := time.Duration(parameter.QuasarSpeedIncreaseTicks) * parameter.GameUpdateInterval
	if now.Sub(quasarComp.LastSpeedIncreaseAt) >= speedIncreaseInterval {
		newMultiplier := vmath.Mul(quasarComp.SpeedMultiplier, parameter.QuasarSpeedIncreasePercent)
		if newMultiplier > parameter.QuasarSpeedMultiplierMaxFixed {
			newMultiplier = parameter.QuasarSpeedMultiplierMaxFixed
		}
		quasarComp.SpeedMultiplier = newMultiplier
		quasarComp.LastSpeedIncreaseAt = now
	}

	// Target cursor center
	cursorXFixed, cursorYFixed := vmath.CenteredFromGrid(cursorPos.X, cursorPos.Y)

	// Homing with arrival steering, drag only when not immune
	settled := physics.ApplyHomingScaled(
		&kineticComp.Kinetic,
		cursorXFixed, cursorYFixed,
		&physics.QuasarHoming,
		quasarComp.SpeedMultiplier,
		dtFixed,
		true, // Always apply drag regardless of kinetic immunity
	)

	if settled {
		// Snap to exact cursor center
		kineticComp.PreciseX = cursorXFixed
		kineticComp.PreciseY = cursorYFixed
		kineticComp.VelX = 0
		kineticComp.VelY = 0
		// Sync grid position if snap crossed cell boundary
		if headerPos.X != cursorPos.X || headerPos.Y != cursorPos.Y {
			s.processCollisionsAtNewPositions(headerEntity, cursorPos.X, cursorPos.Y)
			s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: cursorPos.X, Y: cursorPos.Y})
		}
		s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
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
	newX, newY, _ := physics.IntegrateWithBounce(
		&kineticComp.Kinetic,
		dtFixed,
		parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.QuasarRestitution,
		wallCheck,
	)

	// Soft collision with other species
	combatComp, hasCombat := s.world.Components.Combat.GetComponent(headerEntity)
	if hasCombat {
		s.applySoftCollisions(headerEntity, &kineticComp, &combatComp, newX, newY)
		s.world.Components.Combat.SetComponent(headerEntity, combatComp)
	}

	// Update header position if cell changed
	if newX != headerPos.X || newY != headerPos.Y {
		s.processCollisionsAtNewPositions(headerEntity, newX, newY)
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
	}

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// applySoftCollisions checks quasar overlap with swarms and other quasars
func (s *QuasarSystem) applySoftCollisions(
	headerEntity core.Entity,
	kineticComp *component.KineticComponent,
	combatComp *component.CombatComponent,
	headerX, headerY int,
) {
	// Check collision with swarms
	for _, sc := range s.swarmCache {
		radialX, radialY, hit := physics.CheckSoftCollision(
			headerX, headerY, sc.x, sc.y,
			parameter.SwarmCollisionInvRxSq, parameter.SwarmCollisionInvRySq,
		)

		if hit {
			physics.ApplyCollision(
				&kineticComp.Kinetic,
				radialX, radialY,
				&physics.SoftCollisionQuasarToSwarm,
				s.rng,
			)
			combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration
			return
		}
	}

	// Check collision with other quasars
	for _, qc := range s.quasarCache {
		if qc.entity == headerEntity {
			continue
		}

		radialX, radialY, hit := physics.CheckSoftCollision(
			headerX, headerY, qc.x, qc.y,
			parameter.QuasarCollisionInvRxSq, parameter.QuasarCollisionInvRySq,
		)

		if hit {
			physics.ApplyCollision(
				&kineticComp.Kinetic,
				radialX, radialY,
				&physics.SoftCollisionQuasarToQuasar,
				s.rng,
			)
			combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration
			return
		}
	}
}

// isCursorInZapRange checks if cursor is within zap ellipse centered on quasar
func (s *QuasarSystem) isCursorInZapRange(headerEntity core.Entity, quasarComp *component.QuasarComponent) bool {
	cursorEntity := s.world.Resources.Player.Entity

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return true // Failsafe: don't zap if can't determine
	}

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return true
	}

	dx := vmath.FromInt(cursorPos.X - headerPos.X)
	dy := vmath.FromInt(cursorPos.Y - headerPos.Y)

	// Inside visual circle = in range (no zap)
	dyCirc := vmath.ScaleToCircular(dy) // Aspect correction: dy * 2
	dist := vmath.Magnitude(dx, dyCirc)
	return dist <= quasarComp.ZapRadius
}

// Start zapping - spawn tracked lightning
func (s *QuasarSystem) startZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	cursorEntity := s.world.Resources.Player.Entity

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:        headerEntity,
		OriginX:      headerPos.X,
		OriginY:      headerPos.Y,
		TargetX:      cursorPos.X,
		TargetY:      cursorPos.Y,
		OriginEntity: headerEntity,
		TargetEntity: cursorEntity,
		ColorType:    component.LightningCyan,
		Duration:     parameter.QuasarZapDuration,
		Tracked:      true,
	})

	quasarComp.ChargeRemaining = 0
	quasarComp.IsCharging = false
	quasarComp.IsZapping = true
	quasarComp.IsShielded = true

	// Activate visual shield component
	if shield, ok := s.world.Components.Shield.GetComponent(headerEntity); ok {
		shield.Active = true
		s.world.Components.Shield.SetComponent(headerEntity, shield)
	}

	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// stopZapping despawns lightning
func (s *QuasarSystem) stopZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	s.world.PushEvent(event.EventLightningDespawnRequest, &event.LightningDespawnPayload{Owner: headerEntity})

	quasarComp.IsZapping = false
	quasarComp.IsShielded = false

	if shield, ok := s.world.Components.Shield.GetComponent(headerEntity); ok {
		shield.Active = false
		s.world.Components.Shield.SetComponent(headerEntity, shield)
	}

	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// updateZapTarget lightning target to track cursor
func (s *QuasarSystem) updateZapTarget(headerEntity core.Entity) {
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventLightningUpdate, &event.LightningUpdatePayload{
		Owner:   headerEntity,
		TargetX: cursorPos.X,
		TargetY: cursorPos.Y,
	})
}

// applyZapDamage applies zap damage
func (s *QuasarSystem) applyZapDamage() {
	cursorEntity := s.world.Resources.Player.Entity

	shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shield.Active

	if shieldActive {
		// Drain energy through shield
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: parameter.QuasarShieldDrain,
		})
	} else {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -parameter.QuasarDamageHeat})
	}
}

// processCollisionsAtNewPositions destroys entities at quasar's destination
func (s *QuasarSystem) processCollisionsAtNewPositions(headerEntity core.Entity, headerX, headerY int) {
	cursorEntity := s.world.Resources.Player.Entity

	header, ok := s.world.Components.Header.GetComponent(headerEntity)
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

	// Check each cell the quasar will occupy
	topLeftX := headerX - parameter.QuasarHeaderOffsetX
	topLeftY := headerY - parameter.QuasarHeaderOffsetY

	for row := 0; row < parameter.QuasarHeight; row++ {
		for col := 0; col < parameter.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, entity := range entities {
				if entity == 0 || entity == cursorEntity || memberSet[entity] {
					continue
				}

				// Check protection
				if protComp, ok := s.world.Components.Protection.GetComponent(entity); ok {
					if protComp.Mask == component.ProtectAll || protComp.Mask.Has(component.ProtectFromSpecies) {
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
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, toDestroy)
	}
}

// handleInteractions processes shield drain and cursor collision
func (s *QuasarSystem) handleInteractions(headerEntity core.Entity, headerComp *component.HeaderComponent) {
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shieldComp.Active

	anyOnCursor := false

	// Stack-allocated buffer for shield overlapping member offsets (max 15 cells in 3x5 quasar)
	var hitEntities []core.Entity

	for _, memberEntry := range headerComp.MemberEntries {
		if memberEntry.Entity == 0 {
			continue
		}

		memberPos, ok := s.world.Positions.GetPosition(memberEntry.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
			hitEntities = append(hitEntities, memberEntry.Entity)
		}
	}
	anyInShield := len(hitEntities) > 0

	// Shield knockback check
	if anyInShield {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackShield,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: headerEntity,
			HitEntities:  hitEntities,
		})
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: parameter.QuasarShieldDrain,
		})
	} else if anyOnCursor && !shieldActive {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -parameter.QuasarDamageHeat})
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
	s.world.PushEvent(event.EventLightningDespawnRequest, &event.LightningDespawnPayload{
		Owner:        headerEntity,
		TargetEntity: 0, // 0 = all lightning from this owner
	})

	// Delegate composite destruction to CompositeSystem
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})

	s.world.PushEvent(event.EventQuasarDestroyed, nil)
}