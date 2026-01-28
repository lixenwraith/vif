package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// QuasarSystem manages the quasar boss entity lifecycle
// Quasar is a 3x5 composite that tracks cursor at 2x drain speed
// Drains 1000 energy/tick when any part overlaps shield
// Resets heat to 0 on direct cursor collision without shield
type QuasarSystem struct {
	world *engine.World

	// Runtime state
	headerEntity core.Entity

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive    *atomic.Bool
	statHitPoints *atomic.Int64

	enabled bool
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	s := &QuasarSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("quasar.active")
	s.statHitPoints = world.Resources.Status.Ints.Get("quasar.hit_points")

	s.Init()
	return s
}

func (s *QuasarSystem) Init() {
	s.headerEntity = 0
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statHitPoints.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *QuasarSystem) Name() string {
	return "quasar"
}

func (s *QuasarSystem) Priority() int {
	return constant.PriorityQuasar
}

func (s *QuasarSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventQuasarSpawnRequest,
		event.EventQuasarCancelRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		if s.headerEntity != 0 {
			s.terminateQuasar()
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
	case event.EventQuasarSpawnRequest:
		if payload, ok := ev.Payload.(*event.QuasarSpawnRequestPayload); ok {
			s.spawnQuasar(payload.SpawnX, payload.SpawnY)
		}

	case event.EventQuasarCancelRequest:
		s.terminateQuasar()
	}
}

func (s *QuasarSystem) spawnQuasar(targetX, targetY int) {
	// 1. Clamp position to screen bounds
	headerX, headerY := s.clampQuasarSpawnPosition(targetX, targetY)

	// 2. Clear area
	s.clearQuasarSpawnArea(headerX, headerY)

	// 3. Create composite entity
	headerEntity := s.createQuasarComposite(headerX, headerY)

	// 4. Update internal state
	s.headerEntity = headerEntity
	s.statActive.Store(true)

	// 5. Notify world
	s.world.PushEvent(event.EventQuasarSpawned, &event.QuasarSpawnedPayload{
		HeaderEntity: headerEntity,
	})
}

