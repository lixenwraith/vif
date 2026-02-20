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

// WeaponSystem manages the cursor gained effects and abilities, it resets on energy getting to or crossing zero
type WeaponSystem struct {
	world *engine.World

	// Telemetry
	statRod       *atomic.Bool
	statRodFired  *atomic.Int64
	statLauncher  *atomic.Bool
	statDisruptor *atomic.Bool

	enabled bool
}

// NewWeaponSystem creates a new quasar system
func NewWeaponSystem(world *engine.World) engine.System {
	s := &WeaponSystem{
		world: world,
	}

	s.statRod = world.Resources.Status.Bools.Get("weapon.rod")
	s.statRodFired = world.Resources.Status.Ints.Get("weapon.rod_fired")
	s.statLauncher = world.Resources.Status.Bools.Get("weapon.launcher")
	s.statDisruptor = world.Resources.Status.Bools.Get("weapon.disruptor")

	s.Init()
	return s
}

func (s *WeaponSystem) Init() {
	s.destroyAllOrbs()
	s.statRod.Store(false)
	s.statRodFired.Store(0)
	s.statLauncher.Store(false)
	s.statDisruptor.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *WeaponSystem) Name() string {
	return "weapon"
}

func (s *WeaponSystem) Priority() int {
	return parameter.PriorityWeapon
}

func (s *WeaponSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventWeaponAddRequest,
		event.EventEnergyCrossedZeroNotification,
		event.EventWeaponFireRequest,
		event.EventWeaponFireRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *WeaponSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventWeaponAddRequest:
		if payload, ok := ev.Payload.(*event.WeaponAddRequestPayload); ok {
			s.addWeapon(payload.Weapon)
		}

	case event.EventEnergyCrossedZeroNotification:
		s.removeAllWeapons()

	case event.EventWeaponFireRequest:
		s.handleFireMain()
	}
}

func (s *WeaponSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resources.Player.Entity
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	// Update main fire cooldown
	if weaponComp.MainFireCooldown > 0 {
		weaponComp.MainFireCooldown -= dt
		if weaponComp.MainFireCooldown < 0 {
			weaponComp.MainFireCooldown = 0
		}
	}

	// Update weapon cooldowns
	for weapon, active := range weaponComp.Active {
		if !active {
			continue
		}
		weaponComp.Cooldown[weapon] -= dt
		if weaponComp.Cooldown[weapon] < 0 {
			weaponComp.Cooldown[weapon] = 0
		}
	}

	s.world.Components.Weapon.SetComponent(cursorEntity, weaponComp)

	// Update pulse effect timer
	if pulseComp, ok := s.world.Components.Pulse.GetComponent(cursorEntity); ok {
		pulseComp.Remaining -= dt
		if pulseComp.Remaining <= 0 {
			s.world.Components.Pulse.RemoveEntity(cursorEntity)
		} else {
			s.world.Components.Pulse.SetComponent(cursorEntity, pulseComp)
		}
	}

	// Ensure orbs exist for active weapons (self-healing after resize/destruction)
	s.ensureOrbs(cursorEntity)

	// Update orb motion
	s.updateOrbs()
}

func (s *WeaponSystem) addWeapon(weapon component.WeaponType) {
	cursorEntity := s.world.Resources.Player.Entity
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Initialize maps if nil
	if weaponComp.Active == nil {
		weaponComp.Active = make(map[component.WeaponType]bool)
	}
	if weaponComp.Cooldown == nil {
		weaponComp.Cooldown = make(map[component.WeaponType]time.Duration)
	}
	if weaponComp.Orbs == nil {
		weaponComp.Orbs = make(map[component.WeaponType]core.Entity)
	}

	// Skip if already active
	if weaponComp.Active[weapon] {
		return
	}

	weaponComp.Active[weapon] = true
	weaponComp.Cooldown[weapon] = 0 // Ready to fire immediately
	switch weapon {
	case component.WeaponRod:
		s.statRod.Store(true)
	case component.WeaponLauncher:
		s.statLauncher.Store(true)
	case component.WeaponDisruptor:
		s.statDisruptor.Store(true)
	default:
		return
	}

	s.world.Components.Weapon.SetComponent(cursorEntity, weaponComp)
}

