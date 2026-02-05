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
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// BuffSystem manages the cursor gained effects and abilities, it resets on energy getting to or crossing zero
type BuffSystem struct {
	world *engine.World

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
		event.EventBuffFireMainRequest,
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

	case event.EventBuffFireMainRequest:
		s.handleFireMain()

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

	// Update main fire cooldown
	if buffComp.MainFireCooldown > 0 {
		buffComp.MainFireCooldown -= dt
		if buffComp.MainFireCooldown < 0 {
			buffComp.MainFireCooldown = 0
		}
	}

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
	buffComp.Cooldown[buff] = 0 // Ready to fire immediately
	switch buff {
	case component.BuffRod:
		s.statRod.Store(true)
	case component.BuffLauncher:
		s.statLauncher.Store(true)
	case component.BuffChain:
		s.statChain.Store(true)
	default:
		return
	}

	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)

	// TEST: AddEntityAt launcher orb for multi-orb testing
	if buff == component.BuffRod && !buffComp.Active[component.BuffLauncher] {
		buffComp.Active[component.BuffLauncher] = true
		buffComp.Cooldown[component.BuffLauncher] = 0 // Ready immediately
		s.statLauncher.Store(true)
	}
}

func (s *BuffSystem) removeAllBuffs() {
	cursorEntity := s.world.Resources.Player.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}
	clear(buffComp.Active)
	clear(buffComp.Cooldown)
	clear(buffComp.Orbs)
	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)

	s.destroyAllOrbs()

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
	var sigilColor terminal.RGB
	switch buffType {
	case component.BuffRod:
		sigilColor = visual.RgbOrbRod
	case component.BuffLauncher:
		sigilColor = visual.RgbOrbLauncher
	case component.BuffChain:
		sigilColor = visual.RgbOrbChain
	}
	sigilComp := component.SigilComponent{
		Rune:  visual.CircleBullsEye,
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
		sigil.Color = visual.RgbOrbFlash
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
		sigil.Color = visual.RgbOrbRod
	case component.BuffLauncher:
		sigil.Color = visual.RgbOrbLauncher
	case component.BuffChain:
		sigil.Color = visual.RgbOrbChain
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

	event.EmitDeathOne(s.world.Resources.Event.Queue, orbEntity, 0)
}

func (s *BuffSystem) destroyAllOrbs() {
	orbEntities := s.world.Components.Orb.GetAllEntities()
	for _, orbEntity := range orbEntities {
		s.destroyOrb(orbEntity)
	}
}

func (s *BuffSystem) handleFireMain() {
	cursorEntity := s.world.Resources.Player.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	if buffComp.MainFireCooldown > 0 {
		return
	}

	// Reset cooldown
	buffComp.MainFireCooldown = parameter.BuffCooldownMainFire
	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)

	// 1. Fire Main Weapon (Cleaner)
	// Origin is current cursor position
	if pos, ok := s.world.Positions.GetPosition(cursorEntity); ok {
		s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
			OriginX: pos.X,
			OriginY: pos.Y,
		})
	}

	// 2. Fire Auxiliary Buffs
	s.fireAllBuffs()
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

	// Resolve targets once
	assignments := resolveBuffTargets(s.world, cursorPos.X, cursorPos.Y, shots)

	// GUARD: If no targets are visible, don't waste energy/cooldowns
	if len(assignments) == 0 {
		return
	}

	for buff, active := range buffComp.Active {
		if !active || buffComp.Cooldown[buff] > 0 {
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

			// Lazy resolve targets
			if assignments == nil {
				assignments = resolveBuffTargets(s.world, cursorPos.X, cursorPos.Y, shots)
			}

			// Fire lightning to targets
			for i := 0; i < min(shots, len(assignments)); i++ {
				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackLightning,
					OwnerEntity:  cursorEntity,
					OriginEntity: rodOrbEntity,
					TargetEntity: assignments[i].target,
					HitEntity:    assignments[i].hit,
				})
			}

		case component.BuffLauncher:
			// 1. Resolve targets based on current cursor position to determine if launcher should fire
			if assignments == nil {
				assignments = resolveBuffTargets(s.world, cursorPos.X, cursorPos.Y, shots)
			}

			// Do not fire and waste cooldown if no target
			if len(assignments) == 0 {
				continue
			}

			// 2. Consume cooldown and handle orb-specific origin
			buffComp.Cooldown[buff] = parameter.BuffCooldownLauncher
			launcherOrbEntity := buffComp.Orbs[component.BuffLauncher]

			// Origin of fire at launcher orb with cursor fallback
			originX, originY := cursorPos.X, cursorPos.Y
			if launcherOrbEntity != 0 {
				s.triggerOrbFlash(launcherOrbEntity)
				if orbPos, ok := s.world.Positions.GetPosition(launcherOrbEntity); ok {
					originX, originY = orbPos.X, orbPos.Y
				}
			}

			// 3. Prepare target metadata and calculate Centroid
			targets := make([]core.Entity, len(assignments))
			hits := make([]core.Entity, len(assignments))

			sumX, sumY := 0, 0
			validPosCount := 0

			for i, a := range assignments {
				targets[i] = a.target
				hits[i] = a.hit

				// Accumulate positions for centroid calculation
				if tPos, ok := s.world.Positions.GetPosition(a.target); ok {
					sumX += tPos.X
					sumY += tPos.Y
					validPosCount++
				}
			}

			// 4. Determine TargetX/Y (The "Split Point")
			var targetX, targetY int
			if validPosCount > 0 {
				// The parent missile aims for the geometric center of its intended targets
				centroidX := sumX / validPosCount
				centroidY := sumY / validPosCount
				// Split point is HALFWAY between origin and centroid for single boss target
				targetX = originX + (centroidX-originX)/2
				targetY = originY + (centroidY-originY)/2
			} else {
				// Fallback to screen center
				config := s.world.Resources.Config
				targetX = config.GameWidth / 2
				targetY = config.GameHeight / 2
			}

			// 5. Fire the request
			s.world.PushEvent(event.EventMissileSpawnRequest, &event.MissileSpawnRequestPayload{
				OwnerEntity:  cursorEntity,
				OriginEntity: launcherOrbEntity,
				OriginX:      originX,
				OriginY:      originY,
				TargetX:      targetX,
				TargetY:      targetY,
				ChildCount:   shots,
				Targets:      targets,
				HitEntities:  hits,
			})
		}
		// Update the component with new cooldowns and potentially updated orb states
		s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
	}
}

