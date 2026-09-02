package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/navigation"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// DropResult holds a single drop outcome
type DropResult struct {
	Loot  component.LootType
	Count int
}

// spawnOffsets defines deterministic scatter patterns by count
var spawnOffsets = [][]struct{ dx, dy int }{
	{},                                 // 0: unused
	{{0, 0}},                           // 1: center
	{{-1, 0}, {1, 0}},                  // 2: horizontal
	{{-1, 0}, {1, 0}, {0, -1}},         // 3: T-shape
	{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}, // 4: cross
	{{-1, 0}, {1, 0}, {0, -1}, {0, 1}, {0, 0}}, // 5: cross + center
}

// pityState tracks consecutive misses per loot type for a species type
type pityState struct {
	misses [component.LootCount]int
}

// pityKey scopes a pity streak to one cursor's slot; local cursors roll independently
type pityKey struct {
	species component.SpeciesType
	slot    uint8
}

// ownerRoute is one cursor's private flow field: the route every loot that cursor
// owns follows home.
//
// It is not the shared NavigationSystem's, and that is the whole point. Group 0 is
// every live cursor, so the field it maintains is a multi-source Dijkstra whose
// gradient leads to the *nearest* cursor — the right answer for a hostile species
// and the wrong one for a drop that belongs to exactly one participant. Loot
// steered by it walked to whichever cursor was closer and stalled there, and the
// line-of-sight flag beside it was computed against that same nearest cursor while
// the loot homed at its owner, so a loot standing next to the wrong cursor read
// "direct path" and drove itself into the wall between it and its own.
//
// The field is derived from shared walls and one shared cursor cell, but *which*
// fields exist is a function of this instance's own drops, so it is player-domain
// state (D-6): no capture carries it, and its telemetry is under the `loot.` prefix
// the compared surface already drops.
type ownerRoute struct {
	cache *navigation.FlowFieldCache
	cell  vmath.Point
	live  bool // some loot named this owner during this tick
}

type LootSystem struct {
	world *engine.World

	// Loot is player-domain, so it never advances the shared stream
	rng *vmath.FastRand

	// Pity tracking per species type and roster slot
	pity map[pityKey]*pityState

	// One route per owner that currently has loot in flight, and the single-element
	// target slice its recompute is fed from
	ownerRoutes map[core.Entity]*ownerRoute
	routeGoal   [1]vmath.Point

	// Telemetry
	statDrops       *atomic.Int64
	statActive      *atomic.Int64
	statCollects    *atomic.Int64
	statRoutes      *atomic.Int64
	statRecomputes  *atomic.Int64
	statUnreachable *atomic.Int64
	buffers         bufferTelemetry
	motion          bounceTelemetry

	enabled bool
}

func NewLootSystem(world *engine.World) engine.System {
	s := &LootSystem{
		world: world,
	}

	s.statDrops = world.Resources.Status.Ints.Get("loot.drops")
	s.statActive = world.Resources.Status.Ints.Get("loot.active")
	s.statCollects = world.Resources.Status.Ints.Get("loot.collects")
	// Owner routes: how many are live, how often they are rebuilt, and how many
	// drops are walled off from the cursor that owns them
	s.statRoutes = world.Resources.Status.Ints.Get("loot.routes")
	s.statRecomputes = world.Resources.Status.Ints.Get("loot.route_recomputes")
	s.statUnreachable = world.Resources.Status.Ints.Get("loot.unreachable")
	s.buffers = newBufferTelemetry(world.Resources.Status, "loot", "pity", "routes")
	s.motion = newBounceTelemetry(world.Resources.Status, "loot")

	s.Init()
	return s
}

func (s *LootSystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.pity = make(map[pityKey]*pityState)
	s.ownerRoutes = make(map[core.Entity]*ownerRoute)
	s.statDrops.Store(0)
	s.statActive.Store(0)
	s.statCollects.Store(0)
	s.statRoutes.Store(0)
	s.statRecomputes.Store(0)
	s.statUnreachable.Store(0)
	s.buffers.Reset()
	s.motion.Reset()

	s.enabled = true
}

func (s *LootSystem) Name() string {
	return "loot"
}

func (s *LootSystem) Priority() int {
	return parameter.PriorityLoot
}

func (s *LootSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSpeciesKilled,
		event.EventLootSpawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *LootSystem) HandleEvent(ev event.GameEvent) {
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
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventSpeciesKilled:
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			s.onSpeciesKilled(payload)
		}

	case event.EventLootSpawnRequest:
		// Direct spawn names no owner; it belongs to the local cursor
		if payload, ok := ev.Payload.(*event.LootSpawnRequestPayload); ok {
			s.spawnLootMulti([]component.LootType{payload.Type}, payload.X, payload.Y,
				s.world.Resources.Player.Entity)
		}
	}
}

