package system

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
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

	rng *vmath.FastRand // shot spread; a cursor's shots are this instance's alone

	// The gun as the scenario aims it, which every cursor this instance simulates
	// fires: one that arrives, comes back or is adopted later takes it as it updates
	aim gunAim

	// Tracers in flight, read detached: one destroyed this tick frees its cells for the next
	tracerBuf []core.Entity
	glyphBuf  []core.Entity

	// Per-cursor loadout, by weapon kind
	statHeld [component.WeaponCount]*status.PlayerBool
	statOrbs *status.PlayerInt

	// Roster-wide fire counters
	statMainFired  *atomic.Int64
	statFired      [component.WeaponCount]*atomic.Int64
	statOrbsReaped *atomic.Int64
	statKind       *atomic.Int64
	rejects        rejectionTelemetry

	statTracers      *atomic.Int64
	statTracerWalls  *atomic.Int64
	statTracerBounds *atomic.Int64
	statTracerSteps  *atomic.Int64
	buffers          bufferTelemetry

	toggle
}

// gunAim is the cardinals and the upgrade path a scenario gives the gun, Levels of
// Path long; the zero value leaves the gun on its own four
type gunAim struct {
	Directions component.TracerDirection                `json:"directions"`
	Path       [parameter.GunPathMax]component.GunLevel `json:"path"`
	Levels     int                                      `json:"levels"`
}

// NewWeaponSystem creates a new weapon system
func NewWeaponSystem(world *engine.World) engine.System {
	s := &WeaponSystem{world: world}

	reg := world.Resources.Status
	for wt, spec := range component.WeaponSpecs {
		key := "weapon." + spec.Name
		s.statHeld[wt] = status.NewPlayerBool(reg, parameter.MaxPlayers, key, key)
		s.statFired[wt] = reg.Ints.Get(key + "_fired")
	}
	s.statOrbs = status.NewPlayerInt(reg, parameter.MaxPlayers, "weapon.orbs", "weapon.orbs")
	s.statMainFired = reg.Ints.Get("weapon.main_fired")
	s.statKind = reg.Ints.Get("weapon.kind_rejects")
	// An orb the store held and no loadout justified. Zero is the ordinary reading:
	// a rising count is a lifecycle the index no longer agrees with, which is the
	// gauge the per-slot weapon.orbs cell could not offer while it counted
	// references rather than entities.
	s.statOrbsReaped = reg.Ints.Get("weapon.orbs_reaped")
	s.rejects = newRejectionTelemetry(reg, "weapon")
	s.statTracers = reg.Ints.Get("weapon.tracers")
	s.statTracerWalls = reg.Ints.Get("weapon.tracer_wall_collisions")
	s.statTracerBounds = reg.Ints.Get("weapon.tracer_boundary_steps")
	s.statTracerSteps = reg.Ints.Get("weapon.tracer_grid_steps")
	s.buffers = newBufferTelemetry(reg, "weapon", "tracers", "glyphs")

	return s
}

// Init resets session state for a new game, dropping every orb in the world
func (s *WeaponSystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.aim = gunAim{}
	s.destroyAllOrbs()
	s.orbs = [parameter.MaxPlayers]orbSlots{}
	s.reapBuf = s.reapBuf[:0]
	for wt := range component.WeaponCount {
		s.statHeld[wt].Reset()
		s.statFired[wt].Store(0)
	}
	s.statOrbs.Reset()
	s.statMainFired.Store(0)
	s.statOrbsReaped.Store(0)
	s.statKind.Store(0)
	s.rejects.Reset()
	s.tracerBuf, s.glyphBuf = s.tracerBuf[:0], s.glyphBuf[:0]
	s.statTracers.Store(0)
	s.statTracerWalls.Store(0)
	s.statTracerBounds.Store(0)
	s.statTracerSteps.Store(0)
	s.buffers.Reset()
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
		event.EventWeaponRemoveRequest,
		event.EventWeaponSetRequest,
		event.EventEnergyCrossedZero,
		event.EventWeaponFireRequest,
		event.EventGunAimRequest,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
	}
}

// HandleEvent processes weapon commands, each naming the cursor it acts on
func (s *WeaponSystem) HandleEvent(ev event.GameEvent) {
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
			if payload.Weapon < 0 || payload.Weapon >= component.WeaponCount {
				s.statKind.Add(1)
				return
			}
			cursor := s.world.ResolveOwnedCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.addWeapon(cursor, payload.Weapon)
		}

	case event.EventWeaponRemoveRequest:
		if payload, ok := ev.Payload.(*event.WeaponRemoveRequestPayload); ok {
			if payload.Weapon < 0 || payload.Weapon >= component.WeaponCount {
				s.statKind.Add(1)
				return
			}
			cursor := s.world.ResolveOwnedCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.removeWeapon(cursor, payload.Weapon)
		}

	case event.EventWeaponSetRequest:
		if payload, ok := ev.Payload.(*event.WeaponSetRequestPayload); ok {
			if payload.Weapon < 0 || payload.Weapon >= component.WeaponCount {
				s.statKind.Add(1)
				return
			}
			cursor := s.world.ResolveOwnedCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.setWeapon(cursor, payload.Weapon, payload.Charges)
		}

	case event.EventWeaponFireRequest:
		if payload, ok := ev.Payload.(*event.WeaponFireRequestPayload); ok {
			if cursor := s.world.ResolveOwnedCursor(payload.Entity); cursor != 0 {
				s.fire(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}

	case event.EventGunAimRequest:
		// The gun's charges stay: a path the request replaces fires the level they
		// count to on the new one, and an instance installing the game mid-run aims
		// with the charges it carried
		if payload, ok := ev.Payload.(*event.GunAimRequestPayload); ok {
			s.aim = gunAim{Directions: payload.Directions & component.TracerAllDirections}
			s.aim.Levels = copy(s.aim.Path[:], payload.Path)
			s.world.Components.Cursor.Each(func(cursor core.Entity, _ *component.CursorComponent) bool {
				if weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor); ok && s.world.SimulatesLocally(cursor) {
					s.aimGun(weaponComp)
				}
				return true
			})
		}
	}
}

