package system

// @lixen: #dev{feature[dust(render,system)],feature[quasar(render,system)]}

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// QuasarSystem manages the quasar boss entity lifecycle
// Quasar is a 3x5 composite that tracks cursor at 2x drain speed
// Drains 1000 energy/tick when any part overlaps shield
// Resets heat to 0 on direct cursor collision without shield
type QuasarSystem struct {
	mu sync.RWMutex
	engine.SystemBase

	// Runtime state
	active       bool
	anchorEntity core.Entity

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool

	enabled bool
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	res := engine.GetResourceStore(world)
	s := &QuasarSystem{
		SystemBase: engine.NewSystemBase(world),
	}

	s.statActive = res.Status.Bools.Get("quasar.active")

	s.initLocked()
	return s
}

func (s *QuasarSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

func (s *QuasarSystem) initLocked() {
	s.active = false
	s.anchorEntity = 0
	s.rng = vmath.NewFastRand(uint32(s.Resource.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.enabled = true
}

func (s *QuasarSystem) Priority() int {
	return constant.PriorityQuasar
}

func (s *QuasarSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventQuasarSpawned,
		event.EventGoldComplete,
		event.EventGameReset,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.mu.Lock()
		if s.active && s.anchorEntity != 0 {
			s.terminateQuasarLocked()
		}
		s.mu.Unlock()
		s.Init()
		return
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
		s.mu.Lock()
		s.active = true
		s.anchorEntity = payload.AnchorEntity

		// Initialize kinetic state
		cursorEntity := s.Resource.Cursor.Entity
		now := s.Resource.Time.GameTime

		if quasar, ok := s.Component.Quasar.Get(s.anchorEntity); ok {
			anchorPos, _ := s.World.Positions.Get(s.anchorEntity)
			cursorPos, _ := s.World.Positions.Get(cursorEntity)

			quasar.PreciseX = vmath.FromInt(anchorPos.X)
			quasar.PreciseY = vmath.FromInt(anchorPos.Y)
			quasar.SpeedMultiplier = vmath.Scale
			quasar.LastSpeedIncreaseAt = now
			quasar.HitPoints = constant.QuasarInitialHP // Init HP

			// Initialize dynamic radius
			quasar.ZapRadius = s.calculateZapRadius()

			// Initial velocity toward cursor
			dx := vmath.FromInt(cursorPos.X - anchorPos.X)
			dy := vmath.FromInt(cursorPos.Y - anchorPos.Y)
			dirX, dirY := vmath.Normalize2D(dx, dy)
			quasar.VelX = vmath.Mul(dirX, constant.QuasarBaseSpeed)
			quasar.VelY = vmath.Mul(dirY, constant.QuasarBaseSpeed)

			s.Component.Quasar.Set(s.anchorEntity, quasar)
		}

		s.statActive.Store(true)
		s.mu.Unlock()

		// Activate persistent grayout
		s.Resource.State.State.StartGrayout()

	case event.EventGoldComplete:
		s.mu.RLock()
		active := s.active
		s.mu.RUnlock()

		if active {
			s.terminateQuasar()
		}
	}
}

// calculateZapRadius compute zap range from game dimensions
func (s *QuasarSystem) calculateZapRadius() int32 {
	width := s.Resource.Config.GameWidth
	height := s.Resource.Config.GameHeight
	// Visual radius = max(width/2, height) since height cells = height*2 visual units
	return vmath.FromInt(max(width/2, height))
}

func (s *QuasarSystem) Update() {
	if !s.enabled {
		return
	}

	s.mu.RLock()
	active := s.active
	anchorEntity := s.anchorEntity
	s.mu.RUnlock()

	if !active || anchorEntity == 0 {
		return
	}

	// Check heat for termination (heat=0 ends quasar phase)
	cursorEntity := s.Resource.Cursor.Entity
	if hc, ok := s.Component.Heat.Get(cursorEntity); ok {
		if hc.Current.Load() <= 0 {
			s.terminateQuasar()
			return
		}
	}

	// Verify composite still exists
	header, ok := s.Component.Header.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	quasar, ok := s.Component.Quasar.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	// Dynamic resize check: ensure radius is up to date with current screen dimensions
	currentRadius := s.calculateZapRadius()
	if quasar.ZapRadius != currentRadius {
		quasar.ZapRadius = currentRadius
	}

	// Decrement flash timer
	if quasar.HitFlashRemaining > 0 {
		quasar.HitFlashRemaining -= s.Resource.Time.DeltaTime
		if quasar.HitFlashRemaining < 0 {
			quasar.HitFlashRemaining = 0
		}
	}

	// Check HP for termination
	if quasar.HitPoints <= 0 {
		s.terminateQuasar()
		return
	}

	// Check if cursor is within zap range
	cursorInRange := s.isCursorInZapRange(anchorEntity, &quasar)

	// State machine: InRange ←→ Charging → Zapping
	if cursorInRange {
		// Cursor in range: cancel any active state, return to homing
		if quasar.IsZapping {
			s.stopZapping(&quasar, anchorEntity)
		}
		if quasar.IsCharging {
			s.cancelCharging(&quasar, anchorEntity)
		}

		s.updateKineticMovement(anchorEntity, &quasar)
		s.Component.Quasar.Set(anchorEntity, quasar)

	} else if quasar.IsZapping {
		// Already zapping: continue zap, update target
		s.updateZapTarget(anchorEntity)
		s.applyZapDamage()
		s.Component.Quasar.Set(anchorEntity, quasar) // Persist flash decrement // TODO: check

	} else if quasar.IsCharging {
		// Charging: decrement timer, check completion
		quasar.ChargeRemaining -= s.Resource.Time.DeltaTime

		if quasar.ChargeRemaining <= 0 {
			s.completeCharging(&quasar, anchorEntity)
		} else {
			// Continue homing during charge
			s.updateKineticMovement(anchorEntity, &quasar)
			s.Component.Quasar.Set(anchorEntity, quasar)
		}

	} else {
		// Cursor out of range, not charging, not zapping: start charging
		s.startCharging(&quasar, anchorEntity)
	}

	// Shield and cursor interaction (all states)
	s.handleInteractions(anchorEntity, &header, &quasar)
}

// startCharging initiates the charge phase before zapping
func (s *QuasarSystem) startCharging(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	quasar.IsCharging = true
	quasar.ChargeRemaining = constant.QuasarChargeDuration

	s.Component.Quasar.Set(anchorEntity, *quasar)

	s.World.PushEvent(event.EventQuasarChargeStart, &event.QuasarChargeStartPayload{
		AnchorEntity: anchorEntity,
		Duration:     constant.QuasarChargeDuration,
	})
}

// cancelCharging aborts the charge phase when cursor re-enters range
func (s *QuasarSystem) cancelCharging(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	quasar.IsCharging = false
	quasar.ChargeRemaining = 0
	quasar.ShieldActive = false

	s.Component.Quasar.Set(anchorEntity, *quasar)

	s.World.PushEvent(event.EventQuasarChargeCancel, &event.QuasarChargeCancelPayload{
		AnchorEntity: anchorEntity,
	})
}

// completeCharging transitions from charging to zapping
func (s *QuasarSystem) completeCharging(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	quasar.IsCharging = false
	quasar.ChargeRemaining = 0

	s.Component.Quasar.Set(anchorEntity, *quasar)

	// Transition to zapping
	s.startZapping(quasar, anchorEntity)
}

// updateKineticMovement handles continuous kinetic quasar movement toward cursor
func (s *QuasarSystem) updateKineticMovement(anchorEntity core.Entity, quasar *component.QuasarComponent) {
	config := s.Resource.Config
	cursorEntity := s.Resource.Cursor.Entity
	now := s.Resource.Time.GameTime

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	anchorPos, ok := s.World.Positions.Get(anchorEntity)
	if !ok {
		return
	}

	dtFixed := vmath.FromFloat(s.Resource.Time.DeltaTime.Seconds())
	// Cap delta to prevent tunneling
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Periodic speed scaling with cap
	speedIncreaseInterval := time.Duration(constant.QuasarSpeedIncreaseTicks) * constant.GameUpdateInterval
	if now.Sub(quasar.LastSpeedIncreaseAt) >= speedIncreaseInterval {
		newMultiplier := vmath.Mul(quasar.SpeedMultiplier, vmath.FromFloat(1.0+constant.QuasarSpeedIncreasePercent))
		if newMultiplier > int32(constant.QuasarSpeedMultiplierMaxFixed) {
			newMultiplier = int32(constant.QuasarSpeedMultiplierMaxFixed)
		}
		quasar.SpeedMultiplier = newMultiplier
		quasar.LastSpeedIncreaseAt = now
	}

	inDeflection := now.Before(quasar.DeflectUntil)

	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)
	dx := cursorXFixed - quasar.PreciseX
	dy := cursorYFixed - quasar.PreciseY
	dist := vmath.Magnitude(dx, dy)

	// Hard snap - dead zone clamp for zero wobble
	deadZone := int32(vmath.Scale) / 2 // 0.5 cells
	if dist < deadZone {
		quasar.PreciseX = cursorXFixed
		quasar.PreciseY = cursorYFixed
		quasar.VelX = 0
		quasar.VelY = 0

		// Sync grid position if snap crossed cell boundary
		if anchorPos.X != cursorPos.X || anchorPos.Y != cursorPos.Y {
			s.processCollisionsAtNewPositions(anchorEntity, cursorPos.X, cursorPos.Y)
			s.World.Positions.Set(anchorEntity, component.PositionComponent{X: cursorPos.X, Y: cursorPos.Y})
		}

		// Skip all physics, already at target
		return
	}

	// Physics configuration
	effectiveAccel := vmath.Mul(constant.QuasarHomingAccel, quasar.SpeedMultiplier)
	effectiveDrag := constant.QuasarDrag

	// ARRIVAL STEERING: Dampen motion when close to prevent overshoot
	arrivalRadius := vmath.FromFloat(3.0)
	if dist < arrivalRadius {
		factor := vmath.Div(dist, arrivalRadius) // 0 at target, 1 at edge

		// Ramp down acceleration
		effectiveAccel = vmath.Mul(effectiveAccel, factor)

		// Ramp up drag: 1x at edge → 4x at center
		dampingBoost := vmath.FromFloat(4.0) - vmath.Mul(vmath.FromFloat(3.0), factor)
		effectiveDrag = vmath.Mul(effectiveDrag, dampingBoost)
	}

	// Apply homing force (always, even during deflection per user spec)
	dirX, dirY := vmath.Normalize2D(dx, dy)
	quasar.VelX += vmath.Mul(vmath.Mul(dirX, effectiveAccel), dtFixed)
	quasar.VelY += vmath.Mul(vmath.Mul(dirY, effectiveAccel), dtFixed)

	// Drag only outside deflection (ballistic deflection)
	if !inDeflection {
		dragFactor := vmath.Mul(effectiveDrag, dtFixed)
		if dragFactor > vmath.Scale {
			dragFactor = vmath.Scale
		}
		quasar.VelX -= vmath.Mul(quasar.VelX, dragFactor)
		quasar.VelY -= vmath.Mul(quasar.VelY, dragFactor)
	}

	// Soft settling - backup for edge cases
	settleDistThreshold := int32(vmath.Scale) / 4  // 0.25 cells
	settleSpeedThreshold := int32(vmath.Scale) / 2 // 0.5 cells/sec
	speed := vmath.Magnitude(quasar.VelX, quasar.VelY)

	if dist < settleDistThreshold && speed < settleSpeedThreshold {
		quasar.VelX = 0
		quasar.VelY = 0
	}

	// Integrate position
	newX, newY := quasar.Integrate(dtFixed)

	// Boundary reflection with footprint constraints
	minAnchorX := constant.QuasarAnchorOffsetX
	maxAnchorX := config.GameWidth - (constant.QuasarWidth - constant.QuasarAnchorOffsetX)
	minAnchorY := constant.QuasarAnchorOffsetY
	maxAnchorY := config.GameHeight - (constant.QuasarHeight - constant.QuasarAnchorOffsetY)

	if newX < minAnchorX {
		newX = minAnchorX
		quasar.PreciseX = vmath.FromInt(minAnchorX)
		quasar.VelX, quasar.VelY = vmath.ReflectAxisX(quasar.VelX, quasar.VelY)
	} else if newX >= maxAnchorX {
		newX = maxAnchorX - 1
		quasar.PreciseX = vmath.FromInt(maxAnchorX - 1)
		quasar.VelX, quasar.VelY = vmath.ReflectAxisX(quasar.VelX, quasar.VelY)
	}

	if newY < minAnchorY {
		newY = minAnchorY
		quasar.PreciseY = vmath.FromInt(minAnchorY)
		quasar.VelX, quasar.VelY = vmath.ReflectAxisY(quasar.VelX, quasar.VelY)
	} else if newY >= maxAnchorY {
		newY = maxAnchorY - 1
		quasar.PreciseY = vmath.FromInt(maxAnchorY - 1)
		quasar.VelX, quasar.VelY = vmath.ReflectAxisY(quasar.VelX, quasar.VelY)
	}

	// Update anchor position if cell changed
	if newX != anchorPos.X || newY != anchorPos.Y {
		s.processCollisionsAtNewPositions(anchorEntity, newX, newY)
		s.World.Positions.Set(anchorEntity, component.PositionComponent{X: newX, Y: newY})
	}
}

// isCursorInZapRange checks if cursor is within zap ellipse centered on quasar
func (s *QuasarSystem) isCursorInZapRange(anchorEntity core.Entity, quasar *component.QuasarComponent) bool {
	cursorEntity := s.Resource.Cursor.Entity

	anchorPos, ok := s.World.Positions.Get(anchorEntity)
	if !ok {
		return true // Failsafe: don't zap if can't determine
	}

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return true
	}

	dx := vmath.FromInt(cursorPos.X - anchorPos.X)
	dy := vmath.FromInt(cursorPos.Y - anchorPos.Y)

	// Inside visual circle = in range (no zap)
	dyCirc := vmath.ScaleToCircular(dy) // Aspect correction: dy * 2
	dist := vmath.MagnitudeEuclidean(dx, dyCirc)
	return dist <= quasar.ZapRadius
}