func (s *LootSystem) Update() {
	if !s.enabled {
		return
	}

	// collectLoot destroys the current entity immediately, so iteration needs a
	// detached entity list even though surviving kinetic components mutate in place.
	lootEntities := s.world.Components.Loot.GetAllEntities()
	if len(lootEntities) == 0 {
		s.statActive.Store(0)
		s.statUnreachable.Store(0)
		return
	}

	config := s.world.Resources.Config
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	s.refreshOwnerRoutes(lootEntities)

	var activeCount int64
	var unreachable int64
	for _, lootEntity := range lootEntities {
		lootComp, ok := s.world.Components.Loot.GetPtr(lootEntity)
		if !ok {
			continue
		}

		kineticComp, ok := s.world.Components.Kinetic.GetPtr(lootEntity)
		if !ok {
			continue
		}

		curX, curY := physics.GridPos(&kineticComp.Kinetic)
		owner := lootComp.Owner
		ownerPos, hasOwner := s.world.Positions.GetPosition(owner)

		// Collection check
		if hasOwner && vmath.IntAbs(curX-ownerPos.X) <= parameter.LootCollectRadius &&
			vmath.IntAbs(curY-ownerPos.Y) <= parameter.LootCollectRadius {
			s.collectLoot(owner, lootEntity, lootComp.Type)
			continue
		}

		targetX, targetY, routed := s.homingTarget(lootComp, kineticComp, curX, curY, ownerPos, hasOwner)
		if routed {
			// Cornering brake, as every homing species applies it: acceleration
			// alone conserves the sideways component of an approach, which is what
			// left a drop circling its own cursor until something else moved it.
			turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
				parameter.NavCorneringThreshold, 1.0)
			physics.ApplyHoming(&kineticComp.Kinetic, targetX, targetY, &profile.LootHoming, dtSec)
			if turnSeverity > 0 {
				physics.ApplyLinearDrag(&kineticComp.Kinetic,
					turnSeverity*parameter.NavCorneringBrake, dtSec)
			}
		} else {
			// Owner gone, or walled off from every route: bleed to rest rather than
			// press against whatever is in the way.
			unreachable++
			physics.ApplyLinearDrag(&kineticComp.Kinetic, parameter.LootVelocityBleed, dtSec)
			if vmath.AbsF(kineticComp.VelX) < parameter.LootStopSpeed &&
				vmath.AbsF(kineticComp.VelY) < parameter.LootStopSpeed {
				kineticComp.VelX, kineticComp.VelY = 0, 0
			}
		}

		newGridX, newGridY, motion := physics.IntegrateWithBounceStats(
			&kineticComp.Kinetic,
			dtSec,
			0, 0,
			0, config.MapWidth,
			0, config.MapHeight,
			parameter.LootRestitution,
			func(tx, ty int) bool {
				return s.world.Positions.IsBlocked(tx, ty, component.WallBlockKinetic)
			},
		)
		s.motion.Record(motion)

		if newGridX != curX || newGridY != curY {
			s.world.Positions.SetPosition(lootEntity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		activeCount++
	}
	s.statActive.Store(activeCount)
	s.statUnreachable.Store(unreachable)
}

// refreshOwnerRoutes brings one flow field up to date per cursor that currently has
// loot in flight, and drops the rest.
//
// The goal is that cursor's cell and nothing else, which is the difference that
// matters: a drop belongs to one participant, so the field it steers by must lead
// to that participant and not to whoever happens to be nearer. A field is built the
// first time that cursor drops something and released when the cursor itself goes;
// in between, a tick with none of its loot in flight recomputes nothing.
func (s *LootSystem) refreshOwnerRoutes(loots []core.Entity) {
	for _, r := range s.ownerRoutes {
		r.live = false
	}

	for _, lootEntity := range loots {
		lootComp, ok := s.world.Components.Loot.GetComponent(lootEntity)
		if !ok {
			continue
		}
		pos, ok := s.world.Positions.GetPosition(lootComp.Owner)
		if !ok {
			continue // owner despawned; its drops bleed to rest
		}
		r, ok := s.ownerRoutes[lootComp.Owner]
		if !ok {
			r = &ownerRoute{}
			s.ownerRoutes[lootComp.Owner] = r
		}
		r.live = true
		r.cell = vmath.Point{X: pos.X, Y: pos.Y}
	}

	config := s.world.Resources.Config
	if config.MapWidth <= 0 || config.MapHeight <= 0 {
		return
	}
	blocked := func(x, y int) bool {
		return s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic)
	}

	var recomputes int64
	for owner, r := range s.ownerRoutes {
		if !s.world.Positions.HasPosition(owner) {
			// The cursor is gone; so is any reason to hold a field aimed at it.
			delete(s.ownerRoutes, owner)
			continue
		}
		if !r.live {
			// A field is a map-sized allocation and a cursor keeps dropping loot,
			// so an idle owner keeps its field and stops paying for it: the next
			// drop finds the throttle where it left it, and Update recomputes
			// because the cursor has moved since.
			continue
		}
		switch {
		case r.cache == nil:
			r.cache = navigation.NewFlowFieldCache(
				config.MapWidth, config.MapHeight,
				parameter.NavFlowMinTicksBetweenCompute,
				parameter.NavFlowDirtyDistance,
			)
		case r.cache.Field.Width != config.MapWidth || r.cache.Field.Height != config.MapHeight:
			r.cache.Resize(config.MapWidth, config.MapHeight)
		}
		s.routeGoal[0] = r.cell
		if r.cache.Update(s.routeGoal[:], blocked) {
			recomputes++
		}
	}
	s.statRoutes.Store(int64(len(s.ownerRoutes)))
	s.statRecomputes.Add(recomputes)
	s.buffers.Observe(1, len(s.ownerRoutes))
}