// clampQuasarSpawnPosition ensures the Quasar fits within bounds given a target center
// Input x, y is the desired center (or centroid)
// Returns the Phantom Head position (Quasar header)
func (s *QuasarSystem) clampQuasarSpawnPosition(targetX, targetY int) (int, int) {
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

// clearQuasarSpawnArea destroys all entities within the quasar footprint
func (s *QuasarSystem) clearQuasarSpawnArea(headerX, headerY int) {
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
func (s *QuasarSystem) createQuasarComposite(headerX, headerY int) core.Entity {
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

	// Set combat component
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityQuasar,
		HitPoints:        constant.CombatInitialHPQuasar,
	})

	// Set kinetic component
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

// calculateZapRadius compute zap range from game dimensions
func (s *QuasarSystem) calculateZapRadius() int64 {
	width := s.world.Resources.Config.GameWidth
	height := s.world.Resources.Config.GameHeight
	// Visual radius = max(width/2, height) since height cells = height*2 visual units
	return vmath.FromInt(max(width/2, height))
}

func (s *QuasarSystem) Update() {
	if !s.enabled {
		return
	}

	headerEntity := s.headerEntity

	if headerEntity == 0 {
		return
	}

	// Verify composite still exists
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	quasarComp, ok := s.world.Components.Quasar.GetComponent(headerEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	// Dynamic resize check: ensure radius is up to date with current screen dimensions
	currentRadius := s.calculateZapRadius()
	if quasarComp.ZapRadius != currentRadius {
		quasarComp.ZapRadius = currentRadius
	}

	// Combat sync: termination if zero hitpoints
	combatComp, ok := s.world.Components.Combat.GetComponent(headerEntity)
	if ok {
		if combatComp.HitPoints <= 0 {
			// Emit destroyed event
			s.world.PushEvent(event.EventQuasarDestroyed, nil)
			// TODO: audio effect

			s.terminateQuasar()
			return
		}
	}

	// Check if cursor is within zap range
	cursorInRange := s.isCursorInZapRange(headerEntity, &quasarComp)

	// State machine: InRange ←→ Charging → Zapping
	if cursorInRange {
		// Cursor in range: cancel any active state, return to homing
		if quasarComp.IsZapping {
			s.stopZapping(headerEntity, &quasarComp)
		}
		if quasarComp.IsCharging {
			s.cancelCharging(headerEntity, &quasarComp)
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
			s.completeCharging(headerEntity, &quasarComp)
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

	// Combat update
	if quasarComp.IsShielded {
		combatComp.RemainingDamageImmunity = constant.CombatDamageImmunityDuration
		combatComp.RemainingKineticImmunity = constant.CombatKineticImmunityDuration
		combatComp.IsEnraged = true
	} else if quasarComp.IsCharging || quasarComp.IsZapping {
		combatComp.RemainingKineticImmunity = constant.CombatKineticImmunityDuration
		combatComp.IsEnraged = true
	} else {
		combatComp.IsEnraged = false
	}
	s.world.Components.Combat.SetComponent(headerEntity, combatComp)

	s.statHitPoints.Store(int64(combatComp.HitPoints))
}

// startCharging initiates the charge phase before zapping
func (s *QuasarSystem) startCharging(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	quasarComp.IsCharging = true
	quasarComp.ChargeRemaining = constant.QuasarChargeDuration

	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)

	s.world.PushEvent(event.EventSplashTimerRequest, &event.SplashTimerRequestPayload{
		AnchorEntity: headerEntity,
		Color:        component.SplashColorCyan,
		MarginRight:  constant.QuasarHeaderOffsetX + 1, // Accounting for anchor column
		MarginLeft:   constant.QuasarHeaderOffsetX,
		MarginTop:    constant.QuasarHeaderOffsetY,
		MarginBottom: constant.QuasarHeaderOffsetY + 1, // Accounting for anchor row
		Duration:     constant.QuasarChargeDuration,
	})
}

// cancelCharging aborts the charge phase when cursor re-enters range
func (s *QuasarSystem) cancelCharging(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	quasarComp.IsCharging = false
	quasarComp.ChargeRemaining = 0
	quasarComp.IsShielded = false

	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)

	s.world.PushEvent(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{
		AnchorEntity: headerEntity,
	})
}

// completeCharging transitions from charging to zapping
func (s *QuasarSystem) completeCharging(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	quasarComp.IsCharging = false
	quasarComp.ChargeRemaining = 0

	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)

	// Transition to zapping
	s.startZapping(headerEntity, quasarComp)
}

// updateKineticMovement handles continuous kinetic quasar movement toward cursor
func (s *QuasarSystem) updateKineticMovement(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Cursor.Entity
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
	speedIncreaseInterval := time.Duration(constant.QuasarSpeedIncreaseTicks) * constant.GameUpdateInterval
	if now.Sub(quasarComp.LastSpeedIncreaseAt) >= speedIncreaseInterval {
		newMultiplier := vmath.Mul(quasarComp.SpeedMultiplier, vmath.FromFloat(1.0+constant.QuasarSpeedIncreasePercent))
		if newMultiplier > int64(constant.QuasarSpeedMultiplierMaxFixed) {
			newMultiplier = int64(constant.QuasarSpeedMultiplierMaxFixed)
		}
		quasarComp.SpeedMultiplier = newMultiplier
		quasarComp.LastSpeedIncreaseAt = now
	}

	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)

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
		// Sync grid position if snap crossed cell boundary
		if headerPos.X != cursorPos.X || headerPos.Y != cursorPos.Y {
			s.processCollisionsAtNewPositions(headerEntity, cursorPos.X, cursorPos.Y)
			s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: cursorPos.X, Y: cursorPos.Y})
		}
		return
	}

	// Cap velocity before integration to prevent runaway from cumulative dust hits
	physics.CapSpeed(&kineticComp.VelX, &kineticComp.VelY, constant.QuasarMaxSpeed)

	// Integrate position
	newX, newY := physics.Integrate(&kineticComp.Kinetic, dtFixed)

	// Boundary reflection with footprint constraints
	minHeaderX := constant.QuasarHeaderOffsetX
	maxHeaderX := config.GameWidth - (constant.QuasarWidth - constant.QuasarHeaderOffsetX)
	minHeaderY := constant.QuasarHeaderOffsetY
	maxHeaderY := config.GameHeight - (constant.QuasarHeight - constant.QuasarHeaderOffsetY)

	physics.ReflectBoundsX(&kineticComp.Kinetic, minHeaderX, maxHeaderX)
	physics.ReflectBoundsY(&kineticComp.Kinetic, minHeaderY, maxHeaderY)
	newX, newY = physics.GridPos(&kineticComp.Kinetic)

	// Update header position if cell changed
	if newX != headerPos.X || newY != headerPos.Y {
		s.processCollisionsAtNewPositions(headerEntity, newX, newY)
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
	}

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// isCursorInZapRange checks if cursor is within zap ellipse centered on quasar
func (s *QuasarSystem) isCursorInZapRange(headerEntity core.Entity, quasarComp *component.QuasarComponent) bool {
	cursorEntity := s.world.Resources.Cursor.Entity

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
	dist := vmath.MagnitudeEuclidean(dx, dyCirc)
	return dist <= quasarComp.ZapRadius
}

