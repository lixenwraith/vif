package system

import (
	"slices"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// BuffSystem manages the cursor gained effects and abilities, it resets on energy getting to or crossing zero
type BuffSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Telemetry
	statRod      *atomic.Bool
	statRodFired *atomic.Int64
	statLauncher *atomic.Bool
	statChain    *atomic.Bool

	enabled bool
}

// NewBuffSystem creates a new quasar system
func NewBuffSystem(world *engine.World) engine.System {
	s := &BuffSystem{
		world: world,
	}

	s.statRod = world.Resources.Status.Bools.Get("buff.rod")
	s.statRodFired = world.Resources.Status.Ints.Get("buff.rod_fired")
	s.statLauncher = world.Resources.Status.Bools.Get("buff.launcher")
	s.statChain = world.Resources.Status.Bools.Get("buff.chain")

	s.Init()
	return s
}

func (s *BuffSystem) Init() {
	s.destroyAllOrbs()
	s.statRod.Store(false)
	s.statRodFired.Store(0)
	s.statLauncher.Store(false)
	s.statChain.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *BuffSystem) Name() string {
	return "buff"
}

func (s *BuffSystem) Priority() int {
	return parameter.PriorityBuff
}

func (s *BuffSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBuffAddRequest,
		event.EventEnergyCrossedZeroNotification,
		event.EventBuffFireRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *BuffSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventBuffAddRequest:
		if payload, ok := ev.Payload.(*event.BuffAddRequestPayload); ok {
			s.addBuff(payload.Buff)
		}

	case event.EventEnergyCrossedZeroNotification:
		s.removeAllBuffs()

	case event.EventBuffFireRequest:
		s.fireAllBuffs()
	}
}

func (s *BuffSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resources.Player.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	// Update buff cooldowns
	for buff, active := range buffComp.Active {
		if !active {
			continue
		}
		buffComp.Cooldown[buff] -= dt
		if buffComp.Cooldown[buff] < 0 {
			buffComp.Cooldown[buff] = 0
		}
	}
	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)

	// Ensure orbs exist for active buffs (self-healing after resize/destruction)
	s.ensureOrbs(cursorEntity)

	// Update orb motion
	s.updateOrbs()
}

func (s *BuffSystem) addBuff(buff component.BuffType) {
	cursorEntity := s.world.Resources.Player.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Initialize maps if nil
	if buffComp.Active == nil {
		buffComp.Active = make(map[component.BuffType]bool)
	}
	if buffComp.Cooldown == nil {
		buffComp.Cooldown = make(map[component.BuffType]time.Duration)
	}
	if buffComp.Orbs == nil {
		buffComp.Orbs = make(map[component.BuffType]core.Entity)
	}

	// Skip if already active
	if buffComp.Active[buff] {
		return
	}

	buffComp.Active[buff] = true
	switch buff {
	case component.BuffRod:
		buffComp.Cooldown[buff] = parameter.BuffCooldownRod
		s.statRod.Store(true)
	case component.BuffLauncher:
		buffComp.Cooldown[buff] = parameter.BuffCooldownLauncher
		s.statLauncher.Store(true)
	case component.BuffChain:
		buffComp.Cooldown[buff] = parameter.BuffCooldownChain
		s.statChain.Store(true)
	default:
		return
	}

	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)

	// TEST: Add launcher orb for multi-orb testing
	if buff == component.BuffRod && !buffComp.Active[component.BuffLauncher] {
		buffComp.Active[component.BuffLauncher] = true
		buffComp.Cooldown[component.BuffLauncher] = parameter.BuffCooldownLauncher
		s.statLauncher.Store(true)
	}
}

func (s *BuffSystem) removeAllBuffs() {
	cursorEntity := s.world.Resources.Player.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Destroy all orb entities
	for _, orbEntity := range buffComp.Orbs {
		if orbEntity != 0 {
			s.world.Components.Orb.RemoveEntity(orbEntity)
			s.world.Components.Kinetic.RemoveEntity(orbEntity)
			s.world.Components.Sigil.RemoveEntity(orbEntity)
			s.world.Positions.RemoveEntity(orbEntity)
			s.world.DestroyEntity(orbEntity)
		}
	}

	clear(buffComp.Active)
	clear(buffComp.Cooldown)
	clear(buffComp.Orbs)
	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
	s.statRod.Store(false)
	s.statLauncher.Store(false)
	s.statChain.Store(false)
}

