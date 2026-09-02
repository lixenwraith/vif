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

// orbSlots indexes one cursor's orbs by weapon type, zero where the cursor has
// none. It is a reading of the Orb store, never a stored reference: an orb is a
// player-domain entity and its handle means nothing outside the instance that
// allocated it, so no shared component may hold one (D-4).
type orbSlots [component.WeaponCount]core.Entity

// WeaponSystem manages per-cursor weapon loadouts, orbs and firing.
// A loadout resets when its own cursor's energy crosses zero.
type WeaponSystem struct {
	world *engine.World

	// orbs is this tick's index, rebuilt by reapOrbs from the Orb store at the top
	// of every Update and keyed by the owner's roster slot. Only cursors this
	// instance simulates carry orbs (D-2), so the array is bounded by the roster
	// and the scan by the local loadout.
	orbs    [parameter.MaxPlayers]orbSlots
	reapBuf []core.Entity

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
	statOrbsReaped     *atomic.Int64
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
	// An orb the store held and no loadout justified. Zero is the ordinary reading:
	// a rising count is a lifecycle the index no longer agrees with, which is the
	// gauge the per-slot weapon.orbs cell could not offer while it counted
	// references rather than entities.
	s.statOrbsReaped = reg.Ints.Get("weapon.orbs_reaped")
	s.rejects = newRejectionTelemetry(reg, "weapon")

	s.Init()
	return s
}