// aimGun puts a gun on the scenario's aim and path
func (s *WeaponSystem) aimGun(weaponComp *component.WeaponComponent) {
	weaponComp.Aim, weaponComp.GunPath, weaponComp.GunLevels = s.aim.Directions, s.aim.Path, s.aim.Levels
}

// SaveLocal carries the scenario's aim, which a copy's cursors go on firing
func (s *WeaponSystem) SaveLocal() ([]byte, error) { return json.Marshal(s.aim) }

func (s *WeaponSystem) LoadLocal(data []byte) error {
	if err := json.Unmarshal(data, &s.aim); err != nil {
		return fmt.Errorf("weapon: %w", err)
	}
	return nil
}

// Update advances cooldowns and orbit for every cursor
func (s *WeaponSystem) Update() {
	if !s.enabled {
		return
	}

	s.publishSlots()

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
		s.aimGun(weaponComp)

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

		slot, ok := s.world.CursorSlot(cursor)
		if !ok {
			return true
		}
		orbs := s.ensureOrbs(cursor, slot, weaponComp)
		s.updateOrbs(cursor, slot, orbs)
		s.advanceRays(cursor, orbs, dt)
		s.advanceStreams(cursor, weaponComp, orbs, dt)
		return true
	})

	s.advanceTracers()
}

// advanceStreams fires a capped kind's stream shots while its wave cooldown runs,
// on its own clock at the main-fire rate; a wave arms it and the cooldown's end stops it
func (s *WeaponSystem) advanceStreams(cursor core.Entity, weaponComp *component.WeaponComponent, orbs orbSlots, dt time.Duration) {
	cursorPos, ok := s.world.CursorCell(cursor)
	if !ok {
		return
	}
	for wt, charges := range weaponComp.Charges {
		spec := &component.WeaponSpecs[wt]
		if spec.Stream <= 0 || charges < spec.MaxCharges || weaponComp.Cooldown[wt] <= 0 {
			continue
		}
		if weaponComp.Stream[wt] -= dt; weaponComp.Stream[wt] > 0 {
			continue
		}
		weaponComp.Stream[wt] = parameter.WeaponStreamInterval
		x, y := s.emitterCell(orbs[wt], cursorPos)
		fromX, fromY := vmath.Point{X: x, Y: y}.Center()
		targets := FindNearestTargets(s.world, fromX, fromY, spec.Stream, engine.ScopeBoth, cursor)
		if spec.Delivery == component.DeliveryBullet && s.fireBullets(cursor, x, y, spec.Attack, targets) {
			s.statFired[wt].Add(1)
		}
	}
}

// addWeapon grants or recharges one weapon on one cursor
func (s *WeaponSystem) addWeapon(cursor core.Entity, weapon component.WeaponType) {
	if weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor); ok && weaponComp.Charges[weapon] < chargeCap(weaponComp, weapon) {
		s.setWeapon(cursor, weapon, weaponComp.Charges[weapon]+1)
	}
}

// setWeapon writes one kind's charges on one cursor, capped at what it holds; 0 or
// less takes the kind
func (s *WeaponSystem) setWeapon(cursor core.Entity, weapon component.WeaponType, charges int) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok {
		return
	}
	if charges <= 0 {
		s.removeWeapon(cursor, weapon)
		return
	}

	firstAcquire := weaponComp.Charges[weapon] == 0
	weaponComp.Charges[weapon] = min(charges, chargeCap(weaponComp, weapon))
	if firstAcquire {
		weaponComp.Cooldown[weapon] = 0 // Ready to fire immediately on first pickup
		s.publishLoadout(cursor, weaponComp)
	}
}

// chargeCap is the charges a kind holds at most: its spec's, or a charge per level
// of the path a gun climbs
func chargeCap(weaponComp *component.WeaponComponent, weapon component.WeaponType) int {
	if weaponComp.GunLevels > 0 && component.WeaponSpecs[weapon].Delivery == component.DeliveryTracer {
		return weaponComp.GunLevels
	}
	return component.WeaponSpecs[weapon].MaxCharges
}

// removeAllWeapons strips the loot from one cursor's loadout and destroys only
// its own orbs; an innate kind stays, so a zero crossing costs the gun nothing
func (s *WeaponSystem) removeAllWeapons(cursor core.Entity) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok {
		return
	}

	s.destroyCursorOrbs(cursor)

	for wt := range weaponComp.Charges {
		if component.WeaponSpecs[wt].Innate {
			continue
		}
		weaponComp.Charges[wt], weaponComp.Cooldown[wt], weaponComp.Stream[wt] = 0, 0, 0
	}

	s.publishLoadout(cursor, weaponComp)
	if slot, ok := s.world.CursorSlot(cursor); ok {
		s.statOrbs.Store(slot, 0)
	}
}

// removeWeapon takes one kind from a cursor, its orb with it; a level takes the
// gun away this way for a region fought without it
func (s *WeaponSystem) removeWeapon(cursor core.Entity, weapon component.WeaponType) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok {
		return
	}
	weaponComp.Charges[weapon], weaponComp.Cooldown[weapon], weaponComp.Stream[weapon] = 0, 0, 0
	if orb := s.orbsOf(cursor)[weapon]; orb != 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, orb)
	}
	s.publishLoadout(cursor, weaponComp)
}