// Start zapping - spawnLightning tracked lightning
func (s *QuasarSystem) startZapping(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	cursorEntity := s.Resource.Cursor.Entity

	anchorPos, ok := s.World.Positions.Get(anchorEntity)
	if !ok {
		return
	}
	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	s.World.PushEvent(event.EventLightningSpawn, &event.LightningSpawnPayload{
		Owner:     anchorEntity,
		OriginX:   anchorPos.X,
		OriginY:   anchorPos.Y,
		TargetX:   cursorPos.X,
		TargetY:   cursorPos.Y,
		ColorType: component.LightningCyan,
		Duration:  constant.QuasarZapDuration,
		Tracked:   true,
	})

	quasar.IsZapping = true
	quasar.ShieldActive = true // Shield active during zap
	s.Component.Quasar.Set(anchorEntity, *quasar)
}

// stopZapping despawns lightning
func (s *QuasarSystem) stopZapping(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	s.World.PushEvent(event.EventLightningDespawn, anchorEntity)

	quasar.IsZapping = false
	quasar.ShieldActive = false // Clear shield
	s.Component.Quasar.Set(anchorEntity, *quasar)
}

// Update lightning target to track cursor
func (s *QuasarSystem) updateZapTarget(anchorEntity core.Entity) {
	cursorEntity := s.Resource.Cursor.Entity
	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	s.World.PushEvent(event.EventLightningUpdate, &event.LightningUpdatePayload{
		Owner:   anchorEntity,
		TargetX: cursorPos.X,
		TargetY: cursorPos.Y,
	})
}