func (s *WeaponSystem) removeAllWeapons() {
	cursorEntity := s.world.Resources.Player.Entity
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}
	clear(weaponComp.Active)
	clear(weaponComp.Cooldown)
	clear(weaponComp.Orbs)
	s.world.Components.Weapon.SetComponent(cursorEntity, weaponComp)

	s.destroyAllOrbs()

	s.statRod.Store(false)
	s.statLauncher.Store(false)
	s.statDisruptor.Store(false)
}

// triggerOrbFlash activates flash effect on specified orb
func (s *WeaponSystem) triggerOrbFlash(orbEntity core.Entity) {
	orbComp, ok := s.world.Components.Orb.GetComponent(orbEntity)
	if !ok {
		return
	}

	orbComp.FlashRemaining = parameter.OrbFlashDuration
	s.world.Components.Orb.SetComponent(orbEntity, orbComp)
}

// ensureOrbs creates missing orbs for active weapons and triggers redistribution if needed
func (s *WeaponSystem) ensureOrbs(cursorEntity core.Entity) {
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	if weaponComp.Orbs == nil {
		weaponComp.Orbs = make(map[component.WeaponType]core.Entity)
	}

	changed := false
	for weapon, active := range weaponComp.Active {
		if !active {
			continue
		}

		orbEntity := weaponComp.Orbs[weapon]
		if orbEntity == 0 || !s.world.Components.Orb.HasEntity(orbEntity) {
			newOrb := s.spawnOrbEntity(cursorEntity, weapon)
			weaponComp.Orbs[weapon] = newOrb
			changed = true
		}
	}

	if changed {
		s.world.Components.Weapon.SetComponent(cursorEntity, weaponComp)
		s.redistributeOrbs(cursorEntity)
	}
}

// spawnOrbEntity creates an orb entity for a weapon type
func (s *WeaponSystem) spawnOrbEntity(ownerEntity core.Entity, weaponType component.WeaponType) core.Entity {
	ownerPos, ok := s.world.Positions.GetPosition(ownerEntity)
	if !ok {
		return 0
	}

	orbEntity := s.world.CreateEntity()

	orbComp := component.OrbComponent{
		WeaponType:   weaponType,
		OwnerEntity:  ownerEntity,
		OrbitAngle:   0,
		TargetAngle:  0,
		OrbitRadiusX: parameter.OrbOrbitRadiusX,
		OrbitRadiusY: parameter.OrbOrbitRadiusY,
		OrbitSpeed:   parameter.OrbOrbitSpeed,
	}

	// Initial position at angle 0
	gridX, gridY := vmath.AngleToGridPos(0, ownerPos.X, ownerPos.Y, orbComp.OrbitRadiusX, orbComp.OrbitRadiusY)
	preciseX, preciseY := vmath.CenteredFromGrid(gridX, gridY)

	kineticComp := component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	}

	protComp := component.ProtectionComponent{
		Mask: component.ProtectFromSpecies | component.ProtectFromDecay | component.ProtectFromDelete,
	}

	s.world.Components.Protection.SetComponent(orbEntity, protComp)
	s.world.Components.Orb.SetComponent(orbEntity, orbComp)
	s.world.Components.Kinetic.SetComponent(orbEntity, kineticComp)
	s.world.Positions.SetPosition(orbEntity, component.PositionComponent{X: gridX, Y: gridY})

	return orbEntity
}

// redistributeOrbs triggers angle redistribution for all orbs
// Called when orb added/removed - actual redistribution happens in updateOrbs()
func (s *WeaponSystem) redistributeOrbs(cursorEntity core.Entity) {
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Mark all orbs for redistribution by invalidating their target angles
	// The next updateOrbs() call will calculate proper distribution
	for _, orbEntity := range weaponComp.Orbs {
		if orbEntity == 0 {
			continue
		}
		if orb, ok := s.world.Components.Orb.GetComponent(orbEntity); ok {
			orb.TargetAngle = -1 // Invalid angle forces recalculation
			s.world.Components.Orb.SetComponent(orbEntity, orb)
		}
	}
}