// emitterCell is the cell a weapon's shot leaves from: its orb's, or the cursor's
// while the orb is not yet placed
func (s *WeaponSystem) emitterCell(orbEntity core.Entity, cursorPos component.PositionComponent) (int, int) {
	if pos, ok := s.world.Positions.GetPosition(orbEntity); ok {
		return pos.X, pos.Y
	}
	return cursorPos.X, cursorPos.Y
}

// triggerOrbFlash activates flash effect on specified orb
func (s *WeaponSystem) triggerOrbFlash(orbEntity core.Entity) {
	orbComp, ok := s.world.Components.Orb.GetPtr(orbEntity)
	if !ok {
		return
	}

	orbComp.FlashRemaining = parameter.OrbFlashDuration
}

// Rebuild from live ownership so corrections cannot strand duplicate orbs.
// Keep the oldest entity for each cursor/weapon pair, independent of store order.
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
	if orb.WeaponType < 0 || orb.WeaponType >= component.WeaponCount || component.WeaponSpecs[orb.WeaponType].FromCursor {
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
		if weaponComp.Charges[wt] <= 0 || orbs[wt] != 0 || component.WeaponSpecs[wt].FromCursor {
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
	ownerPos, ok := s.world.CursorCell(ownerEntity)
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
		OrbitRadiusY: s.world.Resources.Config.CellAspect.Y(parameter.OrbOrbitRadiusY),
		OrbitSpeed:   parameter.OrbOrbitSpeed,
	}

	// Initial position at angle 0
	gridX, gridY := vmath.AngleToGridPos(0, ownerPos.X, ownerPos.Y, orbComp.OrbitRadiusX, orbComp.OrbitRadiusY)
	preciseX, preciseY := vmath.Point{X: gridX, Y: gridY}.Center()

	kineticComp := component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	}

	protComp := component.ProtectionComponent{
		Mask: component.ProtectFromSpecies | component.ProtectFromParticle,
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

	cursorPos, ok := s.world.CursorCell(cursor)
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
	const angleThreshold = vmath.TwoPi / 32
	redistribute := false
	for i, entry := range entries {
		orb, _ := s.world.Components.Orb.GetPtr(entry.entity)
		if orb.TargetAngle < 0 || vmath.Abs(vmath.AngleDiff(orb.TargetAngle, targetAngles[i])) > angleThreshold {
			redistribute = true
			break
		}
	}
	freeOrbit := fullCircle && !redistribute
	for _, entry := range entries {
		orb, _ := s.world.Components.Orb.GetPtr(entry.entity)
		freeOrbit = freeOrbit && orb.RedistributeRemaining <= 0
	}
	phase := firstOrb.OrbitAngle + firstOrb.OrbitSpeed*dt.Seconds()
	spacing := vmath.TwoPi / float64(len(entries))

	// Update each orb
	for i := range entries {
		orbEntity := entries[i].entity
		orb, ok := s.world.Components.Orb.GetPtr(orbEntity)
		if !ok {
			continue
		}
		targetAngle := targetAngles[i]

		// All orbs must finish together before free rotation preserves their spacing.
		if redistribute {
			orb.StartAngle = orb.OrbitAngle
			orb.TargetAngle = targetAngle
			orb.RedistributeRemaining = parameter.OrbRedistributeDuration
		}

		// Handle movement based on arc availability
		if freeOrbit {
			// A common phase also repairs offsets left by blocked-path recovery.
			orb.OrbitAngle = vmath.NormalizeAngle(phase + float64(i)*spacing)
		} else if orb.RedistributeRemaining > 0 {
			// Animating to new position
			orb.RedistributeRemaining -= dt
			if orb.RedistributeRemaining <= 0 {
				orb.RedistributeRemaining = 0
				orb.OrbitAngle = orb.TargetAngle
			} else {
				t := 1.0 - orb.RedistributeRemaining.Seconds()/parameter.OrbRedistributeDuration.Seconds()
				// Use shortest path interpolation
				diff := vmath.AngleDiff(orb.StartAngle, orb.TargetAngle)
				orb.OrbitAngle = vmath.NormalizeAngle(orb.StartAngle + diff*t)
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
			kinetic.PreciseX, kinetic.PreciseY = vmath.Point{X: targetGridX, Y: targetGridY}.Center()
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

// fire discharges one cursor's loadout on a main-fire request, under main fire's cooldown
func (s *WeaponSystem) fire(cursor core.Entity) {
	weaponComp, ok := s.world.Components.Weapon.GetPtr(cursor)
	if !ok {
		return
	}
	orbs := s.orbsOf(cursor)
	// Every request, taken or not, holds a capped ray lit; the cooldown gates the rest
	s.holdRays(cursor, weaponComp, orbs)
	if weaponComp.MainFireCooldown > 0 {
		return
	}
	weaponComp.MainFireCooldown = parameter.WeaponCooldownMain
	s.statMainFired.Add(1)
	s.fireLoadout(cursor, weaponComp, orbs)
}

// holdRays keeps each capped sustained kind's ray lit on its orb for RayHold, so the
// ray follows the trigger like every other kind: it lights one where none burns,
// needing no target, and converts a timed ray still burning in place
func (s *WeaponSystem) holdRays(cursor core.Entity, weaponComp *component.WeaponComponent, orbs orbSlots) {
	rays := s.world.Components.Ray
	for wt, orb := range orbs {
		spec := &component.WeaponSpecs[wt]
		charges := weaponComp.Charges[wt]
		if orb == 0 || !spec.Sustained || charges < spec.MaxCharges {
			continue
		}
		ray, ok := rays.GetPtr(orb)
		if !ok {
			lit := component.RayComponent{Phase: component.RayFiring,
				Remaining: parameter.RayDuration, Duration: parameter.RayDuration, Palette: s.palette(cursor)}
			s.layRay(cursor, orb, &lit)
			rays.SetComponent(orb, lit)
			ray, _ = rays.GetPtr(orb)
		}
		ray.Hold, ray.Scale, ray.Remaining = parameter.RayHold, charges, ray.Duration
	}
}

// fireLoadout discharges every ready kind of one cursor's loadout, each from its
// orb or, holding none, from the cursor
func (s *WeaponSystem) fireLoadout(cursor core.Entity, weaponComp *component.WeaponComponent, orbs orbSlots) {
	cursorPos, ok := s.world.CursorCell(cursor)
	if !ok {
		return
	}

	// Aimed weapons share one nearest-target query from the cursor, sized to the
	// largest ready loadout, so a fire cycle scans and sorts the stores once.
	need := 0
	for wt, charges := range weaponComp.Charges {
		if charges > 0 && weaponComp.Cooldown[wt] <= 0 && aimed(component.WeaponSpecs[wt].Delivery) {
			need = max(need, charges)
		}
	}
	var nearest []TargetAssignment
	if need > 0 {
		fromX, fromY := vmath.Point{X: cursorPos.X, Y: cursorPos.Y}.Center()
		nearest = FindNearestTargets(s.world, fromX, fromY, need, engine.ScopeBoth, cursor)
	}

	for wt, charges := range weaponComp.Charges {
		if charges <= 0 || weaponComp.Cooldown[wt] > 0 {
			continue
		}
		spec := &component.WeaponSpecs[wt]
		x, y := s.emitterCell(orbs[wt], cursorPos)
		assignments := nearest[:min(len(nearest), charges)]
		capped := charges >= spec.MaxCharges

		var fired bool
		switch spec.Delivery {
		case component.DeliveryLightning:
			arcRange := 0.0
			if capped {
				arcRange = spec.ArcRange
			}
			fired = s.fireLightning(cursor, x, y, spec.Attack, assignments, arcRange)
		case component.DeliveryMissile:
			fired = s.fireMissiles(cursor, x, y, charges, assignments)
		case component.DeliveryPulse:
			fired = s.firePulse(cursor, x, y, spec, capped)
		case component.DeliveryBullet:
			fired = s.fireBullets(cursor, x, y, spec.Attack, assignments)
		case component.DeliveryRay:
			// A sustained ray is holdRays's; main fire leaves it alone
			fired = !(capped && spec.Sustained) && len(assignments) > 0 && s.fireRay(cursor, orbs[wt], charges)
		case component.DeliveryTracer:
			fired = s.fireTracers(cursor, x, y, gunVolley(weaponComp, spec, charges), s.palette(cursor))
		}
		if !fired {
			continue
		}
		weaponComp.Cooldown[wt] = spec.Cooldown
		if capped && spec.MaxedCooldown > 0 {
			weaponComp.Cooldown[wt] = spec.MaxedCooldown
		}
		if capped && spec.Stream > 0 {
			weaponComp.Stream[wt] = parameter.WeaponStreamInterval
		}
		s.statFired[wt].Add(1)
		s.triggerOrbFlash(orbs[wt])
	}
}

// fireLightning strikes each unique assigned target from the emitter cell, and
// arcs each hit on within arcRange; assignments repeat under overflow, a bolt does not
func (s *WeaponSystem) fireLightning(cursor core.Entity, x, y int, attack component.CombatAttackType, assignments []TargetAssignment, arcRange float64) bool {
	var arced []core.Entity
	for i, a := range assignments {
		if slices.ContainsFunc(assignments[:i], func(b TargetAssignment) bool { return b.Target == a.Target }) {
			continue
		}
		// Resolved by the target, not by the firing cursor's domain (D-10).
		s.world.PushEventDomain(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: a.Target,
			HitEntity:    a.Hit,
			HasOrigin:    true,
			OriginX:      x,
			OriginY:      y,
		}, a.Target.Domain())
		if arcRange > 0 {
			arced = s.arcLightning(cursor, a, assignments, arced, attack, arcRange)
		}
	}
	return len(assignments) > 0
}

// arcLightning chains one hit to the nearest other target within range, in the
// rod's own priority of composites before singles: a request of the same family
// whose origin is the struck cell, so the zap runs from one target to the next.
// A target this volley struck or arced to is skipped rather than left to its
// immunity window
func (s *WeaponSystem) arcLightning(cursor core.Entity, from TargetAssignment, assignments []TargetAssignment, arced []core.Entity, attack component.CombatAttackType, arcRange float64) []core.Entity {
	pos, ok := s.world.Positions.GetPosition(from.Hit)
	if !ok {
		return arced
	}
	fromX, fromY := vmath.Point{X: pos.X, Y: pos.Y}.Center()
	struck := func(e core.Entity) bool {
		return slices.Contains(arced, e) ||
			slices.ContainsFunc(assignments, func(a TargetAssignment) bool { return a.Target == e })
	}
	composites, singles := nearestTargets(s.world, fromX, fromY, engine.ScopeBoth, cursor)
	for _, group := range [2][]TargetAssignment{composites, singles} {
		var best *TargetAssignment
		for i := range group {
			c := &group[i]
			if c.DistSq <= arcRange*arcRange && !struck(c.Target) && (best == nil || c.DistSq < best.DistSq) {
				best = c
			}
		}
		if best == nil {
			continue
		}
		s.world.PushEventDomain(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: best.Target,
			HitEntity:    best.Hit,
			HasOrigin:    true,
			OriginX:      pos.X,
			OriginY:      pos.Y,
		}, best.Target.Domain())
		return append(arced, best.Target)
	}
	return arced
}

// fireMissiles launches one missile per charge from the emitter cell at the assigned targets
func (s *WeaponSystem) fireMissiles(cursor core.Entity, x, y, count int, assignments []TargetAssignment) bool {
	if len(assignments) == 0 {
		return false
	}
	targets := make([]core.Entity, len(assignments))
	hits := make([]core.Entity, len(assignments))
	for i, a := range assignments {
		targets[i] = a.Target
		hits[i] = a.Hit
	}
	s.world.PushLocal(event.EventMissileSpawnRequest, &event.MissileSpawnRequestPayload{
		OwnerEntity: cursor,
		OriginX:     x,
		OriginY:     y,
		Count:       count,
		Targets:     targets,
		HitEntities: hits,
	})
	return true
}

// fireBullets shoots one spread bullet per assignment from the emitter cell at the member it names
func (s *WeaponSystem) fireBullets(cursor core.Entity, x, y int, attack component.CombatAttackType, assignments []TargetAssignment) bool {
	originX, originY := vmath.Point{X: x, Y: y}.Center()
	fired := false
	for _, a := range assignments {
		pos, ok := s.world.Positions.GetPosition(a.Hit)
		if !ok {
			continue
		}
		targetX, targetY := vmath.Point{X: pos.X, Y: pos.Y}.Center()
		dirX, dirY := vmath.Normalize2D(targetX-originX, targetY-originY)
		if dirX == 0 && dirY == 0 {
			continue
		}
		spread := (s.rng.Float64() - 0.5) * 2 * parameter.TurretSpreadHalfAngle
		dirX, dirY = vmath.RotateVector(dirX, dirY, spread)
		s.world.PushLocal(event.EventBulletSpawnRequest, &event.BulletSpawnRequestPayload{
			OriginX:     originX,
			OriginY:     originY,
			VelX:        dirX * parameter.TurretBulletSpeed,
			VelY:        dirY * parameter.TurretBulletSpeed,
			Owner:       cursor,
			MaxLifetime: parameter.TurretBulletLifetime,
			Attack:      attack,
		})
		fired = true
	}
	return fired
}

// firePulse fires a pulse that stays on the emitter cell, when a target is inside
// its reach; the pulse system strikes with it this tick, a charged one carrying the
// family's charged chain
func (s *WeaponSystem) firePulse(cursor core.Entity, x, y int, spec *component.WeaponSpec, charged bool) bool {
	pulse := spec.Pulse
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(pulse.Radius, s.world.Resources.Config.CellAspect.Y(pulse.Radius/2))
	if len(FindTargetsInEllipse(s.world, x, y, invRxSq, invRySq, engine.ScopeBoth, cursor)) == 0 {
		return false
	}
	pulse.Attack, pulse.Charged = spec.Attack, charged
	e := s.world.CreateEntity(core.DomainPlayer)
	s.world.Components.Pulse.SetComponent(e, component.PulseComponent{
		PulseSpec: pulse, X: x, Y: y, Owner: cursor, Palette: s.palette(cursor),
	})
	return true
}

// fireRay lights a ray from the cursor through its orb, lasting longer and
// striking harder the more charges it holds; advanceRays carries it from there
func (s *WeaponSystem) fireRay(cursor, orb core.Entity, charges int) bool {
	if orb == 0 {
		return false
	}
	duration := parameter.RayDuration + time.Duration(charges-1)*parameter.RayDurationPerCharge
	ray := component.RayComponent{
		Phase: component.RayFiring, Remaining: duration, Duration: duration,
		Scale: charges, Palette: s.palette(cursor),
	}
	s.layRay(cursor, orb, &ray)
	s.world.Components.Ray.SetComponent(orb, ray)
	return true
}

// advanceRays re-lays each of a cursor's rays through its orb as the orb orbits
// and strikes what it covers every tick: a sweep crosses a far target in about one,
// and combat's per-attacker immunity is what rates each target. A held ray stays at
// full strength; one whose hold has lapsed burns down like any timed ray
func (s *WeaponSystem) advanceRays(cursor core.Entity, orbs orbSlots, dt time.Duration) {
	rays := s.world.Components.Ray
	for wt, orb := range orbs {
		ray, ok := rays.GetPtr(orb)
		if !ok {
			continue
		}
		if ray.Hold = max(ray.Hold-dt, 0); ray.Hold > 0 {
			ray.Remaining = ray.Duration
		} else if ray.Remaining -= dt; ray.Remaining <= 0 {
			rays.RemoveEntity(orb, false)
			continue
		}
		s.layRay(cursor, orb, ray)
		s.strikeRay(cursor, ray, component.WeaponSpecs[wt].Attack)
	}
}

// layRay runs a ray from the cursor's cell through its orb's: one cell wide up to
// the orb and RayWidth past it, to the first wall or the map edge. An orb on the
// cursor's own cell leaves the last ray in place.
func (s *WeaponSystem) layRay(cursor, orb core.Entity, ray *component.RayComponent) {
	from, ok := s.world.CursorCell(cursor)
	if !ok {
		return
	}
	to, ok := s.world.Positions.GetPosition(orb)
	dx, dy := to.X-from.X, to.Y-from.Y
	if !ok || (dx == 0 && dy == 0) {
		return
	}
	ray.Ray = vmath.Ray{
		X: from.X, Y: from.Y, DX: float64(dx), DY: float64(dy),
		Knee: max(vmath.IntAbs(dx), vmath.IntAbs(dy)), Far: (parameter.RayWidth - 1) / 2,
	}
	ray.Ray.Length = traceRay(s.world, ray.Ray)
}

// strikeRay hits every combat target group a ray covers once: a drain's locally,
// a Shared target's as one crossing naming its members and the owner (D-3)
func (s *WeaponSystem) strikeRay(cursor core.Entity, ray *component.RayComponent, attack component.CombatAttackType) {
	for _, g := range FindTargetsIn(s.world, ray.Ray.Contains, engine.ScopeBoth, cursor) {
		hit := &event.CombatAttackAreaRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: g.Target,
			HitEntities:  g.Members,
			HasOrigin:    true,
			OriginX:      ray.Ray.X,
			OriginY:      ray.Ray.Y,
			Scale:        uint8(ray.Scale),
		}
		if g.Target.Domain() == core.DomainShared {
			s.world.PushCrossing(event.EventCombatAttackAreaCrossingRequest, hit)
		} else {
			s.world.PushLocal(event.EventCombatAttackAreaRequest, hit)
		}
	}
}

// palette is the colour a cursor's discharge draws in, by its energy polarity
func (s *WeaponSystem) palette(cursor core.Entity) component.WeaponPalette {
	if energy, ok := s.world.Components.Energy.GetPtr(cursor); ok && energy.Current < 0 {
		return component.PaletteNegative
	}
	return component.PalettePositive
}

// publishLoadout mirrors one cursor's owned weapons into its roster slot
func (s *WeaponSystem) publishLoadout(cursor core.Entity, weaponComp *component.WeaponComponent) {
	slot, ok := s.world.CursorSlot(cursor)
	if !ok {
		return
	}
	for wt, charges := range weaponComp.Charges {
		s.statHeld[wt].Store(slot, charges > 0)
	}
}

// publishSlots mirrors every rostered cursor's loadout, a peer's included. Orb
// counts are not here: they are published from the orb index, which holds every
// participant's already. See eachRosterSlot.
func (s *WeaponSystem) publishSlots() {
	eachRosterSlot(s.world, func(slot uint8, cursor core.Entity) {
		weapon, ok := s.world.Components.Weapon.GetPtr(cursor)
		if !ok {
			s.clearLoadout(slot)
			return
		}
		s.publishLoadout(cursor, weapon)
	})
}

// clearLoadout zeroes one slot's held-weapon cells
func (s *WeaponSystem) clearLoadout(slot uint8) {
	for wt := range component.WeaponCount {
		s.statHeld[wt].Store(slot, false)
	}
}

// clearSlot zeroes a retired slot's cells
func (s *WeaponSystem) clearSlot(slot uint8) {
	s.clearLoadout(slot)
	s.statOrbs.Store(slot, 0)
}

// fireTracers launches a volley from a cell: one streak per lane it fires along each
// cardinal, lane by lane across the cardinals, skipping a lane that starts in a wall
// or off the map. Every tracer is this instance's own (D-6).
func (s *WeaponSystem) fireTracers(cursor core.Entity, originX, originY int, volley component.GunLevel, palette component.WeaponPalette) bool {
	config := s.world.Resources.Config
	minFlightX, maxFlightX := tracerFlightBounds(config.MapWidth)
	minFlightY, maxFlightY := tracerFlightBounds(config.MapHeight)
	ox, oy := vmath.Point{X: originX, Y: originY}.Center()

	fired := false
	for lane := range len(parameter.TracerLaneOffsets) {
		for _, d := range component.TracerCardinals {
			n := volleyLanes(volley, d)
			if n == 0 {
				continue
			}
			offsets := tracerLanes(n)
			if lane >= len(offsets) {
				continue
			}
			off := float64(offsets[lane])
			var vx, vy, sx, sy, tx, ty float64
			switch d {
			case component.TracerRight:
				vx, sx, sy, tx, ty = parameter.TracerHorizontalSpeed, ox, oy+off, maxFlightX, oy+off
			case component.TracerLeft:
				vx, sx, sy, tx, ty = -parameter.TracerHorizontalSpeed, ox, oy+off, minFlightX, oy+off
			case component.TracerDown:
				vy, sx, sy, tx, ty = parameter.TracerVerticalSpeed, ox+off, oy, ox+off, maxFlightY
			case component.TracerUp:
				vy, sx, sy, tx, ty = -parameter.TracerVerticalSpeed, ox+off, oy, ox+off, minFlightY
			}
			start := vmath.PointAt(sx, sy)
			if s.world.Positions.IsOutOfBounds(start.X, start.Y) ||
				s.world.Positions.HasBlockingWallAt(start.X, start.Y, component.WallBlockKinetic) {
				continue
			}
			s.spawnTracer(cursor, palette, start, vx, vy, sx, sy, tx, ty)
			fired = true
		}
	}
	if fired {
		s.world.PushLocal(event.EventSoundRequest, &event.SoundRequestPayload{ID: parameter.Sfx.Bullet})
	}
	return fired
}

// spawnTracer creates one streak: its cell first so the spatial index holds it, then its components
func (s *WeaponSystem) spawnTracer(owner core.Entity, palette component.WeaponPalette, start vmath.Point, vx, vy, sx, sy, tx, ty float64) {
	var trail [parameter.TracerTrailLength]vmath.Point
	trail[0] = start
	entity := s.world.CreateEntity(core.DomainPlayer)
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: start.X, Y: start.Y})
	s.world.Components.Tracer.SetComponent(entity, component.TracerComponent{
		OwnerEntity: owner, TargetX: tx, TargetY: ty, TrailRing: trail, TrailLen: 1, Palette: palette,
	})
	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{
		Kinetic: physics.Kinetic{PreciseX: sx, PreciseY: sy, VelX: vx, VelY: vy},
	})
	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectFromSpecies | component.ProtectFromDeath,
	})
}