// spawnOrbEntity creates an orb entity for a buff type
func (s *BuffSystem) spawnOrbEntity(ownerEntity core.Entity, buffType component.BuffType) core.Entity {
	ownerPos, ok := s.world.Positions.GetPosition(ownerEntity)
	if !ok {
		return 0
	}

	orbEntity := s.world.CreateEntity()

	// Initial angle will be set by redistribution
	orbComp := component.OrbComponent{
		BuffType:     buffType,
		OwnerEntity:  ownerEntity,
		OrbitAngle:   0,
		OrbitRadiusX: parameter.OrbOrbitRadiusX,
		OrbitRadiusY: parameter.OrbOrbitRadiusY,
		OrbitSpeed:   parameter.OrbOrbitSpeed,
	}

	// Kinetic for position tracking
	ownerCenterX, ownerCenterY := vmath.CenteredFromGrid(ownerPos.X, ownerPos.Y)
	kineticComp := component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: ownerCenterX + orbComp.OrbitRadiusX, // Start at angle 0
			PreciseY: ownerCenterY,
		},
	}

	// Sigil for rendering
	var sigilColor component.SigilColor
	switch buffType {
	case component.BuffRod:
		sigilColor = component.SigilOrbRod
	case component.BuffLauncher:
		sigilColor = component.SigilOrbLauncher
	case component.BuffChain:
		sigilColor = component.SigilOrbChain
	}
	sigilComp := component.SigilComponent{
		Rune:  parameter.OrbChar,
		Color: sigilColor,
	}

	// Position component for grid-based queries
	gridX := vmath.ToInt(kineticComp.PreciseX)
	gridY := vmath.ToInt(kineticComp.PreciseY)
	posComp := component.PositionComponent{X: gridX, Y: gridY}

	// Protect orb from game interactions (drain, decay, etc.)
	protComp := component.ProtectionComponent{
		Mask: component.ProtectFromDrain | component.ProtectFromDecay | component.ProtectFromDelete,
	}

	s.world.Components.Protection.SetComponent(orbEntity, protComp)
	s.world.Components.Orb.SetComponent(orbEntity, orbComp)
	s.world.Components.Kinetic.SetComponent(orbEntity, kineticComp)
	s.world.Components.Sigil.SetComponent(orbEntity, sigilComp)
	s.world.Positions.SetPosition(orbEntity, posComp)

	return orbEntity
}

// redistributeOrbs triggers angle redistribution for all orbs owned by cursor
func (s *BuffSystem) redistributeOrbs(cursorEntity core.Entity) {
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Collect active orb entities
	var activeOrbs []core.Entity
	for _, orbEntity := range buffComp.Orbs {
		if orbEntity != 0 {
			activeOrbs = append(activeOrbs, orbEntity)
		}
	}

	orbCount := len(activeOrbs)
	if orbCount == 0 {
		return
	}

	// Calculate evenly distributed target angles
	angleStep := vmath.Scale / int64(orbCount)

	for i, orbEntity := range activeOrbs {
		orbComp, ok := s.world.Components.Orb.GetComponent(orbEntity)
		if !ok {
			continue
		}

		targetAngle := int64(i) * angleStep
		orbComp.StartAngle = orbComp.OrbitAngle
		orbComp.TargetAngle = targetAngle
		orbComp.RedistributeRemaining = parameter.OrbRedistributeDuration

		s.world.Components.Orb.SetComponent(orbEntity, orbComp)
	}
}

// triggerOrbFlash activates flash effect on specified orb
func (s *BuffSystem) triggerOrbFlash(orbEntity core.Entity) {
	orbComp, ok := s.world.Components.Orb.GetComponent(orbEntity)
	if !ok {
		return
	}

	orbComp.FlashRemaining = parameter.OrbFlashDuration
	s.world.Components.Orb.SetComponent(orbEntity, orbComp)

	// Set flash color
	sigil, ok := s.world.Components.Sigil.GetComponent(orbEntity)
	if ok {
		sigil.Color = component.SigilOrbFlash
		s.world.Components.Sigil.SetComponent(orbEntity, sigil)
	}
}

// ensureOrbs creates missing orbs for active buffs and triggers redistribution if needed
func (s *BuffSystem) ensureOrbs(cursorEntity core.Entity) {
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	if buffComp.Orbs == nil {
		buffComp.Orbs = make(map[component.BuffType]core.Entity)
	}

	changed := false
	for buff, active := range buffComp.Active {
		if !active {
			continue
		}

		orbEntity := buffComp.Orbs[buff]
		// Check if orb exists and is valid
		if orbEntity == 0 || !s.world.Components.Orb.HasEntity(orbEntity) {
			newOrb := s.spawnOrbEntity(cursorEntity, buff)
			buffComp.Orbs[buff] = newOrb
			changed = true
		}
	}

	if changed {
		s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
		s.redistributeOrbs(cursorEntity)
	}
}

