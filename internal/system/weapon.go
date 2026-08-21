package system

import (
	"slices"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// WeaponSystem manages per-cursor weapon loadouts, orbs and firing.
// A loadout resets when its own cursor's energy crosses zero.
type WeaponSystem struct {
	world *engine.World

	// Per-cursor loadout
	statRod       *status.PlayerBool
	statLauncher  *status.PlayerBool
	statDisruptor *status.PlayerBool
	statOrbs      *status.PlayerInt

	// Roster-wide fire counters
	statMainFired      *atomic.Int64
	statRodFired       *atomic.Int64
	statLauncherFired  *atomic.Int64
	statDisruptorFired *atomic.Int64
	rejects            rejectionTelemetry

	enabled bool
}

// NewWeaponSystem creates a new weapon system
func NewWeaponSystem(world *engine.World) engine.System {
	s := &WeaponSystem{world: world}

	reg := world.Resources.Status
	s.statRod = status.NewPlayerBool(reg, parameter.MaxPlayers, "weapon.rod", "weapon.rod")
	s.statLauncher = status.NewPlayerBool(reg, parameter.MaxPlayers, "weapon.launcher", "weapon.launcher")
	s.statDisruptor = status.NewPlayerBool(reg, parameter.MaxPlayers, "weapon.disruptor", "weapon.disruptor")
	s.statOrbs = status.NewPlayerInt(reg, parameter.MaxPlayers, "weapon.orbs", "weapon.orbs")

	s.statMainFired = reg.Ints.Get("weapon.main_fired")
	s.statRodFired = reg.Ints.Get("weapon.rod_fired")
	s.statLauncherFired = reg.Ints.Get("weapon.launcher_fired")
	s.statDisruptorFired = reg.Ints.Get("weapon.disruptor_fired")
	s.rejects = newRejectionTelemetry(reg, "weapon")

	s.Init()
	return s
}

// Init resets session state for a new game, dropping every orb in the world
func (s *WeaponSystem) Init() {
	s.destroyAllOrbs()
	s.statRod.Reset()
	s.statLauncher.Reset()
	s.statDisruptor.Reset()
	s.statOrbs.Reset()
	s.statMainFired.Store(0)
	s.statRodFired.Store(0)
	s.statLauncherFired.Store(0)
	s.statDisruptorFired.Store(0)
	s.rejects.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *WeaponSystem) Name() string { return "weapon" }

// Priority returns the system's priority
func (s *WeaponSystem) Priority() int { return parameter.PriorityWeapon }

// EventTypes returns the event types WeaponSystem handles
func (s *WeaponSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventWeaponAddRequest,
		event.EventEnergyCrossedZero,
		event.EventWeaponFireRequest,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes weapon commands, each naming the cursor it acts on
func (s *WeaponSystem) HandleEvent(ev event.GameEvent) {
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

	if !s.enabled {
		if ev.Type != event.EventMetaSystemCommandRequest {
			s.rejects.disabled.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventCursorDespawned:
		if p, ok := ev.Payload.(*event.CursorDespawnedPayload); ok {
			s.clearSlot(p.Slot)
		}
		return

	case event.EventEnergyCrossedZero:
		// Notification: it already names the cursor whose energy changed sign
		if p, ok := ev.Payload.(*event.EnergyCrossedZeroPayload); ok {
			if cursor := s.world.ResolveCursor(p.Entity); cursor != 0 {
				s.removeAllWeapons(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}
		return
	}

	switch ev.Type {
	case event.EventWeaponAddRequest:
		if payload, ok := ev.Payload.(*event.WeaponAddRequestPayload); ok {
			cursor := s.world.ResolveCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.addWeapon(cursor, payload.Weapon)
		}

	case event.EventWeaponFireRequest:
		if payload, ok := ev.Payload.(*event.WeaponFireRequestPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.handleFireMain(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}
	}
}

// Update advances cooldowns, pulse and orbit for every cursor
func (s *WeaponSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	s.world.Components.Cursor.Each(func(cursor core.Entity, _ *component.CursorComponent) bool {
		weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
		if !ok {
			return true
		}

		// Update main fire cooldown
		if weaponComp.MainFireCooldown > 0 {
			weaponComp.MainFireCooldown = max(weaponComp.MainFireCooldown-dt, 0)
		}

		// Update weapon cooldowns
		for wt := range weaponComp.Charges {
			if weaponComp.Charges[wt] <= 0 {
				continue
			}
			weaponComp.Cooldown[wt] = max(weaponComp.Cooldown[wt]-dt, 0)
		}

		// Update pulse effect timer
		if pulseComp, ok := s.world.Components.Pulse.GetPtr(cursor); ok {
			pulseComp.Remaining -= dt
			if pulseComp.Remaining <= 0 {
				s.world.Components.Pulse.RemoveEntity(cursor, false)
			}
		}

		// Ensure orbs exist for active weapons (self-healing after resize/destruction)
		s.ensureOrbs(cursor, weaponComp)

		// Update orb motion around this owner
		s.updateOrbs(cursor, weaponComp)
		return true
	})
}

// addWeapon grants or recharges one weapon on one cursor
func (s *WeaponSystem) addWeapon(cursor core.Entity, weapon component.WeaponType) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok {
		return
	}

	firstAcquire := weaponComp.Charges[weapon] == 0
	if maxCharge := parameter.WeaponMaxCharges[weapon]; weaponComp.Charges[weapon] < maxCharge {
		weaponComp.Charges[weapon]++
	}

	if firstAcquire {
		weaponComp.Cooldown[weapon] = 0 // Ready to fire immediately on first pickup
		s.publishLoadout(cursor, weaponComp)
	}
}

// removeAllWeapons strips one cursor's loadout and destroys only its own orbs
func (s *WeaponSystem) removeAllWeapons(cursor core.Entity) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok {
		return
	}

	s.destroyCursorOrbs(weaponComp)

	weaponComp.Charges = [component.WeaponCount]int{}
	weaponComp.Cooldown = [component.WeaponCount]time.Duration{}

	s.publishLoadout(cursor, weaponComp)
	if slot, ok := s.world.CursorSlot(cursor); ok {
		s.statOrbs.Store(slot, 0)
	}
}

// triggerOrbFlash activates flash effect on specified orb
func (s *WeaponSystem) triggerOrbFlash(orbEntity core.Entity) {
	orbComp, ok := s.world.Components.Orb.GetPtr(orbEntity)
	if !ok {
		return
	}

	orbComp.FlashRemaining = parameter.OrbFlashDuration
}

// ensureOrbs creates missing orbs for one cursor's active weapons and triggers redistribution
func (s *WeaponSystem) ensureOrbs(cursor core.Entity, weaponComp *component.WeaponComponent) {
	changed := false
	for wt := range weaponComp.Charges {
		if weaponComp.Charges[wt] <= 0 {
			continue
		}

		orbEntity := weaponComp.Orbs[wt]
		if orbEntity == 0 || !s.world.Components.Orb.HasEntity(orbEntity) {
			weaponComp.Orbs[wt] = s.spawnOrbEntity(cursor, component.WeaponType(wt))
			changed = true
		}
	}

	if changed {
		s.redistributeOrbs(weaponComp)
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
	gridX, gridY := vmath.AngleToGridPosF(0, ownerPos.X, ownerPos.Y, orbComp.OrbitRadiusX, orbComp.OrbitRadiusY)
	preciseX, preciseY := vmath.Point{X: gridX, Y: gridY}.CenterF()

	kineticComp := component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	}

	protComp := component.ProtectionComponent{
		Mask: component.ProtectFromSpecies | component.ProtectFromDecay,
	}

	s.world.Components.Protection.SetComponent(orbEntity, protComp)
	s.world.Components.Orb.SetComponent(orbEntity, orbComp)
	s.world.Components.Kinetic.SetComponent(orbEntity, kineticComp)
	s.world.Positions.SetPosition(orbEntity, component.PositionComponent{X: gridX, Y: gridY})

	return orbEntity
}

// redistributeOrbs invalidates one cursor's orb target angles; updateOrbs recalculates
func (s *WeaponSystem) redistributeOrbs(weaponComp *component.WeaponComponent) {
	for _, orbEntity := range weaponComp.Orbs {
		if orbEntity == 0 {
			continue
		}
		if orb, ok := s.world.Components.Orb.GetPtr(orbEntity); ok {
			orb.TargetAngle = -1 // Invalid angle forces recalculation
		}
	}
}

// updateOrbs handles one cursor's orbital motion with arc-aware collision avoidance
func (s *WeaponSystem) updateOrbs(cursor core.Entity, weaponComp *component.WeaponComponent) {
	dt := s.world.Resources.Time.DeltaTime
	config := s.world.Resources.Config

	cursorPos, ok := s.world.Positions.GetPosition(cursor)
	if !ok {
		return
	}

	slot, hasSlot := s.world.CursorSlot(cursor)

	// Collect active orbs in STABLE order (sort by weapon type)
	type orbEntry struct {
		entity core.Entity
		weapon component.WeaponType
	}
	var entries []orbEntry
	for weapon, orbEntity := range weaponComp.Orbs {
		if orbEntity == 0 {
			continue
		}
		if s.world.Components.Orb.HasEntity(orbEntity) {
			entries = append(entries, orbEntry{entity: orbEntity, weapon: component.WeaponType(weapon)})
		}
	}

	if len(entries) == 0 {
		if hasSlot {
			s.statOrbs.Store(slot, 0)
		}
		return
	}
	if hasSlot {
		s.statOrbs.Store(slot, int64(len(entries)))
	}

	// Sort by weapon type for deterministic index assignment
	slices.SortFunc(entries, func(a, b orbEntry) int {
		return int(a.weapon) - int(b.weapon)
	})

	// Use first orb's radius (all of one owner's orbs share the same orbit).
	firstOrb, ok := s.world.Components.Orb.GetPtr(entries[0].entity)
	if !ok {
		return
	}
	radiusX := firstOrb.OrbitRadiusX
	radiusY := firstOrb.OrbitRadiusY

	// Sample orbital ellipse for blockage
	samplePoints := vmath.SampleEllipseGridF(cursorPos.X, cursorPos.Y, radiusX, radiusY, vmath.EllipseSampleCount)
	blocked := make([]bool, len(samplePoints))
	for i, pt := range samplePoints {
		blocked[i] = !s.world.Positions.IsPointValidForOrbit(pt[0], pt[1], component.WallBlockKinetic)
	}

	// Find available arcs
	arcs := vmath.FindUnblockedArcsF(blocked)
	fullCircle := vmath.IsFullCircleF(arcs)

	// Distribute target angles
	targetAngles := vmath.DistributeAnglesF(arcs, len(entries))
	if targetAngles == nil {
		// Fully blocked - orbs stay in place
		return
	}

	// Hysteresis threshold to prevent jitter (~11 degrees)
	const angleThreshold = vmath.TwoPi / 32

	// Update each orb
	for i := range entries {
		orbEntity := entries[i].entity
		orb, ok := s.world.Components.Orb.GetPtr(orbEntity)
		if !ok {
			continue
		}
		targetAngle := targetAngles[i]

		// Check if redistribution needed (with hysteresis)
		angleDiff := vmath.AbsF(vmath.AngleDiffF(orb.TargetAngle, targetAngle))
		if angleDiff > angleThreshold || orb.TargetAngle < 0 { // TargetAngle -1 is sentinel, never a returned value
			orb.StartAngle = orb.OrbitAngle
			orb.TargetAngle = targetAngle
			orb.RedistributeRemaining = parameter.OrbRedistributeDuration
		}

		// Handle movement based on arc availability
		if fullCircle && orb.RedistributeRemaining <= 0 {
			// Free orbit - advance angle
			orb.OrbitAngle = vmath.NormalizeAngleF(orb.OrbitAngle + orb.OrbitSpeed*dt.Seconds())
		} else if orb.RedistributeRemaining > 0 {
			// Animating to new position
			orb.RedistributeRemaining -= dt
			if orb.RedistributeRemaining <= 0 {
				orb.RedistributeRemaining = 0
				orb.OrbitAngle = orb.TargetAngle
			} else {
				t := 1.0 - orb.RedistributeRemaining.Seconds()/parameter.OrbRedistributeDuration.Seconds()
				// Use shortest path interpolation
				diff := vmath.AngleDiffF(orb.StartAngle, orb.TargetAngle)
				orb.OrbitAngle = vmath.NormalizeAngleF(orb.StartAngle + diff*t)
			}
		} else {
			// Partial arc, stationary - snap to target
			orb.OrbitAngle = orb.TargetAngle
		}

		// Calculate world position from angle
		targetGridX, targetGridY := vmath.AngleToGridPosF(orb.OrbitAngle, cursorPos.X, cursorPos.Y, radiusX, radiusY)

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
				targetGridX, targetGridY = vmath.AngleToGridPosF(targetAngle, cursorPos.X, cursorPos.Y, radiusX, radiusY)

				// Re-validate teleport destination
				if !s.world.Positions.IsPointValidForOrbit(targetGridX, targetGridY, component.WallBlockKinetic) {
					// Teleport destination also blocked - stay put
					if hasPos {
						targetGridX, targetGridY = currentPos.X, currentPos.Y
					} else {
						continue
					}
				}
			}
		}

		// Clamp to map bounds
		targetGridX = max(0, min(targetGridX, config.MapWidth-1))
		targetGridY = max(0, min(targetGridY, config.MapHeight-1))

		// Update kinetic position
		if kinetic, ok := s.world.Components.Kinetic.GetPtr(orbEntity); ok {
			kinetic.PreciseX, kinetic.PreciseY = vmath.Point{X: targetGridX, Y: targetGridY}.CenterF()
		}

		// Update grid position
		s.world.Positions.SetPosition(orbEntity, component.PositionComponent{X: targetGridX, Y: targetGridY})

		// Handle flash decay (flash triggered only by firing, not movement)
		if orb.FlashRemaining > 0 {
			orb.FlashRemaining = max(orb.FlashRemaining-dt, 0)
		}
	}
}

// destroyOrb removes an orb entity and clears its reference from its owner's loadout
func (s *WeaponSystem) destroyOrb(orbEntity core.Entity) {
	if orbComp, ok := s.world.Components.Orb.GetPtr(orbEntity); ok {
		if weaponComp, ok := s.world.Components.Weapon.GetPtr(orbComp.OwnerEntity); ok {
			if weaponComp.Orbs[orbComp.WeaponType] == orbEntity {
				weaponComp.Orbs[orbComp.WeaponType] = 0
			}
		}
	}

	event.EmitDeathOne(s.world.Resources.Event.Queue, orbEntity, 0)
}

// destroyCursorOrbs drops one cursor's orbs, clearing the references in place
func (s *WeaponSystem) destroyCursorOrbs(weaponComp *component.WeaponComponent) {
	for wt, orbEntity := range weaponComp.Orbs {
		if orbEntity == 0 {
			continue
		}
		weaponComp.Orbs[wt] = 0
		event.EmitDeathOne(s.world.Resources.Event.Queue, orbEntity, 0)
	}
}

// destroyAllOrbs drops every orb in the world; the reset path
func (s *WeaponSystem) destroyAllOrbs() {
	for _, orbEntity := range s.world.Components.Orb.Entities() {
		s.destroyOrb(orbEntity)
	}
}

// handleFireMain fires one cursor's main weapon and its ready loadout
func (s *WeaponSystem) handleFireMain(cursor core.Entity) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok || weaponComp.MainFireCooldown > 0 {
		return
	}

	// Reset cooldown
	weaponComp.MainFireCooldown = parameter.WeaponCooldownMain
	s.statMainFired.Add(1)

	// Determine color type from this cursor's energy polarity
	colorType := component.CleanerColorPositive
	if energyComp, ok := s.world.Components.Energy.GetPtr(cursor); ok {
		if energyComp.Current < 0 {
			colorType = component.CleanerColorNegative
		}
	}

	// Fire Main Weapon (Cleaner)
	if pos, ok := s.world.Positions.GetPosition(cursor); ok {
		s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
			Entity:    cursor,
			OriginX:   pos.X,
			OriginY:   pos.Y,
			ColorType: colorType,
		})
	}

	s.fireAllWeapons(cursor, weaponComp)
}