// advanceTracers moves every streak, sweeping each crossed cell for walls, combat
// targets and glyphs, and drains a blocked one's trail to its stop point
func (s *WeaponSystem) advanceTracers() {
	config := s.world.Resources.Config

	// Tracers are destroyed immediately to free their spatial cells for later
	// tracers in this tick, so the entity order must remain detached.
	s.tracerBuf = append(s.tracerBuf[:0], s.world.Components.Tracer.Entities()...)
	s.buffers.Observe(0, len(s.tracerBuf))
	s.statTracers.Store(int64(len(s.tracerBuf)))
	if len(s.tracerBuf) == 0 {
		return
	}

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), parameter.MaxSimulationDeltaSeconds)
	gameWidth := config.MapWidth
	gameHeight := config.MapHeight

	for _, tracerEntity := range s.tracerBuf {
		tracer, ok := s.world.Components.Tracer.GetPtr(tracerEntity)
		if !ok {
			continue
		}

		if tracer.Blocked {
			tracer.DrainRemaining -= tracer.DrainSpeed * dtSec
			if tracer.DrainRemaining <= 0 {
				s.world.DestroyEntity(tracerEntity)
			}
			continue
		}

		kineticComp, ok := s.world.Components.Kinetic.GetPtr(tracerEntity)
		if !ok {
			continue
		}
		oldPos, ok := s.world.Positions.GetPosition(tracerEntity)
		if !ok {
			continue
		}

		prevPreciseX := kineticComp.PreciseX
		prevPreciseY := kineticComp.PreciseY
		physics.Integrate(&kineticComp.Kinetic, dtSec)
		prevCell := vmath.PointAt(prevPreciseX, prevPreciseY)
		newCell := vmath.PointAt(kineticComp.PreciseX, kineticComp.PreciseY)

		blocked := false
		var blockGridX, blockGridY int

		if kineticComp.VelY != 0 && kineticComp.VelX == 0 {
			fromY, toY := prevCell.Y, newCell.Y
			x := oldPos.X
			step := 1
			if kineticComp.VelY < 0 {
				step = -1
			}

			lastValidY := fromY
			for y := fromY + step; (step > 0 && y <= toY) || (step < 0 && y >= toY); y += step {
				s.statTracerSteps.Add(1)
				// Skip OOB cells (the tracer flies off-screen, lifecycle handles destruction)
				if y < 0 || y >= gameHeight {
					s.statTracerBounds.Add(1)
					appendTracerTrail(tracer, x, y)
					lastValidY = y
					continue
				}

				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
					s.statTracerWalls.Add(1)
					blocked = true
					blockGridX, blockGridY = x, lastValidY
					break
				}

				// Combat + glyph; the combat target blocks the head at this cell.
				if s.tracerStrike(x, y, tracerEntity, tracer.OwnerEntity,
					kineticComp.VelX, kineticComp.VelY, tracer.Palette) {
					blocked = true
					blockGridX, blockGridY = x, y
					appendTracerTrail(tracer, x, y)
					break
				}

				appendTracerTrail(tracer, x, y)
				lastValidY = y
			}
		} else if kineticComp.VelX != 0 {
			fromX, toX := prevCell.X, newCell.X
			y := oldPos.Y
			step := 1
			if kineticComp.VelX < 0 {
				step = -1
			}

			lastValidX := fromX
			for x := fromX + step; (step > 0 && x <= toX) || (step < 0 && x >= toX); x += step {
				s.statTracerSteps.Add(1)
				if x < 0 || x >= gameWidth {
					s.statTracerBounds.Add(1)
					appendTracerTrail(tracer, x, y)
					lastValidX = x
					continue
				}

				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
					s.statTracerWalls.Add(1)
					blocked = true
					blockGridX, blockGridY = lastValidX, y
					break
				}

				if s.tracerStrike(x, y, tracerEntity, tracer.OwnerEntity,
					kineticComp.VelX, kineticComp.VelY, tracer.Palette) {
					blocked = true
					blockGridX, blockGridY = x, y
					appendTracerTrail(tracer, x, y)
					break
				}

				appendTracerTrail(tracer, x, y)
				lastValidX = x
			}
		}

		if blocked {
			tracer.Blocked = true

			// Capture drain speed from current velocity (don't zero — combat events need it)
			drainSpeed := kineticComp.VelX
			if drainSpeed < 0 {
				drainSpeed = -drainSpeed
			}
			if drainSpeed == 0 {
				drainSpeed = kineticComp.VelY
				if drainSpeed < 0 {
					drainSpeed = -drainSpeed
				}
			}
			tracer.DrainSpeed = drainSpeed

			drainDist := float64(tracer.TrailLen)
			tracer.DrainRemaining = drainDist
			tracer.DrainTotal = drainDist

			// Update precise position to block point (velocity preserved for combat resolution)
			kineticComp.PreciseX, kineticComp.PreciseY = vmath.Point{X: blockGridX, Y: blockGridY}.Center()
			s.world.Positions.SetPosition(tracerEntity, component.PositionComponent{X: blockGridX, Y: blockGridY})
			continue
		}

		if newCell.X != oldPos.X || newCell.Y != oldPos.Y {
			s.world.Positions.SetPosition(tracerEntity, component.PositionComponent{X: newCell.X, Y: newCell.Y})
		}

		reached := (kineticComp.VelX > 0 && kineticComp.PreciseX >= tracer.TargetX) ||
			(kineticComp.VelX < 0 && kineticComp.PreciseX <= tracer.TargetX) ||
			(kineticComp.VelY > 0 && kineticComp.PreciseY >= tracer.TargetY) ||
			(kineticComp.VelY < 0 && kineticComp.PreciseY <= tracer.TargetY)
		if reached {
			s.world.DestroyEntity(tracerEntity)
		}
	}
}