// Init resets session state for a new game, dropping every orb in the world
func (s *WeaponSystem) Init() {
	s.destroyAllOrbs()
	s.orbs = [parameter.MaxPlayers]orbSlots{}
	s.reapBuf = s.reapBuf[:0]
	s.statRod.Reset()
	s.statLauncher.Reset()
	s.statDisruptor.Reset()
	s.statOrbs.Reset()
	s.statMainFired.Store(0)
	s.statRodFired.Store(0)
	s.statLauncherFired.Store(0)
	s.statDisruptorFired.Store(0)
	s.statOrbsReaped.Store(0)
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
			if cursor := s.world.ResolveOwnedCursor(p.Entity); cursor != 0 {
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
			cursor := s.world.ResolveOwnedCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.addWeapon(cursor, payload.Weapon)
		}

	case event.EventWeaponFireRequest:
		if payload, ok := ev.Payload.(*event.WeaponFireRequestPayload); ok {
			if cursor := s.world.ResolveOwnedCursor(payload.Entity); cursor != 0 {
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

	// The index and the orbs it admits are one pass: what the store holds and what
	// the loadouts justify are compared before any cursor is advanced.
	s.reapOrbs()

	s.world.Components.Cursor.Each(func(cursor core.Entity, _ *component.CursorComponent) bool {
		// D-2: only the owner simulates a cursor's weapons, cooldowns and orbs
		if !s.world.SimulatesLocally(cursor) {
			return true
		}

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

		slot, ok := s.world.CursorSlot(cursor)
		if !ok {
			return true
		}
		orbs := s.ensureOrbs(cursor, slot, weaponComp)
		s.updateOrbs(cursor, slot, orbs)
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

	s.destroyCursorOrbs(cursor)

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

// reapOrbs rebuilds the per-slot index from the Orb store and drops every orb the
// store no longer justifies.
//
// The store is the index: each orb names its owner and its weapon type, so the
// pass that reads it is also the pass that can see what it should not hold — an
// orb whose owner is no longer a cursor this instance simulates, one whose weapon
// is no longer charged, and any second orb for an (owner, weapon) pair that
// already has one. Which duplicate survives is the older entity rather than
// whichever the dense store happened to hold first, so two readings of one store
// agree.
//
// This is what a lost reference used to cost. The index lived on the cursor's
// shared view component, a correction overwrote it with the sender's zeroes, and
// ensureOrbs read a zero and spawned a replacement: the entity the zero had named
// stayed in the store, protected from decay, no longer followed by updateOrbs and
// still drawn, once per correction for the life of the run.
func (s *WeaponSystem) reapOrbs() {
	s.orbs = [parameter.MaxPlayers]orbSlots{}
	s.reapBuf = s.reapBuf[:0]

	for _, orbEntity := range s.world.Components.Orb.Entities() {
		orb, ok := s.world.Components.Orb.GetPtr(orbEntity)
		if !ok {
			continue
		}
		slot, weapon, kept := s.orbOwnership(orb)
		if !kept {
			s.reapBuf = append(s.reapBuf, orbEntity)
			continue
		}
		if prior := s.orbs[slot][weapon]; prior != 0 {
			if prior < orbEntity {
				s.reapBuf = append(s.reapBuf, orbEntity)
				continue
			}
			s.reapBuf = append(s.reapBuf, prior)
		}
		s.orbs[slot][weapon] = orbEntity
	}

	if len(s.reapBuf) > 0 {
		s.statOrbsReaped.Add(int64(len(s.reapBuf)))
		event.EmitDeath(s.world.Resources.Event.Queue, 0, s.reapBuf...)
	}
}

// orbOwnership resolves the roster slot and weapon an orb still belongs to. The
// admission is D-2's: an orb belongs to a cursor this instance simulates, and to a
// weapon that cursor still holds a charge of.
func (s *WeaponSystem) orbOwnership(orb *component.OrbComponent) (slot uint8, weapon component.WeaponType, ok bool) {
	if orb.WeaponType < 0 || orb.WeaponType >= component.WeaponCount {
		return 0, 0, false
	}
	cursor := s.world.ResolveOwnedCursor(orb.OwnerEntity)
	if cursor == 0 {
		return 0, 0, false
	}
	slot, ok = s.world.CursorSlot(cursor)
	if !ok {
		return 0, 0, false
	}
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok || weaponComp.Charges[orb.WeaponType] <= 0 {
		return 0, 0, false
	}
	return slot, orb.WeaponType, true
}

// orbsOf reads one cursor's orbs straight from the store, for a caller that runs
// between two ticks and cannot use the index reapOrbs left. The duplicate rule is
// reapOrbs's, so the orb a fire path flashes is the one the next tick advances.
func (s *WeaponSystem) orbsOf(cursor core.Entity) orbSlots {
	var out orbSlots
	if cursor == 0 {
		return out
	}
	for _, orbEntity := range s.world.Components.Orb.Entities() {
		orb, ok := s.world.Components.Orb.GetPtr(orbEntity)
		if !ok || orb.OwnerEntity != cursor {
			continue
		}
		if orb.WeaponType < 0 || orb.WeaponType >= component.WeaponCount {
			continue
		}
		if prior := out[orb.WeaponType]; prior != 0 && prior < orbEntity {
			continue
		}
		out[orb.WeaponType] = orbEntity
	}
	return out
}

// ensureOrbs creates the orbs one cursor's charged weapons are missing and
// triggers redistribution. A weapon whose orb reapOrbs already found keeps it:
// recovery is the ordinary case and creation the exception, which is the inverse
// of what a cached reference could offer.
func (s *WeaponSystem) ensureOrbs(cursor core.Entity, slot uint8, weaponComp *component.WeaponComponent) orbSlots {
	orbs := s.orbs[slot]
	changed := false
	for wt := range weaponComp.Charges {
		if weaponComp.Charges[wt] <= 0 || orbs[wt] != 0 {
			continue
		}
		orbEntity := s.spawnOrbEntity(cursor, component.WeaponType(wt))
		if orbEntity == 0 {
			continue
		}
		orbs[wt] = orbEntity
		changed = true
	}
	if changed {
		s.orbs[slot] = orbs
		s.redistributeOrbs(orbs)
	}
	return orbs
}

// redistributeOrbs invalidates one cursor's orb target angles; updateOrbs recalculates
func (s *WeaponSystem) redistributeOrbs(orbs orbSlots) {
	for _, orbEntity := range orbs {
		if orbEntity == 0 {
			continue
		}
		if orb, ok := s.world.Components.Orb.GetPtr(orbEntity); ok {
			orb.TargetAngle = -1 // Invalid angle forces recalculation
		}
	}
}

// spawnOrbEntity creates an orb entity for a weapon type
func (s *WeaponSystem) spawnOrbEntity(ownerEntity core.Entity, weaponType component.WeaponType) core.Entity {
	ownerPos, ok := s.world.Positions.GetPosition(ownerEntity)
	if !ok {
		return 0
	}

	orbEntity := s.world.CreateEntity(core.DomainPlayer)

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

// updateOrbs handles one cursor's orbital motion with arc-aware collision avoidance
func (s *WeaponSystem) updateOrbs(cursor core.Entity, slot uint8, orbs orbSlots) {
	dt := s.world.Resources.Time.DeltaTime
	config := s.world.Resources.Config

	cursorPos, ok := s.world.Positions.GetPosition(cursor)
	if !ok {
		return
	}

	// Collect active orbs in STABLE order (sort by weapon type)
	type orbEntry struct {
		entity core.Entity
		weapon component.WeaponType
	}
	var entries []orbEntry
	for weapon, orbEntity := range orbs {
		if orbEntity == 0 {
			continue
		}
		if s.world.Components.Orb.HasEntity(orbEntity) {
			entries = append(entries, orbEntry{entity: orbEntity, weapon: component.WeaponType(weapon)})
		}
	}

	if len(entries) == 0 {
		s.statOrbs.Store(slot, 0)
		return
	}
	s.statOrbs.Store(slot, int64(len(entries)))

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

// destroyCursorOrbs drops every orb one cursor owns. It reads the store rather
// than a per-cursor list, so an orb an earlier defect stranded leaves with the
// rest instead of outliving the loadout that justified it.
func (s *WeaponSystem) destroyCursorOrbs(cursor core.Entity) {
	for _, orbEntity := range s.orbsOf(cursor) {
		if orbEntity == 0 {
			continue
		}
		event.EmitDeath(s.world.Resources.Event.Queue, 0, orbEntity)
	}
}

// destroyAllOrbs drops every orb in the world; the reset path
func (s *WeaponSystem) destroyAllOrbs() {
	for _, orbEntity := range s.world.Components.Orb.Entities() {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, orbEntity)
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
		s.world.PushLocal(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
			Entity:    cursor,
			OriginX:   pos.X,
			OriginY:   pos.Y,
			ColorType: colorType,
		})
	}

	s.fireAllWeapons(cursor, weaponComp, s.orbsOf(cursor))
}

// fireAllWeapons discharges every ready weapon in one cursor's loadout
func (s *WeaponSystem) fireAllWeapons(cursor core.Entity, weaponComp *component.WeaponComponent, orbs orbSlots) {
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
		sharedAssignments = FindNearestTargets(s.world, fromX, fromY, maxNeeded, engine.ScopeBoth, cursor)
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

			rodOrbEntity := orbs[wt]
			originX, originY := cursorPos.X, cursorPos.Y
			if rodOrbEntity != 0 {
				s.triggerOrbFlash(rodOrbEntity)
				if orbPos, ok := s.world.Positions.GetPosition(rodOrbEntity); ok {
					originX, originY = orbPos.X, orbPos.Y
				}
			}

			// Rod fires at unique targets only - assignments may repeat under overflow
			seen := make(map[core.Entity]bool, len(assignments))
			for _, a := range assignments {
				if seen[a.Target] {
					continue
				}
				seen[a.Target] = true

				// Resolved by the target, not by the firing cursor's domain (D-10).
				s.world.PushEventDomain(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackLightning,
					OwnerEntity:  cursor,
					OriginEntity: cursor,
					TargetEntity: a.Target,
					HitEntity:    a.Hit,
					HasOrigin:    true,
					OriginX:      originX,
					OriginY:      originY,
				}, a.Target.Domain())
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
			launcherOrbEntity := orbs[wt]

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
				OwnerEntity: cursor,
				OriginX:     originX,
				OriginY:     originY,
				Count:       charges,
				Targets:     targets,
				HitEntities: hits,
			})

		case component.WeaponDisruptor:
			s.fireDisruptorWeapon(cursor, cursorPos, weaponComp, orbs)
		}
	}
}

// fireDisruptorWeapon discharges one cursor's area pulse
func (s *WeaponSystem) fireDisruptorWeapon(cursor core.Entity, cursorPos component.PositionComponent, weaponComp *component.WeaponComponent, orbs orbSlots) {
	targets := FindTargetsInEllipse(s.world, cursorPos.X, cursorPos.Y, parameter.PulseRadiusInvRxSq, parameter.PulseRadiusInvRySq, engine.ScopeBoth, cursor)
	if len(targets) == 0 {
		return
	}

	// Consume cooldown
	weaponComp.Cooldown[component.WeaponDisruptor] = parameter.WeaponCooldownDisruptor
	s.statDisruptorFired.Add(1)

	// Visual orb flash
	if disruptorOrbEntity := orbs[component.WeaponDisruptor]; disruptorOrbEntity != 0 {
		s.triggerOrbFlash(disruptorOrbEntity)
	}

	// Resolve player targets here; shared targets are re-derived from the crossing geometry.
	var pulse blastArea
	pulse.resetOne(cursorPos.X, cursorPos.Y, parameter.PulseRadiusX)
	strikePlayerTargets(s.world, cursor, &pulse, component.CombatAttackPulse)
	s.world.PushCrossing(event.EventExplosionRequest, &event.ExplosionRequestPayload{
		Entity: cursor,
		X:      cursorPos.X,
		Y:      cursorPos.Y,
		Radius: parameter.PulseRadiusX,
		Attack: component.CombatAttackPulse,
	})

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