// updateOrbs handles orbital motion with arc-aware collision avoidance
func (s *WeaponSystem) updateOrbs() {
	dt := s.world.Resources.Time.DeltaTime
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Collect active orbs in STABLE order (sort by weapon type)
	type orbEntry struct {
		entity core.Entity
		comp   component.OrbComponent
		weapon component.WeaponType
	}
	var entries []orbEntry
	for weapon, orbEntity := range weaponComp.Orbs {
		if orbEntity == 0 {
			continue
		}
		if orb, ok := s.world.Components.Orb.GetComponent(orbEntity); ok {
			entries = append(entries, orbEntry{entity: orbEntity, comp: orb, weapon: weapon})
		}
	}

	if len(entries) == 0 {
		return
	}

	// Sort by weapon type for deterministic index assignment
	slices.SortFunc(entries, func(a, b orbEntry) int {
		return int(a.weapon) - int(b.weapon)
	})

	// Use first orb's radius (all orbs share same orbit)
	radiusX := entries[0].comp.OrbitRadiusX
	radiusY := entries[0].comp.OrbitRadiusY

	// Sample orbital ellipse for blockage
	samplePoints := vmath.SampleEllipseGrid(cursorPos.X, cursorPos.Y, radiusX, radiusY, vmath.EllipseSampleCount)
	blocked := make([]bool, len(samplePoints))
	for i, pt := range samplePoints {
		blocked[i] = !s.world.Positions.IsPointValidForOrbit(pt[0], pt[1], component.WallBlockKinetic)
	}

	// Find available arcs
	arcs := vmath.FindUnblockedArcs(blocked)
	fullCircle := vmath.IsFullCircle(arcs)

	// Distribute target angles
	targetAngles := vmath.DistributeAngles(arcs, len(entries))
	if targetAngles == nil {
		// Fully blocked - orbs stay in place
		return
	}

	// Hysteresis threshold to prevent jitter (~11 degrees)
	const angleThreshold = vmath.Scale / 32

	// Update each orb
	for i := range entries {
		orbEntity := entries[i].entity
		orb := entries[i].comp
		targetAngle := targetAngles[i]

		// Check if redistribution needed (with hysteresis)
		angleDiff := vmath.Abs(vmath.AngleDiff(orb.TargetAngle, targetAngle))
		if angleDiff > angleThreshold || orb.TargetAngle < 0 {
			orb.StartAngle = orb.OrbitAngle
			orb.TargetAngle = targetAngle
			orb.RedistributeRemaining = parameter.OrbRedistributeDuration
		}

		// Handle movement based on arc availability
		if fullCircle && orb.RedistributeRemaining <= 0 {
			// Free orbit - advance angle
			dtFixed := vmath.FromFloat(dt.Seconds())
			angleAdvance := vmath.Mul(orb.OrbitSpeed, dtFixed)
			orb.OrbitAngle = vmath.NormalizeAngle(orb.OrbitAngle + angleAdvance)
		} else if orb.RedistributeRemaining > 0 {
			// Animating to new position
			orb.RedistributeRemaining -= dt
			if orb.RedistributeRemaining <= 0 {
				orb.RedistributeRemaining = 0
				orb.OrbitAngle = orb.TargetAngle
			} else {
				t := vmath.FromFloat(1.0 - orb.RedistributeRemaining.Seconds()/parameter.OrbRedistributeDuration.Seconds())
				// Use shortest path interpolation
				diff := vmath.AngleDiff(orb.StartAngle, orb.TargetAngle)
				orb.OrbitAngle = vmath.NormalizeAngle(orb.StartAngle + vmath.Mul(diff, t))
			}
		} else {
			// Partial arc, stationary - snap to target
			orb.OrbitAngle = orb.TargetAngle
		}

		// Calculate world position from angle
		targetGridX, targetGridY := vmath.AngleToGridPos(orb.OrbitAngle, cursorPos.X, cursorPos.Y, radiusX, radiusY)

		// Get current position
		currentPos, hasPos := s.world.Positions.GetPosition(orbEntity)

		// Validate target cell is actually free (sample resolution may miss edge cases)
		targetValid := s.world.Positions.IsPointValidForOrbit(targetGridX, targetGridY, component.WallBlockKinetic)
		if !targetValid {
			// Target blocked - stay at current if valid
			if hasPos && s.world.Positions.IsPointValidForOrbit(currentPos.X, currentPos.Y, component.WallBlockKinetic) {
				targetGridX, targetGridY = currentPos.X, currentPos.Y
			} else {
				// Both invalid - skip position update, keep component state
				s.world.Components.Orb.SetComponent(orbEntity, orb)
				continue
			}
		} else if hasPos && (currentPos.X != targetGridX || currentPos.Y != targetGridY) {
			// Check if orb is isolated (can't reach target)
			pathBlocked := s.world.Positions.IsPathBlocked(
				currentPos.X, currentPos.Y,
				targetGridX, targetGridY,
				component.WallBlockKinetic,
			)
			if pathBlocked {
				// Isolated - teleport to target (no flash, reserved for firing)
				orb.OrbitAngle = targetAngle
				orb.RedistributeRemaining = 0
				targetGridX, targetGridY = vmath.AngleToGridPos(targetAngle, cursorPos.X, cursorPos.Y, radiusX, radiusY)

				// Re-validate teleport destination
				if !s.world.Positions.IsPointValidForOrbit(targetGridX, targetGridY, component.WallBlockKinetic) {
					// Teleport destination also blocked - stay put
					if hasPos {
						targetGridX, targetGridY = currentPos.X, currentPos.Y
					} else {
						s.world.Components.Orb.SetComponent(orbEntity, orb)
						continue
					}
				}
			}
		}

		// Clamp to map bounds
		targetGridX = max(0, min(targetGridX, config.MapWidth-1))
		targetGridY = max(0, min(targetGridY, config.MapHeight-1))

		// Update kinetic position
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(orbEntity); ok {
			kinetic.PreciseX, kinetic.PreciseY = vmath.CenteredFromGrid(targetGridX, targetGridY)
			s.world.Components.Kinetic.SetComponent(orbEntity, kinetic)
		}

		// Update grid position
		s.world.Positions.SetPosition(orbEntity, component.PositionComponent{X: targetGridX, Y: targetGridY})

		// Handle flash decay (flash triggered only by firing, not movement)
		if orb.FlashRemaining > 0 {
			orb.FlashRemaining -= dt
			if orb.FlashRemaining <= 0 {
				orb.FlashRemaining = 0
			}
		}

		s.world.Components.Orb.SetComponent(orbEntity, orb)
	}
}