// appendTracerTrail records every crossed cell, rather than only the final
// cell of a tick. This keeps fast tracers visually continuous and mirrors the
// swept collision path without allocating.
func appendTracerTrail(tracer *component.TracerComponent, x, y int) {
	if tracer.TrailLen > 0 {
		last := tracer.TrailRing[tracer.TrailHead]
		if last.X == x && last.Y == y {
			return
		}
	}

	tracer.TrailHead = (tracer.TrailHead + 1) % parameter.TracerTrailLength
	tracer.TrailRing[tracer.TrailHead] = vmath.Point{X: x, Y: y}
	if tracer.TrailLen < parameter.TracerTrailLength {
		tracer.TrailLen++
	}
}

// aimed reports whether a delivery needs targets assigned before it fires
func aimed(d component.WeaponDelivery) bool {
	return d != component.DeliveryPulse && d != component.DeliveryTracer
}

// tracerLanes are the offsets a volley of charges covers; charges past the table cover every lane
func tracerLanes(charges int) []int {
	return parameter.TracerLaneOffsets[max(min(charges, len(parameter.TracerLaneOffsets)), 1)-1]
}

// gunVolley is what a gun of charges fires: the level they count to on its path,
// the path's last past its end, or off one a lane per charge along each cardinal it
// aims at
func gunVolley(weaponComp *component.WeaponComponent, spec *component.WeaponSpec, charges int) component.GunLevel {
	if weaponComp.GunLevels > 0 {
		return weaponComp.GunPath[max(min(charges, weaponComp.GunLevels), 1)-1]
	}
	directions := cmp.Or(weaponComp.Aim, spec.Directions)
	along := func(d component.TracerDirection) uint8 {
		if directions&d == 0 {
			return 0
		}
		return uint8(min(charges, len(parameter.TracerLaneOffsets)))
	}
	return component.GunLevel{
		Right: along(component.TracerRight), Left: along(component.TracerLeft),
		Down: along(component.TracerDown), Up: along(component.TracerUp),
	}
}