// Apply zap damage - same rate as shield overlap
func (s *QuasarSystem) applyZapDamage() {
	cursorEntity := s.Resource.Cursor.Entity

	shield, shieldOk := s.Component.Shield.Get(cursorEntity)
	shieldActive := shieldOk && shield.Active

	if shieldActive {
		// Drain energy through shield
		s.World.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
		})
	} else {
		// Direct hit - reset heat (terminates phase)
		s.World.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
	}
}

// processCollisionsAtNewPositions destroys entities at quasar's destination
func (s *QuasarSystem) processCollisionsAtNewPositions(anchorEntity core.Entity, anchorX, anchorY int) {
	cursorEntity := s.Resource.Cursor.Entity

	header, ok := s.Component.Header.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	// Build set of member entity IDs for exclusion
	memberSet := make(map[core.Entity]bool, len(header.Members)+1)
	memberSet[s.anchorEntity] = true
	for _, m := range header.Members {
		if m.Entity != 0 {
			memberSet[m.Entity] = true
		}
	}

	var toDestroy []core.Entity

	// Check each cell the quasar will occupy
	topLeftX := anchorX - constant.QuasarAnchorOffsetX
	topLeftY := anchorY - constant.QuasarAnchorOffsetY

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.World.Positions.GetAllAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity || memberSet[e] {
					continue
				}

				// Check protection
				if prot, ok := s.Component.Protection.Get(e); ok {
					if prot.Mask == component.ProtectAll {
						continue
					}
				}

				// Handle nugget collision
				if s.Component.Nugget.Has(e) {
					s.World.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: e,
					})
				}

				// Handle gold composite collision
				if member, ok := s.Component.Member.Get(e); ok {
					if h, hOk := s.Component.Header.Get(member.AnchorID); hOk && h.BehaviorID == component.BehaviorGold {
						s.destroyGoldComposite(member.AnchorID)
						continue
					}
				}

				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.Resource.Events.Queue, 0, toDestroy, s.Resource.Time.FrameNumber)
	}
}

