package system

// @lixen: #dev{feature[quasar(render,system)]}

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
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	quasarStore *engine.Store[component.QuasarComponent]
	headerStore *engine.Store[component.CompositeHeaderComponent]
	memberStore *engine.Store[component.MemberComponent]
	shieldStore *engine.Store[component.ShieldComponent]
	heatStore   *engine.Store[component.HeatComponent]
	protStore   *engine.Store[component.ProtectionComponent]
	nuggetStore *engine.Store[component.NuggetComponent]

	// Runtime state
	active       bool
	anchorEntity core.Entity

	// Zap range ellipse (precomputed on config change)
	zapInvRxSq int32
	zapInvRySq int32

	// Telemetry
	statActive *atomic.Bool

	enabled bool
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &QuasarSystem{
		world: world,
		res:   res,

		quasarStore: engine.GetStore[component.QuasarComponent](world),
		headerStore: engine.GetStore[component.CompositeHeaderComponent](world),
		memberStore: engine.GetStore[component.MemberComponent](world),
		shieldStore: engine.GetStore[component.ShieldComponent](world),
		heatStore:   engine.GetStore[component.HeatComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
		nuggetStore: engine.GetStore[component.NuggetComponent](world),

		statActive: res.Status.Bools.Get("quasar.active"),
	}
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
	s.zapInvRxSq = 0
	s.zapInvRySq = 0
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
		cursorEntity := s.res.Cursor.Entity
		now := s.res.Time.GameTime

		if quasar, ok := s.quasarStore.Get(s.anchorEntity); ok {
			anchorPos, _ := s.world.Positions.Get(s.anchorEntity)
			cursorPos, _ := s.world.Positions.Get(cursorEntity)

			quasar.PreciseX = vmath.FromInt(anchorPos.X)
			quasar.PreciseY = vmath.FromInt(anchorPos.Y)
			quasar.SpeedMultiplier = vmath.Scale
			quasar.LastSpeedIncreaseAt = now

			// Initial velocity toward cursor
			dx := vmath.FromInt(cursorPos.X - anchorPos.X)
			dy := vmath.FromInt(cursorPos.Y - anchorPos.Y)
			dirX, dirY := vmath.Normalize2D(dx, dy)
			quasar.VelX = vmath.Mul(dirX, constant.QuasarBaseSpeed)
			quasar.VelY = vmath.Mul(dirY, constant.QuasarBaseSpeed)

			s.quasarStore.Set(s.anchorEntity, quasar)
		}

		// Compute zap range ellipse
		s.updateZapEllipse()

		s.statActive.Store(true)
		s.mu.Unlock()

		// Activate persistent grayout
		s.res.State.State.StartGrayout()

	case event.EventGoldComplete:
		s.mu.RLock()
		active := s.active
		s.mu.RUnlock()

		if active {
			s.terminateQuasar()
		}
	}
}

// Compute zap range ellipse from game dimensions
func (s *QuasarSystem) updateZapEllipse() {
	config := s.res.Config
	rx := vmath.FromInt(config.GameWidth / 2)
	ry := vmath.FromInt(config.GameHeight / 2)
	s.zapInvRxSq, s.zapInvRySq = vmath.EllipseInvRadiiSq(rx, ry)
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
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		if hc.Current.Load() <= 0 {
			s.terminateQuasar()
			return
		}
	}

	// Verify composite still exists
	header, ok := s.headerStore.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	quasar, ok := s.quasarStore.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	// Check if cursor is within zap range
	cursorInRange := s.isCursorInZapRange(anchorEntity)

	if cursorInRange {
		// Stop zapping if was zapping
		if quasar.IsZapping {
			s.stopZapping(&quasar, anchorEntity)
		}

		// Kinetic movement
		s.updateKineticMovement(anchorEntity, &quasar)

		s.quasarStore.Set(anchorEntity, quasar)
	} else {
		// Cursor outside range: zap
		if !quasar.IsZapping {
			s.startZapping(&quasar, anchorEntity)
		} else {
			s.updateZapTarget(&quasar, anchorEntity)
		}

		// Apply zap damage (same as shield overlap)
		s.applyZapDamage()
	}

	// Shield and cursor interaction
	s.handleInteractions(anchorEntity, &header, &quasar)
}