// volleyLanes is how many lanes a volley fires along one cardinal
func volleyLanes(volley component.GunLevel, d component.TracerDirection) int {
	switch d {
	case component.TracerRight:
		return int(volley.Right)
	case component.TracerLeft:
		return int(volley.Left)
	case component.TracerDown:
		return int(volley.Down)
	case component.TracerUp:
		return int(volley.Up)
	}
	return 0
}

// tracerFlightBounds returns cell-centered off-map targets far enough for a
// full trail to clear the map, with no extra off-map lifetime.
func tracerFlightBounds(size int) (negative, positive float64) {
	margin := float64(parameter.TracerTrailLength)
	return -margin + 0.5, float64(size) + margin - 0.5
}

// tracerStrike handles combat and glyph interactions at a single cell
// Returns true if a combat entity was hit (blocks the tracer's head)
func (s *WeaponSystem) tracerStrike(x, y int, selfEntity, owner core.Entity, velX, velY float64, palette component.WeaponPalette) bool {
	var entityBuf [parameter.MaxEntitiesPerCell]core.Entity
	n := s.world.Positions.GetAllEntitiesAtInto(x, y, entityBuf[:])
	if n == 0 {
		return false
	}
	entities := entityBuf[:n]

	blocked := false

	for _, entity := range entities {
		if entity == 0 || entity == selfEntity {
			continue
		}

		// Fast-path: skip other tracers (frequent co-location, no combat components)
		if s.world.Components.Tracer.HasEntity(entity) {
			continue
		}

		target, hit, valid := ResolveTargetFromEntity(s.world, entity, selfEntity)
		if !valid {
			continue
		}

		// Skip the player side: cursors, their orbs, the tower, the firing cursor's own.
		if isPlayerSide(s.world, target, owner) {
			continue
		}

		// Resolved by the target: a tracer hit on a shared species crosses, one on a
		// player entity does not (D-3, D-10).
		s.world.PushEventDomain(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
			AttackType:   component.CombatAttackTracer,
			OwnerEntity:  owner,
			OriginEntity: owner,
			TargetEntity: target,
			HitEntity:    hit,
			HasOrigin:    true,
			OriginX:      x,
			OriginY:      y,
			HasVelocity:  true,
			OriginVelX:   velX,
			OriginVelY:   velY,
		}, target.Domain())
		blocked = true
	}

	switch palette {
	case component.PalettePositive:
		s.blossomRedGlyphs(entities, selfEntity)
	case component.PaletteNegative:
		s.decayBlueGlyphs(x, y, entities, selfEntity)
	}

	return blocked
}