// fireAllWeapons discharges every ready weapon in one cursor's loadout
func (s *WeaponSystem) fireAllWeapons(cursor core.Entity, weaponComp *component.WeaponComponent) {
	cursorPos, ok := s.world.Positions.GetPosition(cursor)
	if !ok {
		return
	}

	// Resolve targets once for all weapons
	fromX, fromY := vmath.Point{X: cursorPos.X, Y: cursorPos.Y}.CenterF()

	// Single shared fetch for Rod+Launcher, sized to whichever needs more targets this tick
	// collapses two Combat/Member store scans+sorts into one per fire cycle
	rodCharges := weaponComp.Charges[component.WeaponRod]
	rodReady := rodCharges > 0 && weaponComp.Cooldown[component.WeaponRod] <= 0

	launcherCharges := weaponComp.Charges[component.WeaponLauncher]
	launcherReady := launcherCharges > 0 && weaponComp.Cooldown[component.WeaponLauncher] <= 0

	var sharedAssignments []TargetAssignment
	if rodReady || launcherReady {
		maxNeeded := 0
		if rodReady {
			maxNeeded = rodCharges
		}
		if launcherReady && launcherCharges > maxNeeded {
			maxNeeded = launcherCharges
		}
		sharedAssignments = FindNearestTargets(s.world, fromX, fromY, maxNeeded, cursor)
	}

	for wt := range weaponComp.Charges {
		charges := weaponComp.Charges[wt]
		if charges <= 0 || weaponComp.Cooldown[wt] > 0 {
			continue
		}

		switch component.WeaponType(wt) {
		case component.WeaponRod:
			// Slice shared result instead of independent fetch
			assignments := sharedAssignments
			if len(assignments) > charges {
				assignments = assignments[:charges]
			}
			if len(assignments) == 0 {
				continue
			}

			weaponComp.Cooldown[wt] = parameter.WeaponCooldownRod
			s.statRodFired.Add(1)

			rodOrbEntity := weaponComp.Orbs[wt]
			if rodOrbEntity != 0 {
				s.triggerOrbFlash(rodOrbEntity)
			}

			// Rod fires at unique targets only - assignments may repeat under overflow
			seen := make(map[core.Entity]bool, len(assignments))
			for _, a := range assignments {
				if seen[a.Target] {
					continue
				}
				seen[a.Target] = true

				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackLightning,
					OwnerEntity:  cursor,
					OriginEntity: rodOrbEntity,
					TargetEntity: a.Target,
					HitEntity:    a.Hit,
				})
			}

		case component.WeaponLauncher:
			assignments := sharedAssignments
			if len(assignments) > charges {
				assignments = assignments[:charges]
			}
			if len(assignments) == 0 {
				continue
			}

			weaponComp.Cooldown[wt] = parameter.WeaponCooldownLauncher
			s.statLauncherFired.Add(1)
			launcherOrbEntity := weaponComp.Orbs[wt]

			originX, originY := cursorPos.X, cursorPos.Y
			if launcherOrbEntity != 0 {
				s.triggerOrbFlash(launcherOrbEntity)
				if orbPos, ok := s.world.Positions.GetPosition(launcherOrbEntity); ok {
					originX, originY = orbPos.X, orbPos.Y
				}
			}

			targets := make([]core.Entity, len(assignments))
			hits := make([]core.Entity, len(assignments))
			for i, a := range assignments {
				targets[i] = a.Target
				hits[i] = a.Hit
			}

			s.world.PushEvent(event.EventMissileSpawnRequest, &event.MissileSpawnRequestPayload{
				OwnerEntity:  cursor,
				OriginEntity: launcherOrbEntity,
				OriginX:      originX,
				OriginY:      originY,
				Count:        charges,
				Targets:      targets,
				HitEntities:  hits,
			})

		case component.WeaponDisruptor:
			s.fireDisruptorWeapon(cursor, cursorPos, weaponComp)
		}
	}
}