// destroyOrb removes an orb entity and clears its reference from owner's WeaponComponent
func (s *WeaponSystem) destroyOrb(orbEntity core.Entity) {
	orbComp, ok := s.world.Components.Orb.GetComponent(orbEntity)
	if ok {
		if weaponComp, ok := s.world.Components.Weapon.GetComponent(orbComp.OwnerEntity); ok {
			if weaponComp.Orbs != nil && weaponComp.Orbs[orbComp.WeaponType] == orbEntity {
				weaponComp.Orbs[orbComp.WeaponType] = 0
				s.world.Components.Weapon.SetComponent(orbComp.OwnerEntity, weaponComp)
			}
		}
	}

	event.EmitDeathOne(s.world.Resources.Event.Queue, orbEntity, 0)
}

func (s *WeaponSystem) destroyAllOrbs() {
	orbEntities := s.world.Components.Orb.GetAllEntities()
	for _, orbEntity := range orbEntities {
		s.destroyOrb(orbEntity)
	}
}

func (s *WeaponSystem) handleFireMain() {
	cursorEntity := s.world.Resources.Player.Entity
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	if weaponComp.MainFireCooldown > 0 {
		return
	}

	// Reset cooldown
	weaponComp.MainFireCooldown = parameter.WeaponCooldownMain
	s.world.Components.Weapon.SetComponent(cursorEntity, weaponComp)

	// Determine color type from energy polarity
	colorType := component.CleanerColorPositive
	if energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		if energyComp.Current < 0 {
			colorType = component.CleanerColorNegative
		}
	}

	// Fire Main Weapon (Cleaner)
	if pos, ok := s.world.Positions.GetPosition(cursorEntity); ok {
		s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
			OriginX:   pos.X,
			OriginY:   pos.Y,
			ColorType: colorType,
		})
	}

	// Fire weapons
	s.fireAllWeapons()
}