// destroyGoldComposite handles gold sequence destruction by quasar
func (s *QuasarSystem) destroyGoldComposite(anchorID core.Entity) {
	header, ok := s.Component.Header.Get(anchorID)
	if !ok {
		return
	}

	s.World.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		AnchorEntity: anchorID,
	})

	// Destroy all members
	var toDestroy []core.Entity
	for _, m := range header.Members {
		if m.Entity != 0 {
			s.Component.Member.Remove(m.Entity)
			toDestroy = append(toDestroy, m.Entity)
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.Resource.Events.Queue, 0, toDestroy, s.Resource.Time.FrameNumber)
	}

	// Destroy phantom head
	s.Component.Protection.Remove(anchorID)
	s.Component.Header.Remove(anchorID)
	s.World.DestroyEntity(anchorID)
}

// handleInteractions processes shield drain and cursor collision
func (s *QuasarSystem) handleInteractions(anchorEntity core.Entity, header *component.CompositeHeaderComponent, quasar *component.QuasarComponent) {
	cursorEntity := s.Resource.Cursor.Entity

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	shield, shieldOk := s.Component.Shield.Get(cursorEntity)
	shieldActive := shieldOk && shield.Active

	// Stack-allocated buffer for shield overlapping member offsets (max 15 cells in 3x5 quasar)
	var overlapOffsets [15]struct{ x, y int }
	overlapCount := 0
	anyOnCursor := false

	// // Check each member position
	// anyInShield := false
	// anyOnCursor := false

	for _, m := range header.Members {
		if m.Entity == 0 {
			continue
		}

		memberPos, ok := s.World.Positions.Get(m.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && s.isInsideShieldEllipse(memberPos.X, memberPos.Y, cursorPos, &shield) {
			overlapOffsets[overlapCount] = struct{ x, y int }{int(m.OffsetX), int(m.OffsetY)}
			overlapCount++
		}
	}
	anyInShield := overlapCount > 0

	// Update cached state
	if quasar.IsOnCursor != anyOnCursor {
		quasar.IsOnCursor = anyOnCursor
		s.Component.Quasar.Set(anchorEntity, *quasar)
	}

	// Shield knockback check
	if anyInShield {
		s.applyShieldKnockback(anchorEntity, quasar, cursorPos, overlapOffsets[:overlapCount])
	}

	// Shield drain (once per tick if any overlap)
	if anyInShield {
		s.World.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
		})
		return // Shield protects from direct collision
	}

	// Direct cursor collision without shield → reset heat to 0
	if anyOnCursor && !shieldActive {
		s.World.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
		// Heat=0 will trigger termination in next Update()
	}
}