// blossomRedGlyphs destroys the cell's red glyphs with a blossom particle each
func (s *WeaponSystem) blossomRedGlyphs(targetEntities []core.Entity, selfEntity core.Entity) {
	s.glyphBuf = s.glyphBuf[:0]

	for _, targetEntity := range targetEntities {
		if targetEntity == 0 || targetEntity == selfEntity {
			continue
		}
		// Glyph mechanics are player-domain; a shared glyph is a gold member
		if targetEntity.Domain() != core.DomainPlayer {
			continue
		}
		if glyphComp, ok := s.world.Components.Glyph.GetPtr(targetEntity); ok {
			if glyphComp.Type == component.GlyphRed {
				s.glyphBuf = append(s.glyphBuf, targetEntity)
			}
		}
	}
	s.buffers.Observe(1, len(s.glyphBuf))

	if len(s.glyphBuf) == 0 {
		return
	}

	event.EmitParticleDeath(s.world.Resources.Event.Queue, component.ParticleBlossom, s.glyphBuf...)
}

// decayBlueGlyphs mutates the cell's blue glyphs to green with a decay particle each
func (s *WeaponSystem) decayBlueGlyphs(x, y int, targetEntities []core.Entity, selfEntity core.Entity) {
	for _, targetEntity := range targetEntities {
		if targetEntity == 0 || targetEntity == selfEntity {
			continue
		}
		// Glyph mechanics are player-domain; a shared glyph is a gold member
		if targetEntity.Domain() != core.DomainPlayer {
			continue
		}

		glyphComp, ok := s.world.Components.Glyph.GetPtr(targetEntity)
		if !ok || glyphComp.Type != component.GlyphBlue {
			continue
		}

		glyphComp.Type = component.GlyphGreen

		// Spawn decay at same position (particle skips starting cell via LastIntX/Y)
		s.world.PushLocal(event.EventParticleSpawnOne, &event.ParticleSpawnPayload{
			Behavior:      component.ParticleDecay,
			X:             x,
			Y:             y,
			Char:          glyphComp.Rune,
			SkipStartCell: true,
		})
	}
}