func (s *WeaponSystem) fireAllWeapons() {
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
	weaponComp, ok := s.world.Components.Weapon.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Resolve targets once
	assignments := resolveWeaponTargets(s.world, cursorPos.X, cursorPos.Y, shots)

	// GUARD: If no targets are visible, don't waste cooldowns
	if len(assignments) == 0 {
		return
	}

	for weapon, active := range weaponComp.Active {
		if !active || weaponComp.Cooldown[weapon] > 0 {
			continue
		}

		switch weapon {
		case component.WeaponRod:
			weaponComp.Cooldown[weapon] = parameter.WeaponCooldownRod

			// Get rod orb entity and position for lightning origin
			rodOrbEntity := weaponComp.Orbs[component.WeaponRod]
			if rodOrbEntity != 0 {
				s.triggerOrbFlash(rodOrbEntity)
			}

			// Lazy resolve targets
			if assignments == nil {
				assignments = resolveWeaponTargets(s.world, cursorPos.X, cursorPos.Y, shots)
			}

			// Rod fires at unique targets only - no cycling
			// Count unique targets (assignments may have duplicates from overflow distribution)
			seen := make(map[core.Entity]bool, len(assignments))
			for _, a := range assignments {
				if seen[a.target] {
					continue
				}
				seen[a.target] = true

				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackLightning,
					OwnerEntity:  cursorEntity,
					OriginEntity: rodOrbEntity,
					TargetEntity: a.target,
					HitEntity:    a.hit,
				})
			}

		case component.WeaponLauncher:
			// 1. Resolve targets based on current cursor position to determine if launcher should fire
			if assignments == nil {
				assignments = resolveWeaponTargets(s.world, cursorPos.X, cursorPos.Y, shots)
			}

			// Do not fire and waste cooldown if no target
			if len(assignments) == 0 {
				continue
			}

			// 2. Consume cooldown and handle orb-specific origin
			weaponComp.Cooldown[weapon] = parameter.WeaponCooldownLauncher
			launcherOrbEntity := weaponComp.Orbs[component.WeaponLauncher]

			// Origin of fire at launcher orb with cursor fallback
			originX, originY := cursorPos.X, cursorPos.Y
			if launcherOrbEntity != 0 {
				s.triggerOrbFlash(launcherOrbEntity)
				if orbPos, ok := s.world.Positions.GetPosition(launcherOrbEntity); ok {
					originX, originY = orbPos.X, orbPos.Y
				}
			}

			// 3. Prepare target metadata
			targets := make([]core.Entity, len(assignments))
			hits := make([]core.Entity, len(assignments))

			for i, a := range assignments {
				targets[i] = a.target
				hits[i] = a.hit
			}

			// 4. Determine Target Direction (Most Open Space)
			// Scan 8 directions to find the path with longest free distance
			// This prevents immediate wall collisions in tight spaces
			directions := [8][2]int{
				{0, -1}, {1, -1}, {1, 0}, {1, 1},
				{0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
			}

			bestDirX, bestDirY := 0, 0
			maxFreeDistSq := int64(-1)
			scanDist := parameter.MissileClusterScanDistance

			for _, dir := range directions {
				checkX := originX + dir[0]*scanDist
				checkY := originY + dir[1]*scanDist

				endX, endY, _ := s.world.Positions.FindLastFreeOnRay(originX, originY, checkX, checkY, component.WallBlockKinetic)

				dx := vmath.FromInt(endX - originX)
				dy := vmath.FromInt(endY - originY)
				distSq := vmath.MagnitudeSq(dx, dy)

				if distSq > maxFreeDistSq {
					maxFreeDistSq = distSq
					bestDirX = dir[0]
					bestDirY = dir[1]
				}
			}

			// Set target point far in the open direction to guide velocity
			targetX := originX + bestDirX*scanDist
			targetY := originY + bestDirY*scanDist

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

		case component.WeaponDisruptor:
			if weaponComp.Cooldown[weapon] > 0 {
				continue
			}
			s.fireDisruptorWeapon(cursorEntity, cursorPos, &weaponComp)
		}
		// Update the component with new cooldowns and potentially updated orb states
		s.world.Components.Weapon.SetComponent(cursorEntity, weaponComp)
	}
}