// homingTarget resolves one drop's steering target: its owner when the way there is
// clear, and the next step of that owner's route when it is not.
//
// The two answers agree about which cursor they mean, which is what the shared
// navigation component could not promise. Its line-of-sight flag was computed
// against the nearest cursor in the roster while the movement homed at the owner,
// so a drop beside someone else's cursor read "direct path" and accelerated into
// the wall between it and its own.
//
// The lookahead is capped by the remaining distance to the owner. Homing at a point
// a fixed distance ahead is a target that recedes as fast as the drop approaches
// it, so the arrival steering in the profile never engages and the drop reaches
// full cruising speed in a corridor one cell wide; capping it means the last
// stretch of a blocked approach is steered like an arrival rather than a charge.
func (s *LootSystem) homingTarget(
	lootComp *component.LootComponent,
	kineticComp *component.KineticComponent,
	curX, curY int,
	ownerPos component.PositionComponent,
	hasOwner bool,
) (float64, float64, bool) {
	if !hasOwner {
		return 0, 0, false
	}

	ownerCenterX, ownerCenterY := vmath.Point{X: ownerPos.X, Y: ownerPos.Y}.CenterF()
	if s.world.Positions.HasLineOfSight(curX, curY, ownerPos.X, ownerPos.Y, component.WallBlockKinetic) {
		return ownerCenterX, ownerCenterY, true
	}

	r, ok := s.ownerRoutes[lootComp.Owner]
	if !ok || r.cache == nil {
		return 0, 0, false
	}
	flowX, flowY := navigation.InterpolatedDirection(r.cache, kineticComp.PreciseX, kineticComp.PreciseY)
	if flowX == 0 && flowY == 0 {
		return 0, 0, false
	}

	lookahead := vmath.ClampF(
		vmath.MagnitudeF(ownerCenterX-kineticComp.PreciseX, ownerCenterY-kineticComp.PreciseY),
		1.0, parameter.LootFlowLookahead)
	return kineticComp.PreciseX + flowX*lookahead, kineticComp.PreciseY + flowY*lookahead, true
}

// --- Drop Resolution ---

// onSpeciesKilled rolls the drop table once per locally simulated cursor; a kill
// produces this instance's drops only, so ownership is never read across instances.
func (s *LootSystem) onSpeciesKilled(payload *event.SpeciesKilledPayload) {
	// A negative coordinate marks a death with no position; nothing to drop onto
	if payload.X < 0 || payload.Y < 0 {
		return
	}

	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor == 0 || !s.world.SimulatesLocally(cursor) {
			continue
		}

		results := s.rollDropTable(payload.Species, cursor, uint8(i))
		if len(results) == 0 {
			continue
		}

		// Flatten results into spawn list
		var spawns []component.LootType
		for _, r := range results {
			for range r.Count {
				spawns = append(spawns, r.Loot)
			}
		}
		if len(spawns) == 0 {
			continue
		}

		s.spawnLootMulti(spawns, payload.X, payload.Y, cursor)
	}
}

// --- Spawn ---

