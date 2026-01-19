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
	active       bool
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
	s.active = false
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
		event.EventQuasarSpawned,
		event.EventQuasarCancelRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		if s.active && s.headerEntity != 0 {
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
	case event.EventQuasarSpawned:
		payload, ok := ev.Payload.(*event.QuasarSpawnedPayload)
		if !ok {
			return
		}
		s.headerEntity = payload.HeaderEntity

		// Initialize kinetic state
		cursorEntity := s.world.Resources.Cursor.Entity
		now := s.world.Resources.Time.GameTime

		quasarComp, ok := s.world.Components.Quasar.GetComponent(s.headerEntity)
		if !ok {
			return
		}

		kineticComp, ok := s.world.Components.Kinetic.GetComponent(s.headerEntity)
		if !ok {
			return
		}

		headerPos, _ := s.world.Positions.GetPosition(s.headerEntity)
		cursorPos, _ := s.world.Positions.GetPosition(cursorEntity)

		kineticComp.PreciseX = vmath.FromInt(headerPos.X)
		kineticComp.PreciseY = vmath.FromInt(headerPos.Y)
		quasarComp.SpeedMultiplier = vmath.Scale
		quasarComp.LastSpeedIncreaseAt = now

		// Initialize dynamic radius
		quasarComp.ZapRadius = s.calculateZapRadius()

		// Initial velocity toward cursor
		dx := vmath.FromInt(cursorPos.X - headerPos.X)
		dy := vmath.FromInt(cursorPos.Y - headerPos.Y)
		dirX, dirY := vmath.Normalize2D(dx, dy)
		kineticComp.VelX = vmath.Mul(dirX, constant.QuasarBaseSpeed)
		kineticComp.VelY = vmath.Mul(dirY, constant.QuasarBaseSpeed)

		s.world.Components.Quasar.SetComponent(s.headerEntity, quasarComp)
		s.world.Components.Kinetic.SetComponent(s.headerEntity, kineticComp)

		combatComp := component.CombatComponent{
			HitPoints: constant.QuasarInitialHP,
		}
		s.world.Components.Combat.SetComponent(s.headerEntity, combatComp)

		s.statHitPoints.Store(int64(combatComp.HitPoints))
		s.active = true
		s.statActive.Store(true)

	case event.EventQuasarCancelRequest:
		if s.active {
			s.terminateQuasar()
		}
	}
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

	if !s.active || headerEntity == 0 {
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

	// Check HitPoints for termination
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
		s.world.Components.Quasar.SetComponent(headerEntity, quasarComp) // Persist flash decrement // TODO: check

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
	s.handleInteractions(headerEntity, &headerComp, &quasarComp)

	s.statHitPoints.Store(int64(combatComp.HitPoints))
}

// startCharging initiates the charge phase before zapping
func (s *QuasarSystem) startCharging(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	quasarComp.IsCharging = true
	quasarComp.IsEnraged = true
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
	quasarComp.IsEnraged = false

	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)

	s.world.PushEvent(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{
		AnchorEntity: headerEntity,
	})
}