// updateKineticMovement handles continuous kinetic quasar movement toward cursor
func (s *QuasarSystem) updateKineticMovement(anchorEntity core.Entity, quasar *component.QuasarComponent) {
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity
	now := s.res.Time.GameTime

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	anchorPos, ok := s.world.Positions.Get(anchorEntity)
	if !ok {
		return
	}

	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	// Cap delta to prevent tunneling
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Periodic speed scaling
	speedIncreaseInterval := time.Duration(constant.QuasarSpeedIncreaseTicks) * constant.GameUpdateInterval
	if now.Sub(quasar.LastSpeedIncreaseAt) >= speedIncreaseInterval {
		quasar.SpeedMultiplier = vmath.Mul(quasar.SpeedMultiplier, vmath.FromFloat(1.0+constant.QuasarSpeedIncreasePercent))
		quasar.LastSpeedIncreaseAt = now
	}

	// Effective base speed scaled by multiplier
	effectiveBaseSpeed := vmath.Mul(constant.QuasarBaseSpeed, quasar.SpeedMultiplier)

	inDeflection := now.Before(quasar.DeflectUntil)

	if !inDeflection {
		// Homing toward cursor
		cursorXFixed := vmath.FromInt(cursorPos.X)
		cursorYFixed := vmath.FromInt(cursorPos.Y)
		dx := cursorXFixed - quasar.PreciseX
		dy := cursorYFixed - quasar.PreciseY
		dirX, dirY := vmath.Normalize2D(dx, dy)

		currentSpeed := vmath.Magnitude(quasar.VelX, quasar.VelY)

		// Scale homing by inverse speed when overspeed for curved comeback
		homingAccel := constant.QuasarHomingAccel
		if currentSpeed > effectiveBaseSpeed && currentSpeed > 0 {
			homingAccel = vmath.Div(vmath.Mul(constant.QuasarHomingAccel, effectiveBaseSpeed), currentSpeed)
		}

		quasar.VelX += vmath.Mul(vmath.Mul(dirX, homingAccel), dtFixed)
		quasar.VelY += vmath.Mul(vmath.Mul(dirY, homingAccel), dtFixed)

		// Drag if overspeed
		if currentSpeed > effectiveBaseSpeed && currentSpeed > 0 {
			excess := currentSpeed - effectiveBaseSpeed
			dragScale := vmath.Div(excess, currentSpeed)
			dragAmount := vmath.Mul(vmath.Mul(constant.QuasarDrag, dtFixed), dragScale)

			quasar.VelX -= vmath.Mul(quasar.VelX, dragAmount)
			quasar.VelY -= vmath.Mul(quasar.VelY, dragAmount)
		}
	}
	// During deflection: pure ballistic

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
		s.world.Positions.Set(anchorEntity, component.PositionComponent{X: newX, Y: newY})
	}
}

// Check if cursor is within zap ellipse centered on quasar
func (s *QuasarSystem) isCursorInZapRange(anchorEntity core.Entity) bool {
	cursorEntity := s.res.Cursor.Entity

	anchorPos, ok := s.world.Positions.Get(anchorEntity)
	if !ok {
		return true // Failsafe: don't zap if can't determine
	}

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return true
	}

	dx := vmath.FromInt(cursorPos.X - anchorPos.X)
	dy := vmath.FromInt(cursorPos.Y - anchorPos.Y)

	// Inside ellipse = in range (no zap)
	return vmath.EllipseContains(dx, dy, s.zapInvRxSq, s.zapInvRySq)
}

// Start zapping - spawnLightning tracked lightning
func (s *QuasarSystem) startZapping(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	cursorEntity := s.res.Cursor.Entity

	anchorPos, ok := s.world.Positions.Get(anchorEntity)
	if !ok {
		return
	}
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventLightningSpawn, &event.LightningSpawnPayload{
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
	s.quasarStore.Set(anchorEntity, *quasar)
}

// stopZapping despawns lightning
func (s *QuasarSystem) stopZapping(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	s.world.PushEvent(event.EventLightningDespawn, anchorEntity)

	quasar.IsZapping = false
	s.quasarStore.Set(anchorEntity, *quasar)
}

// Update lightning target to track cursor
func (s *QuasarSystem) updateZapTarget(quasar *component.QuasarComponent, anchorEntity core.Entity) {
	cursorEntity := s.res.Cursor.Entity
	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventLightningUpdate, &event.LightningUpdatePayload{
		Owner:   anchorEntity,
		TargetX: cursorPos.X,
		TargetY: cursorPos.Y,
	})
}

// Apply zap damage - same rate as shield overlap
func (s *QuasarSystem) applyZapDamage() {
	cursorEntity := s.res.Cursor.Entity

	shield, shieldOk := s.shieldStore.Get(cursorEntity)
	shieldActive := shieldOk && shield.Active

	if shieldActive {
		// Drain energy through shield
		s.world.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
		})
	} else {
		// Direct hit - reset heat (terminates phase)
		s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
	}
}