// updateOrbs handles orbital motion, boundary clamping (slide), and flash decay
func (s *BuffSystem) updateOrbs() {
	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	config := s.world.Resources.Config

	orbEntities := s.world.Components.Orb.GetAllEntities()
	for _, orbEntity := range orbEntities {
		orbComp, ok := s.world.Components.Orb.GetComponent(orbEntity)
		if !ok {
			continue
		}

		// Get owner cursor position (centered)
		ownerPos, ok := s.world.Positions.GetPosition(orbComp.OwnerEntity)
		if !ok {
			s.destroyOrb(orbEntity)
			continue
		}
		ownerCenterX, ownerCenterY := vmath.CenteredFromGrid(ownerPos.X, ownerPos.Y)

		// Handle redistribution animation
		if orbComp.RedistributeRemaining > 0 {
			orbComp.RedistributeRemaining -= dt
			if orbComp.RedistributeRemaining <= 0 {
				orbComp.RedistributeRemaining = 0
				orbComp.OrbitAngle = orbComp.TargetAngle
			} else {
				totalDuration := parameter.OrbRedistributeDuration
				elapsed := totalDuration - orbComp.RedistributeRemaining
				t := vmath.FromFloat(elapsed.Seconds() / totalDuration.Seconds())
				orbComp.OrbitAngle = vmath.Lerp(orbComp.StartAngle, orbComp.TargetAngle, t)
			}
		} else {
			// Normal orbital motion
			angleAdvance := vmath.Mul(orbComp.OrbitSpeed, dtFixed)
			orbComp.OrbitAngle += angleAdvance
			// Wrap angle to [0, Scale)
			for orbComp.OrbitAngle >= vmath.Scale {
				orbComp.OrbitAngle -= vmath.Scale
			}
			for orbComp.OrbitAngle < 0 {
				orbComp.OrbitAngle += vmath.Scale
			}
		}

		// Compute ideal orbital position
		cosA := vmath.Cos(orbComp.OrbitAngle)
		sinA := vmath.Sin(orbComp.OrbitAngle)
		idealX := ownerCenterX + vmath.Mul(cosA, orbComp.OrbitRadiusX)
		idealY := ownerCenterY + vmath.Mul(sinA, orbComp.OrbitRadiusY)

		// Clamp to game bounds (slide along edge)
		minX := int64(vmath.CellCenter)
		maxX := vmath.FromInt(config.GameWidth-1) + vmath.CellCenter
		minY := int64(vmath.CellCenter)
		maxY := vmath.FromInt(config.GameHeight-1) + vmath.CellCenter

		if idealX < minX {
			idealX = minX
		} else if idealX > maxX {
			idealX = maxX
		}
		if idealY < minY {
			idealY = minY
		} else if idealY > maxY {
			idealY = maxY
		}

		// Update Kinetic position
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(orbEntity)
		if ok {
			kineticComp.PreciseX = idealX
			kineticComp.PreciseY = idealY
			s.world.Components.Kinetic.SetComponent(orbEntity, kineticComp)
		}

		// Sync grid position
		gridX, gridY := vmath.ToInt(idealX), vmath.ToInt(idealY)
		s.world.Positions.SetPosition(orbEntity, component.PositionComponent{X: gridX, Y: gridY})

		// Handle flash decay
		if orbComp.FlashRemaining > 0 {
			orbComp.FlashRemaining -= dt
			if orbComp.FlashRemaining <= 0 {
				orbComp.FlashRemaining = 0
				s.restoreOrbColor(orbEntity, orbComp.BuffType)
			}
		}

		s.world.Components.Orb.SetComponent(orbEntity, orbComp)
	}
}

// restoreOrbColor sets orb sigil back to normal color after flash
func (s *BuffSystem) restoreOrbColor(orbEntity core.Entity, buffType component.BuffType) {
	sigil, ok := s.world.Components.Sigil.GetComponent(orbEntity)
	if !ok {
		return
	}

	switch buffType {
	case component.BuffRod:
		sigil.Color = component.SigilOrbRod
	case component.BuffLauncher:
		sigil.Color = component.SigilOrbLauncher
	case component.BuffChain:
		sigil.Color = component.SigilOrbChain
	}

	s.world.Components.Sigil.SetComponent(orbEntity, sigil)
}

// destroyOrb removes an orb entity and clears its reference from owner's BuffComponent
func (s *BuffSystem) destroyOrb(orbEntity core.Entity) {
	orbComp, ok := s.world.Components.Orb.GetComponent(orbEntity)
	if ok {
		if buffComp, ok := s.world.Components.Buff.GetComponent(orbComp.OwnerEntity); ok {
			if buffComp.Orbs != nil && buffComp.Orbs[orbComp.BuffType] == orbEntity {
				buffComp.Orbs[orbComp.BuffType] = 0
				s.world.Components.Buff.SetComponent(orbComp.OwnerEntity, buffComp)
			}
		}
	}

	s.world.Components.Orb.RemoveEntity(orbEntity)
	s.world.Components.Kinetic.RemoveEntity(orbEntity)
	s.world.Components.Sigil.RemoveEntity(orbEntity)
	s.world.Components.Protection.RemoveEntity(orbEntity)
	s.world.Positions.RemoveEntity(orbEntity)
	s.world.DestroyEntity(orbEntity)
}