// targetAssignment holds resolved target and hit entity pair
type targetAssignment struct {
	target core.Entity // Header for composite, entity for single
	hit    core.Entity // Member entity or same as target
	dist   int64       // Distance from origin (for overflow distribution)
}

// resolveWeaponTargets returns prioritized target assignments for weapon abilities
// Composites first (closest member per header), then distance-sorted singles
// count: maximum assignments needed
// Returns slice of assignments, may be shorter than count if insufficient targets
func resolveWeaponTargets(
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

		// Header Logic
		if headerComp, isHeader := world.Components.Header.GetComponent(e); isHeader {
			// Skip logic-only containers (e.g. Storm Root)
			if headerComp.Type == component.CompositeTypeContainer {
				continue
			}
			// Allow Unit (Swarm) and Ablative (Storm Circle)
			candidates = append(candidates, e)
			continue
		}

		// Member Logic: Skip, we target the Header
		if world.Components.Member.HasEntity(e) {
			continue
		}

		// Simple Entity (Drain)
		candidates = append(candidates, e)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Separate composites and singles, resolve closest member for composites
	composites := make([]targetAssignment, 0)
	singles := make([]targetAssignment, 0)

	for _, e := range candidates {
		if header, isHeader := world.Components.Header.GetComponent(e); isHeader {
			// Composite: find closest member for visual snap
			var bestMember core.Entity
			var bestDist int64 = 1 << 62

			// Fallback to header if no members
			if len(header.MemberEntries) == 0 {
				pos, ok := world.Positions.GetPosition(e)
				if !ok {
					continue
				}
				d := vmath.Magnitude(vmath.FromInt(originX-pos.X), vmath.FromInt(originY-pos.Y))
				composites = append(composites, targetAssignment{target: e, hit: e, dist: d})
				continue
			}

			for _, member := range header.MemberEntries {
				if member.Entity == 0 {
					continue
				}

				pos, ok := world.Positions.GetPosition(member.Entity)
				if !ok {
					continue
				}

				d := vmath.Magnitude(vmath.FromInt(originX-pos.X), vmath.FromInt(originY-pos.Y))
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
			d := vmath.Magnitude(vmath.FromInt(originX-pos.X), vmath.FromInt(originY-pos.Y))
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

	// Build result: composites first (priority), then singles
	result := make([]targetAssignment, 0, count)
	result = append(result, composites...)
	result = append(result, singles...)

	if len(result) == 0 {
		return nil
	}

	// Distribute overflow: if count > len(result), cycle through targets
	if len(result) >= count {
		return result[:count]
	}

	final := make([]targetAssignment, count)
	copy(final, result)
	for i := len(result); i < count; i++ {
		final[i] = result[i%len(result)]
	}
	return final
}

// pulseTarget holds header entity and hit members for pulse attack
type pulseTarget struct {
	header  core.Entity
	members []core.Entity
}

// findPulseTargets finds all enemies within disruptor radius of cursor
func (s *WeaponSystem) findPulseTargets(cx, cy int) []pulseTarget {
	var results []pulseTarget

	// Check drains (single entities)
	for _, entity := range s.world.Components.Drain.GetAllEntities() {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}
		if vmath.EllipseContainsPoint(pos.X, pos.Y, cx, cy, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq) {
			results = append(results, pulseTarget{header: entity, members: []core.Entity{entity}})
		}
	}

	// Check swarms (composite entities)
	for _, headerEntity := range s.world.Components.Swarm.GetAllEntities() {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		var hitMembers []core.Entity
		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberPos, ok := s.world.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}
			if vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cx, cy, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq) {
				hitMembers = append(hitMembers, member.Entity)
			}
		}
		if len(hitMembers) > 0 {
			results = append(results, pulseTarget{header: headerEntity, members: hitMembers})
		}
	}

	// Check quasar (composite entity)
	for _, headerEntity := range s.world.Components.Quasar.GetAllEntities() {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		var hitMembers []core.Entity
		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberPos, ok := s.world.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}
			if vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cx, cy, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq) {
				hitMembers = append(hitMembers, member.Entity)
			}
		}
		if len(hitMembers) > 0 {
			results = append(results, pulseTarget{header: headerEntity, members: hitMembers})
		}
	}

	// Check storm circles (each circle is independent header with StormCircleComponent)
	for _, rootEntity := range s.world.Components.Storm.GetAllEntities() {
		stormComp, ok := s.world.Components.Storm.GetComponent(rootEntity)
		if !ok {
			continue
		}

		for i := 0; i < component.StormCircleCount; i++ {
			if !stormComp.CirclesAlive[i] {
				continue
			}

			circleEntity := stormComp.Circles[i]
			headerComp, ok := s.world.Components.Header.GetComponent(circleEntity)
			if !ok {
				continue
			}

			var hitMembers []core.Entity
			for _, member := range headerComp.MemberEntries {
				if member.Entity == 0 {
					continue
				}
				memberPos, ok := s.world.Positions.GetPosition(member.Entity)
				if !ok {
					continue
				}
				if vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cx, cy, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq) {
					hitMembers = append(hitMembers, member.Entity)
				}
			}
			if len(hitMembers) > 0 {
				results = append(results, pulseTarget{header: circleEntity, members: hitMembers})
			}
		}
	}

	// Check pylons (ablative composite entities)
	for _, headerEntity := range s.world.Components.Pylon.GetAllEntities() {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		var hitMembers []core.Entity
		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberPos, ok := s.world.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}
			if vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cx, cy, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq) {
				hitMembers = append(hitMembers, member.Entity)
			}
		}
		if len(hitMembers) > 0 {
			results = append(results, pulseTarget{header: headerEntity, members: hitMembers})
		}
	}

	return results
}