// isInsideShieldEllipse checks if position is within shield using fixed-point math
func (s *QuasarSystem) isInsideShieldEllipse(x, y int, cursorPos component.PositionComponent, shield *component.ShieldComponent) bool {
	dx := vmath.FromInt(x - cursorPos.X)
	dy := vmath.FromInt(y - cursorPos.Y)
	return vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)
}

// applyShieldKnockback applies radial impulse when quasar overlaps shield
// Uses centroid of overlapping member offsets for offset-influenced direction
func (s *QuasarSystem) applyShieldKnockback(
	anchorEntity core.Entity,
	quasar *component.QuasarComponent,
	cursorPos component.PositionComponent,
	overlaps []struct{ x, y int },
) {
	now := s.Resource.Time.GameTime

	// Immunity gate (unified with cleaner knockback)
	if now.Before(quasar.DeflectUntil) {
		return
	}

	anchorPos, ok := s.World.Positions.Get(anchorEntity)
	if !ok {
		return
	}

	// Radial direction: cursor → anchor (shield pushes outward)
	radialX := vmath.FromInt(anchorPos.X - cursorPos.X)
	radialY := vmath.FromInt(anchorPos.Y - cursorPos.Y)

	// Zero vector fallback (quasar centered on cursor)
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

	// Offset-influenced collision impulse (same physics as cleaner)
	impulseX, impulseY := vmath.ApplyOffsetCollisionImpulse(
		radialX, radialY,
		centroidX, centroidY,
		vmath.OffsetInfluenceDefault,
		vmath.MassRatioCleanerToQuasar,
		constant.DrainDeflectAngleVar,
		constant.ShieldKnockbackImpulseMin,
		constant.ShieldKnockbackImpulseMax,
		s.rng,
	)

	if impulseX == 0 && impulseY == 0 {
		return
	}

	// Additive impulse (momentum transfer)
	quasar.VelX += impulseX
	quasar.VelY += impulseY

	// Set immunity window
	quasar.DeflectUntil = now.Add(constant.ShieldKnockbackImmunity)

	s.Component.Quasar.Set(anchorEntity, *quasar)
}