// processCollisionsAtNewPositions destroys entities at quasar's destination
func (s *QuasarSystem) processCollisionsAtNewPositions(anchorEntity core.Entity, anchorX, anchorY int) {
	cursorEntity := s.res.Cursor.Entity

	header, ok := s.headerStore.Get(anchorEntity)
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

			entities := s.world.Positions.GetAllAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity || memberSet[e] {
					continue
				}

				// Check protection
				if prot, ok := s.protStore.Get(e); ok {
					if prot.Mask == component.ProtectAll {
						continue
					}
				}

				// Handle nugget collision
				if s.nuggetStore.Has(e) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: e,
					})
				}

				// Handle gold composite collision
				if member, ok := s.memberStore.Get(e); ok {
					if h, hOk := s.headerStore.Get(member.AnchorID); hOk && h.BehaviorID == component.BehaviorGold {
						s.destroyGoldComposite(member.AnchorID)
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

// destroyGoldComposite handles gold sequence destruction by quasar
func (s *QuasarSystem) destroyGoldComposite(anchorID core.Entity) {
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		AnchorEntity: anchorID,
	})

	// Destroy all members
	var toDestroy []core.Entity
	for _, m := range header.Members {
		if m.Entity != 0 {
			s.memberStore.Remove(m.Entity)
			toDestroy = append(toDestroy, m.Entity)
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.res.Events.Queue, 0, toDestroy, s.res.Time.FrameNumber)
	}

	// Destroy phantom head
	s.protStore.Remove(anchorID)
	s.headerStore.Remove(anchorID)
	s.world.DestroyEntity(anchorID)
}

// handleInteractions processes shield drain and cursor collision
func (s *QuasarSystem) handleInteractions(anchorEntity core.Entity, header *component.CompositeHeaderComponent, quasar *component.QuasarComponent) {
	cursorEntity := s.res.Cursor.Entity

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	shield, shieldOk := s.shieldStore.Get(cursorEntity)
	shieldActive := shieldOk && shield.Active

	// Check each member position
	anyInShield := false
	anyOnCursor := false

	for _, m := range header.Members {
		if m.Entity == 0 {
			continue
		}

		memberPos, ok := s.world.Positions.Get(m.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && s.isInsideShieldEllipse(memberPos.X, memberPos.Y, cursorPos, &shield) {
			anyInShield = true
		}
	}

	// Update cached state
	if quasar.IsOnCursor != anyOnCursor {
		quasar.IsOnCursor = anyOnCursor
		s.quasarStore.Set(anchorEntity, *quasar)
	}

	// Shield drain (once per tick if any overlap)
	if anyInShield {
		s.world.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
		})
		return // Shield protects from direct collision
	}

	// Direct cursor collision without shield → reset heat to 0
	if anyOnCursor && !shieldActive {
		s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
		// Heat=0 will trigger termination in next Update()
	}
}

// isInsideShieldEllipse checks if position is within shield using fixed-point math
func (s *QuasarSystem) isInsideShieldEllipse(x, y int, cursorPos component.PositionComponent, shield *component.ShieldComponent) bool {
	dx := vmath.FromInt(x - cursorPos.X)
	dy := vmath.FromInt(y - cursorPos.Y)
	return vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)
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
		s.world.PushEvent(event.EventLightningDespawn, s.anchorEntity)
	}

	// Destroy composite
	if s.anchorEntity != 0 {
		s.destroyQuasarComposite(s.anchorEntity)
	}

	// End grayout
	s.res.State.State.EndGrayout()

	// Resume drain spawning
	s.world.PushEvent(event.EventDrainResume, nil)

	// Emit destroyed event (for future audio/effects)
	s.world.PushEvent(event.EventQuasarDestroyed, nil)

	s.active = false
	s.anchorEntity = 0
	s.statActive.Store(false)
}

// destroyQuasarComposite removes the quasar entity structure
func (s *QuasarSystem) destroyQuasarComposite(anchorEntity core.Entity) {
	header, ok := s.headerStore.Get(anchorEntity)
	if !ok {
		return
	}

	// Destroy all members
	for _, m := range header.Members {
		if m.Entity != 0 {
			s.memberStore.Remove(m.Entity)
			s.world.DestroyEntity(m.Entity)
		}
	}

	// Remove components from phantom head
	s.quasarStore.Remove(anchorEntity)
	s.headerStore.Remove(anchorEntity)
	s.protStore.Remove(anchorEntity)
	s.world.DestroyEntity(anchorEntity)
}