// fireDisruptorWeapon fires the disruptor pulse stun weapon
func (s *WeaponSystem) fireDisruptorWeapon(cursorEntity core.Entity, cursorPos component.PositionComponent, weaponComp *component.WeaponComponent) {
	// Find targets in radius
	targets := s.findPulseTargets(cursorPos.X, cursorPos.Y)
	if len(targets) == 0 {
		return
	}

	// Consume cooldown
	weaponComp.Cooldown[component.WeaponDisruptor] = parameter.WeaponCooldownDisruptor

	// Visual orb flash
	if disruptorOrbEntity := weaponComp.Orbs[component.WeaponDisruptor]; disruptorOrbEntity != 0 {
		s.triggerOrbFlash(disruptorOrbEntity)
	}

	// Emit area attack per target
	for _, target := range targets {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackPulse,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: target.header,
			HitEntities:  target.members,
			OriginX:      cursorPos.X,
			OriginY:      cursorPos.Y,
		})
	}

	// Set pulse effect on cursor for visual feedback
	s.world.Components.Pulse.SetComponent(cursorEntity, component.PulseComponent{
		OriginX:   cursorPos.X,
		OriginY:   cursorPos.Y,
		Duration:  parameter.PulseEffectDuration,
		Remaining: parameter.PulseEffectDuration,
	})
}