// terminateQuasar ends the quasar phase
func (s *QuasarSystem) terminateQuasar() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminateQuasarLocked()
}

// terminateQuasarLocked ends quasar phase, caller must hold s.mu
func (s *QuasarSystem) terminateQuasarLocked() {
	if !s.active {
		return
	}

	// Stop zapping via event (LightningSystem handles cleanup)
	if s.anchorEntity != 0 {
		s.World.PushEvent(event.EventLightningDespawn, s.anchorEntity)
	}

	// Destroy composite
	if s.anchorEntity != 0 {
		s.destroyQuasarComposite(s.anchorEntity)
	}

	// End grayout
	s.Resource.State.State.EndGrayout()

	// Resume drain spawning
	s.World.PushEvent(event.EventDrainResume, nil)

	// Emit destroyed event (for future audio/effects)
	s.World.PushEvent(event.EventQuasarDestroyed, nil)

	s.active = false
	s.anchorEntity = 0
	s.statActive.Store(false)
}

// destroyQuasarComposite removes the quasar entity structure
func (s *QuasarSystem) destroyQuasarComposite(anchorEntity core.Entity) {
	header, ok := s.Component.Header.Get(anchorEntity)
	if !ok {
		return
	}

	// Destroy all members
	for _, m := range header.Members {
		if m.Entity != 0 {
			s.Component.Member.Remove(m.Entity)
			s.World.DestroyEntity(m.Entity)
		}
	}

	// Remove components from phantom head
	s.Component.Quasar.Remove(anchorEntity)
	s.Component.Header.Remove(anchorEntity)
	s.Component.Protection.Remove(anchorEntity)
	s.World.DestroyEntity(anchorEntity)
}