// Start zapping - spawn tracked lightning
func (s *QuasarSystem) startZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	cursorEntity := s.world.Resources.Cursor.Entity

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:     headerEntity,
		OriginX:   headerPos.X,
		OriginY:   headerPos.Y,
		TargetX:   cursorPos.X,
		TargetY:   cursorPos.Y,
		ColorType: component.LightningCyan,
		Duration:  constant.QuasarZapDuration,
		Tracked:   true,
	})

	quasarComp.IsZapping = true
	quasarComp.IsShielded = true // Shield active during zap
	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// stopZapping despawns lightning
func (s *QuasarSystem) stopZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	s.world.PushEvent(event.EventLightningDespawn, headerEntity)

	quasarComp.IsZapping = false
	quasarComp.IsShielded = false // Clear shield
	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// Update lightning target to track cursor
func (s *QuasarSystem) updateZapTarget(headerEntity core.Entity) {
	cursorEntity := s.world.Resources.Cursor.Entity
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

// Apply zap damage - same rate as shield overlap
func (s *QuasarSystem) applyZapDamage() {
	cursorEntity := s.world.Resources.Cursor.Entity

	shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shield.Active

	if shieldActive {
		// Drain energy through shield
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: constant.QuasarShieldDrain,
		})
	} else {
		// Direct hit - reduce 100 heat
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -constant.HeatMax})
	}
}

// processCollisionsAtNewPositions destroys entities at quasar's destination
func (s *QuasarSystem) processCollisionsAtNewPositions(headerEntity core.Entity, headerX, headerY int) {
	cursorEntity := s.world.Resources.Cursor.Entity

	header, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	// Build set of member entity IDs for exclusion
	memberSet := make(map[core.Entity]bool, len(header.MemberEntries)+1)
	memberSet[s.headerEntity] = true
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			memberSet[m.Entity] = true
		}
	}

	var toDestroy []core.Entity

	// Check each cell the quasar will occupy
	topLeftX := headerX - constant.QuasarHeaderOffsetX
	topLeftY := headerY - constant.QuasarHeaderOffsetY

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity || memberSet[e] {
					continue
				}

				// Check protection
				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask == component.ProtectAll || prot.Mask.Has(component.ProtectFromDrain) {
						continue
					}
				}

				// Handle nugget collision
				if s.world.Components.Nugget.HasEntity(e) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: e,
					})
				}

				// Handle gold composite collision
				if member, ok := s.world.Components.Member.GetComponent(e); ok {
					if h, hOk := s.world.Components.Header.GetComponent(member.HeaderEntity); hOk && h.Behavior == component.BehaviorGold {
						s.destroyGoldComposite(member.HeaderEntity)
						continue
					}
				}

				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashRequest, toDestroy)
	}
}

// destroyGoldComposite handles gold sequence destruction by quasar
func (s *QuasarSystem) destroyGoldComposite(headerEntity core.Entity) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	// Destroy all members
	var toDestroy []core.Entity
	for _, m := range headerComp.MemberEntries {
		if m.Entity != 0 {
			s.world.Components.Member.RemoveEntity(m.Entity)
			toDestroy = append(toDestroy, m.Entity)
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, toDestroy)
	}

	// Destroy phantom head
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.Components.Header.RemoveEntity(headerEntity)
	s.world.DestroyEntity(headerEntity)
}

// handleInteractions processes shield drain and cursor collision
func (s *QuasarSystem) handleInteractions(headerEntity core.Entity, headerComp *component.HeaderComponent) {
	cursorEntity := s.world.Resources.Cursor.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shieldComp.Active

	// Stack-allocated buffer for shield overlapping member offsets (max 15 cells in 3x5 quasar)
	anyOnCursor := false

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
			Value: constant.QuasarShieldDrain,
		})
	} else if anyOnCursor && !shieldActive {
		// Direct cursor collision without shieldComp → reset heat
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -constant.HeatMax})
	}
}

// terminateQuasar ends quasar phase
func (s *QuasarSystem) terminateQuasar() {
	if s.headerEntity == 0 {
		return
	}

	// Stop zapping via event (LightningSystem handles cleanup)
	s.world.PushEvent(event.EventLightningDespawn, s.headerEntity)

	// Destroy composite
	s.destroyQuasarComposite(s.headerEntity)

	s.headerEntity = 0
	s.statActive.Store(false)
}

// destroyQuasarComposite removes the quasar entity structure
func (s *QuasarSystem) destroyQuasarComposite(headerEntity core.Entity) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Destroy all members
	for _, m := range headerComp.MemberEntries {
		if m.Entity != 0 {
			s.world.Components.Member.RemoveEntity(m.Entity)
			s.world.DestroyEntity(m.Entity)
		}
	}

	// Remove components from phantom head
	s.world.Components.Quasar.RemoveEntity(headerEntity)
	s.world.Components.Header.RemoveEntity(headerEntity)
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.DestroyEntity(headerEntity)
}