// completeCharging transitions from charging to zapping
func (s *QuasarSystem) completeCharging(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	quasarComp.IsCharging = false
	quasarComp.IsEnraged = true
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
		&kineticComp,
		cursorXFixed, cursorYFixed,
		&physics.QuasarHoming,
		quasarComp.SpeedMultiplier,
		dtFixed,
		true, // !quasarComp.IsImmune(now), // applyDrag gated by immunity // TODO: change after full migration
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
	newX, newY := kineticComp.Integrate(dtFixed)

	// Boundary reflection with footprint constraints
	minHeaderX := constant.QuasarHeaderOffsetX
	maxHeaderX := config.GameWidth - (constant.QuasarWidth - constant.QuasarHeaderOffsetX)
	minHeaderY := constant.QuasarHeaderOffsetY
	maxHeaderY := config.GameHeight - (constant.QuasarHeight - constant.QuasarHeaderOffsetY)

	kineticComp.ReflectBoundsX(minHeaderX, maxHeaderX)
	kineticComp.ReflectBoundsY(minHeaderY, maxHeaderY)
	newX, newY = kineticComp.GridPos()

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

	s.world.PushEvent(event.EventLightningSpawn, &event.LightningSpawnRequestPayload{
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
	quasarComp.IsEnraged = true
	quasarComp.IsShielded = true // Shield active during zap
	s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
}

// stopZapping despawns lightning
func (s *QuasarSystem) stopZapping(headerEntity core.Entity, quasarComp *component.QuasarComponent) {
	s.world.PushEvent(event.EventLightningDespawn, headerEntity)

	quasarComp.IsZapping = false
	quasarComp.IsEnraged = false
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
		s.world.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
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
func (s *QuasarSystem) handleInteractions(headerEntity core.Entity, headerComp *component.HeaderComponent, quasar *component.QuasarComponent) {
	cursorEntity := s.world.Resources.Cursor.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shieldComp.Active

	// Stack-allocated buffer for shield overlapping member offsets (max 15 cells in 3x5 quasar)
	var overlapOffsets [15]struct{ x, y int }
	overlapCount := 0
	anyOnCursor := false

	for _, m := range headerComp.MemberEntries {
		if m.Entity == 0 {
			continue
		}

		memberPos, ok := s.world.Positions.GetPosition(m.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && s.isInsideShieldEllipse(memberPos.X, memberPos.Y, cursorPos, &shieldComp) {
			overlapOffsets[overlapCount] = struct{ x, y int }{int(m.OffsetX), int(m.OffsetY)}
			overlapCount++
		}
	}
	anyInShield := overlapCount > 0

	// Shield knockback check
	if anyInShield {
		s.applyShieldKnockback(headerEntity, quasar, cursorPos, overlapOffsets[:overlapCount])
	}

	// Shield drain (once per tick if any overlap)
	if anyInShield {
		s.world.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
		})
		return // Shield protects from direct collision
	}

	// Direct cursor collision without shieldComp → reset heat
	if anyOnCursor && !shieldActive {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: -constant.HeatMax})
	}
}

// isInsideShieldEllipse checks if position is within shield using fixed-point math
func (s *QuasarSystem) isInsideShieldEllipse(x, y int, cursorPos component.PositionComponent, shieldComp *component.ShieldComponent) bool {
	dx := vmath.FromInt(x - cursorPos.X)
	dy := vmath.FromInt(y - cursorPos.Y)
	return vmath.EllipseContains(dx, dy, shieldComp.InvRxSq, shieldComp.InvRySq)
}

// applyShieldKnockback applies radial impulse when quasar overlaps shield
// Uses centroid of overlapping member offsets for offset-influenced direction
func (s *QuasarSystem) applyShieldKnockback(
	headerEntity core.Entity,
	quasarComp *component.QuasarComponent,
	cursorPos component.PositionComponent,
	overlaps []struct{ x, y int },
) {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Radial direction: cursor → anchor (shield pushes outward)
	radialX := vmath.FromInt(headerPos.X - cursorPos.X)
	radialY := vmath.FromInt(headerPos.Y - cursorPos.Y)

	// Zero vector fallback (quasarComp centered on cursor)
	if radialX == 0 && radialY == 0 {
		radialX = vmath.Scale // Push right by default
	}

	// Centroid of overlapping member offsets (integer arithmetic)
	sumX, sumY := 0, 0
	for _, o := range overlaps {
		sumX += o.x
		sumY += o.y
	}
	centroidX := sumX / len(overlaps)
	centroidY := sumY / len(overlaps)

	if physics.ApplyOffsetCollision(
		&kineticComp,
		radialX, radialY,
		centroidX, centroidY,
		&physics.ShieldToQuasar,
		s.rng,
	) {
		s.world.Components.Quasar.SetComponent(headerEntity, *quasarComp)
		s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
	}
}

// terminateQuasar ends quasar phase
func (s *QuasarSystem) terminateQuasar() {
	if !s.active {
		return
	}

	// Stop zapping via event (LightningSystem handles cleanup)
	if s.headerEntity != 0 {
		s.world.PushEvent(event.EventLightningDespawn, s.headerEntity)
	}

	// Destroy composite
	if s.headerEntity != 0 {
		s.destroyQuasarComposite(s.headerEntity)
	}

	// Resume drain spawning
	s.world.PushEvent(event.EventDrainResume, nil)

	s.active = false
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