// spawnLootMulti spawns multiple loot items with scatter pattern and initial burst velocity
func (s *LootSystem) spawnLootMulti(loots []component.LootType, cx, cy int, owner core.Entity) {
	count := len(loots)
	if count == 0 {
		return
	}

	// Clamp to pattern table size
	patternIdx := count
	if patternIdx >= len(spawnOffsets) {
		patternIdx = len(spawnOffsets) - 1
	}
	pattern := spawnOffsets[patternIdx]

	for i, lootType := range loots {
		// Cycle through pattern if more items than offsets
		offset := pattern[i%len(pattern)]
		spawnX, spawnY := cx+offset.dx, cy+offset.dy

		// Calculate burst direction from offset (before validation may change position)
		burstDirX, burstDirY := offset.dx, offset.dy

		// Validate position, fallback to center
		if !s.isValidSpawnPos(spawnX, spawnY) {
			spawnX, spawnY = cx, cy
			if !s.isValidSpawnPos(spawnX, spawnY) {
				// Last resort: find any free cell nearby
				if freeX, freeY, found := s.world.Positions.FindFreeFromPattern(
					cx, cy, 1, 1,
					engine.PatternCardinalFirst,
					1, 5, true,
					component.WallBlockKinetic, nil,
				); found {
					spawnX, spawnY = freeX, freeY
					// Update burst direction based on fallback position
					burstDirX, burstDirY = freeX-cx, freeY-cy
				} else {
					continue // Skip this loot if no valid position
				}
			}
		}

		s.spawnLootWithBurst(lootType, spawnX, spawnY, burstDirX, burstDirY, owner)
		s.statDrops.Add(1)
	}
}

// spawnLootWithBurst creates an owned loot entity with initial velocity in burst direction
func (s *LootSystem) spawnLootWithBurst(lootType component.LootType, x, y, burstDirX, burstDirY int, owner core.Entity) {
	vis, ok := visual.LootVisuals[lootType]
	if !ok {
		return
	}

	entity := s.world.CreateEntity(core.DomainPlayer)
	preciseX, preciseY := vmath.Point{X: x, Y: y}.CenterF()

	// Calculate initial burst velocity
	var velX, velY float64
	if burstDirX != 0 || burstDirY != 0 {
		dirX, dirY := vmath.Normalize2DF(float64(burstDirX), float64(burstDirY))
		velX = dirX * parameter.LootBurstSpeed
		velY = dirY * parameter.LootBurstSpeed
	}

	// Loot component
	s.world.Components.Loot.SetComponent(entity, component.LootComponent{
		Type:     lootType,
		Owner:    owner,
		LastIntX: x,
		LastIntY: y,
	})

	// Kinetic with initial burst velocity
	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
			VelX:     velX,
			VelY:     velY,
		},
	})

	// Shield
	cfg := &visual.ShieldConfigs[component.ShieldTypeLoot]
	s.world.Components.Shield.SetComponent(entity, component.ShieldComponent{
		Active:  true,
		Type:    component.ShieldTypeLoot,
		RadiusX: cfg.RadiusX,
		RadiusY: cfg.RadiusY,
		InvRxSq: cfg.InvRxSq,
		InvRySq: cfg.InvRySq,
	})

	// Position
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// Sigil
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  vis.Rune,
		Color: vis.InnerColor,
	})

	// Protection
	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectFromSpecies | component.ProtectFromDecay,
	})
}

// isValidSpawnPos checks if position is within bounds and not blocked
func (s *LootSystem) isValidSpawnPos(x, y int) bool {
	config := s.world.Resources.Config
	if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
		return false
	}
	return !s.world.Positions.IsBlocked(x, y, component.WallBlockKinetic)
}