// targetAssignment holds resolved target and hit entity pair
type targetAssignment struct {
	target core.Entity // Header for composite, entity for single
	hit    core.Entity // Member entity or same as target
	dist   int64       // Distance from origin (for overflow distribution)
}

// resolveBuffTargets returns prioritized target assignments for buff abilities
// Composites first (closest member per header), then distance-sorted singles
// count: maximum assignments needed
// Returns slice of assignments, may be shorter than count if insufficient targets
func resolveBuffTargets(
	world *engine.World,
	originX, originY int,
	count int,
) []targetAssignment {
	if count <= 0 {
		return nil
	}

	cursorEntity := world.Resources.Player.Entity

	// Collect all combat entities except cursor-owned
	combatEntities := world.Components.Combat.GetAllEntities()
	candidates := make([]core.Entity, 0, len(combatEntities))
	for _, e := range combatEntities {
		combat, ok := world.Components.Combat.GetComponent(e)
		if !ok || combat.OwnerEntity == cursorEntity {
			continue
		}
		candidates = append(candidates, e)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Separate composites and singles, resolve closest member for composites
	composites := make([]targetAssignment, 0)
	singles := make([]targetAssignment, 0)

	for _, e := range candidates {
		if world.Components.Header.HasEntity(e) {
			// Composite: find closest member
			header, ok := world.Components.Header.GetComponent(e)
			if !ok {
				continue
			}
			var bestMember core.Entity
			var bestDist int64 = 1 << 62
			for _, member := range header.MemberEntries {
				pos, ok := world.Positions.GetPosition(member.Entity)
				if !ok {
					continue
				}
				d := vmath.MagnitudeEuclidean(
					vmath.FromInt(originX-pos.X),
					vmath.FromInt(originY-pos.Y),
				)
				if d < bestDist {
					bestDist = d
					bestMember = member.Entity
				}
			}
			if bestMember != 0 {
				composites = append(composites, targetAssignment{
					target: e,
					hit:    bestMember,
					dist:   bestDist,
				})
			}
		} else {
			// Single entity
			pos, ok := world.Positions.GetPosition(e)
			if !ok {
				continue
			}
			d := vmath.MagnitudeEuclidean(
				vmath.FromInt(originX-pos.X),
				vmath.FromInt(originY-pos.Y),
			)
			singles = append(singles, targetAssignment{
				target: e,
				hit:    e,
				dist:   d,
			})
		}
	}

	// Sort singles by distance
	slices.SortStableFunc(singles, func(a, b targetAssignment) int {
		return int(a.dist - b.dist)
	})

	// Build result: composites first, then singles
	result := make([]targetAssignment, 0, count)
	result = append(result, composites...)
	result = append(result, singles...)

	// Distribute overflow: if count > len(result), cycle through by distance priority
	if len(result) == 0 {
		return nil
	}

	if len(result) >= count {
		return result[:count]
	}

	// Overflow: repeat targets, prioritize composites then closest
	final := make([]targetAssignment, count)
	copy(final, result)
	for i := len(result); i < count; i++ {
		// Cycle through existing targets
		final[i] = result[i%len(result)]
	}
	return final
}