func (s *BuffSystem) destroyAllOrbs() {
	orbEntities := s.world.Components.Orb.GetAllEntities()
	for _, orbEntity := range orbEntities {
		s.destroyOrb(orbEntity)
	}
}

func (s *BuffSystem) fireAllBuffs() {
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}
	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}
	shots := heatComp.Current / 10
	if shots == 0 {
		return
	}
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	for buff, active := range buffComp.Active {
		if !active {
			continue
		}

		if buffComp.Cooldown[buff] > 0 {
			continue
		}

		switch buff {
		case component.BuffRod:
			buffComp.Cooldown[buff] = parameter.BuffCooldownRod

			// Get rod orb entity and position for lightning origin
			rodOrbEntity := buffComp.Orbs[component.BuffRod]
			if rodOrbEntity != 0 {
				s.triggerOrbFlash(rodOrbEntity)
			}

			// 1. Filter eligible targets
			combatEntities := s.world.Components.Combat.GetAllEntities()
			candidateTargetEntities := make([]core.Entity, 0, len(combatEntities))
			for _, combatEntity := range combatEntities {
				combatComp, ok := s.world.Components.Combat.GetComponent(combatEntity)
				if !ok {
					continue
				}
				if combatComp.OwnerEntity == cursorEntity {
					continue
				}
				candidateTargetEntities = append(candidateTargetEntities, combatEntity)
			}

			// 2. Prioritize composite targets
			compositeIndex := 0
			for scanIndex := range len(candidateTargetEntities) {
				if s.world.Components.Header.HasEntity(candidateTargetEntities[scanIndex]) {
					if scanIndex != compositeIndex {
						candidateTargetEntities[scanIndex], candidateTargetEntities[compositeIndex] = candidateTargetEntities[compositeIndex], candidateTargetEntities[scanIndex]
					}
					compositeIndex++
				}
			}

			// 3. Process composites (closest member per header)
			finalTargetEntities := make([]core.Entity, 0, shots)
			finalHitEntities := make([]core.Entity, 0, shots)
			for i := range min(shots, compositeIndex) {
				headerComp, ok := s.world.Components.Header.GetComponent(candidateTargetEntities[i])
				if !ok {
					continue
				}
				var hitEntityCandidate core.Entity
				var shortestMemberDistance int64
				for _, memberEntry := range headerComp.MemberEntries {
					memberPos, ok := s.world.Positions.GetPosition(memberEntry.Entity)
					if !ok {
						continue
					}
					memberDistance := vmath.MagnitudeEuclidean(
						vmath.FromInt(cursorPos.X-memberPos.X),
						vmath.FromInt(cursorPos.Y-memberPos.Y),
					)
					if hitEntityCandidate == 0 || memberDistance < shortestMemberDistance {
						hitEntityCandidate = memberEntry.Entity
						shortestMemberDistance = memberDistance
					}
				}
				if hitEntityCandidate != 0 {
					finalTargetEntities = append(finalTargetEntities, candidateTargetEntities[i])
					finalHitEntities = append(finalHitEntities, hitEntityCandidate)
				}
			}

			// 4. Fill the rest with closest non-composite entities
			remaining := shots - len(finalTargetEntities)
			if remaining > 0 && compositeIndex < len(candidateTargetEntities) {
				type entityDistance struct {
					entity   core.Entity
					distance int64
				}
				nonComposites := make([]entityDistance, 0, len(candidateTargetEntities)-compositeIndex)
				for i := compositeIndex; i < len(candidateTargetEntities); i++ {
					targetPos, ok := s.world.Positions.GetPosition(candidateTargetEntities[i])
					if !ok {
						continue
					}
					nonComposites = append(nonComposites, entityDistance{
						entity: candidateTargetEntities[i],
						distance: vmath.MagnitudeEuclidean(
							vmath.FromInt(cursorPos.X-targetPos.X),
							vmath.FromInt(cursorPos.Y-targetPos.Y),
						),
					})
				}
				slices.SortStableFunc(nonComposites, func(a, b entityDistance) int {
					return int(a.distance - b.distance)
				})
				// Bound by actual available non-composites
				for i := range min(remaining, len(nonComposites)) {
					finalTargetEntities = append(finalTargetEntities, nonComposites[i].entity)
					finalHitEntities = append(finalHitEntities, nonComposites[i].entity)
				}
			}

			// 5. Fire lightning to targets
			for i := range len(finalTargetEntities) {
				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackLightning,
					OwnerEntity:  cursorEntity,
					OriginEntity: rodOrbEntity,
					TargetEntity: finalTargetEntities[i],
					HitEntity:    finalHitEntities[i],
				})
			}
		}
		s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
	}
}