// fireDisruptorWeapon discharges one cursor's area pulse
func (s *WeaponSystem) fireDisruptorWeapon(cursor core.Entity, cursorPos component.PositionComponent, weaponComp *component.WeaponComponent) {
	targets := FindTargetsInEllipse(s.world, cursorPos.X, cursorPos.Y, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq, cursor)
	if len(targets) == 0 {
		return
	}

	// Consume cooldown
	weaponComp.Cooldown[component.WeaponDisruptor] = parameter.WeaponCooldownDisruptor
	s.statDisruptorFired.Add(1)

	// Visual orb flash
	if disruptorOrbEntity := weaponComp.Orbs[component.WeaponDisruptor]; disruptorOrbEntity != 0 {
		s.triggerOrbFlash(disruptorOrbEntity)
	}

	// Emit area attack per target
	for _, target := range targets {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackPulse,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: target.Target,
			HitEntities:  target.Members,
			OriginX:      cursorPos.X,
			OriginY:      cursorPos.Y,
		})
	}

	// Set pulse effect on the firing cursor for visual feedback
	s.world.Components.Pulse.SetComponent(cursor, component.PulseComponent{
		OriginX:   cursorPos.X,
		OriginY:   cursorPos.Y,
		Duration:  parameter.PulseEffectDuration,
		Remaining: parameter.PulseEffectDuration,
	})
}

// publishLoadout mirrors one cursor's owned weapons into its roster slot
func (s *WeaponSystem) publishLoadout(cursor core.Entity, weaponComp *component.WeaponComponent) {
	slot, ok := s.world.CursorSlot(cursor)
	if !ok {
		return
	}
	s.statRod.Store(slot, weaponComp.Charges[component.WeaponRod] > 0)
	s.statLauncher.Store(slot, weaponComp.Charges[component.WeaponLauncher] > 0)
	s.statDisruptor.Store(slot, weaponComp.Charges[component.WeaponDisruptor] > 0)
}

// clearSlot zeroes a retired slot's cells
func (s *WeaponSystem) clearSlot(slot uint8) {
	s.statRod.Store(slot, false)
	s.statLauncher.Store(slot, false)
	s.statDisruptor.Store(slot, false)
	s.statOrbs.Store(slot, 0)
}