// rollDropTable processes tiered drop tables with pity and fallback accumulation
// for one cursor. Returns slice of drop results (may be empty)
func (s *LootSystem) rollDropTable(speciesType component.SpeciesType, cursor core.Entity, slot uint8) []DropResult {
	table, ok := component.DropTables[speciesType]
	if !ok || len(table.Tiers) == 0 {
		return nil
	}

	key := pityKey{species: speciesType, slot: slot}
	state := s.pity[key]
	if state == nil {
		state = &pityState{}
		s.pity[key] = state
		s.buffers.Observe(0, len(s.pity))
	}

	activeLoot := s.getActiveLootTypes(cursor)

	isOwned := func(lt component.LootType) bool {
		if activeLoot[lt] {
			return true
		}
		profile := component.LootProfiles[lt]
		if profile.Reward == nil || profile.Reward.Type != component.RewardWeapon {
			return false
		}

		// Max-charge check, repeats drops until this cursor is capped
		wt := profile.Reward.WeaponType
		weapons, ok := s.world.Components.Weapon.GetComponent(cursor)
		return ok && weapons.Charges[wt] >= parameter.WeaponMaxCharges[wt]
	}

	var results []DropResult
	fallbackBonus := 0

	for _, tier := range table.Tiers {
		// Unique tier: skip if all entries owned, accumulate fallback
		if tier.Unique {
			allOwned := true
			for _, entry := range tier.Entries {
				if !isOwned(entry.Loot) {
					allOwned = false
					break
				}
			}
			if allOwned {
				// Accumulate fallback from all entries
				for _, entry := range tier.Entries {
					fallbackBonus += entry.FallbackCount
				}
				continue // Next tier
			}
		}

		// Build eligible candidates
		var candidates []candidate
		var totalRate float64

		for i := range tier.Entries {
			entry := &tier.Entries[i]
			if tier.Unique && isOwned(entry.Loot) {
				continue
			}
			rate := entry.BaseRate * float64(1+state.misses[entry.Loot])
			candidates = append(candidates, candidate{entry, rate})
			totalRate += rate
		}

		if len(candidates) == 0 {
			continue
		}

		// Normalize if exceeds 1.0
		if totalRate >= 1.0 {
			for i := range candidates {
				candidates[i].rate /= totalRate
			}
			totalRate = 1.0
		}

		// Roll
		roll := s.rng.Float64()
		var cumulative float64
		var dropped *component.DropEntry

		for _, c := range candidates {
			cumulative += c.rate
			if roll < cumulative {
				dropped = c.entry
				break
			}
		}

		// Update pity for candidates in this tier
		for _, c := range candidates {
			if dropped != nil && c.entry.Loot == dropped.Loot {
				state.misses[c.entry.Loot] = 0
			} else {
				state.misses[c.entry.Loot]++
			}
		}

		if dropped != nil {
			count := dropped.Count
			if count <= 0 {
				count = 1
			}
			// Apply fallback bonus to non-unique tiers
			if !tier.Unique {
				count += fallbackBonus
			}
			results = append(results, DropResult{Loot: dropped.Loot, Count: count})

			// Unique tier dropped: continue to next tier (no fallback accumulation)
			if tier.Unique {
				continue
			}
		}

		// Non-unique tier: stop processing regardless of outcome
		if !tier.Unique {
			break
		}

		// Unique tier miss: accumulate fallback, continue
		if dropped == nil {
			for _, c := range candidates {
				fallbackBonus += c.entry.FallbackCount
			}
		}
	}

	return results
}

// allPlayersCapped reports whether every live cursor has maximum charges for a weapon.
func (s *LootSystem) allPlayersCapped(weaponType component.WeaponType) bool {
	players := 0
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor == 0 {
			continue
		}
		players++
		weapons, ok := s.world.Components.Weapon.GetComponent(cursor)
		if !ok || weapons.Charges[weaponType] < parameter.WeaponMaxCharges[weaponType] {
			return false
		}
	}
	return players > 0
}

// candidate holds entry with pity-adjusted rate
type candidate struct {
	entry *component.DropEntry
	rate  float64
}

// getActiveLootTypes returns the loot types this cursor already has on the map
func (s *LootSystem) getActiveLootTypes(cursor core.Entity) map[component.LootType]bool {
	active := make(map[component.LootType]bool)
	for _, entity := range s.world.Components.Loot.Entities() {
		lootComp, ok := s.world.Components.Loot.GetPtr(entity)
		if !ok || lootComp.Owner != cursor {
			continue
		}
		active[lootComp.Type] = true
	}
	return active
}

// --- Collection ---

func (s *LootSystem) collectLoot(cursor, entity core.Entity, lootType component.LootType) {
	if int(lootType) >= len(component.LootProfiles) {
		s.world.DestroyEntity(entity)
		return
	}

	profile := &component.LootProfiles[lootType]

	// Apply reward
	if profile.Reward != nil {
		switch profile.Reward.Type {
		case component.RewardWeapon:
			s.world.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{
				Entity: cursor,
				Weapon: profile.Reward.WeaponType,
			})

		case component.RewardEnergy:
			s.world.PushLocal(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Entity: cursor,
				Delta:  profile.Reward.Delta,
				Type:   component.EnergyDeltaReward,
			})

		case component.RewardHeat:
			s.world.PushLocal(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: cursor,
				Delta:  profile.Reward.Delta,
			})
		}
	}

	// Visual feedback
	vis := visual.LootVisuals[lootType]
	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		s.world.PushLocal(event.EventFlashSpawnOneRequest, &event.FlashRequestPayload{
			X: pos.X, Y: pos.Y, Char: vis.Rune,
		})
	}

	s.world.DestroyEntity(entity)
	s.statCollects.Add(1)
}
