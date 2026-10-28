package system

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/navigation"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
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

type LootSystem struct {
	world *engine.World

	// Loot is player-domain, so it never advances the shared stream
	rng *vmath.FastRand

	// costs weight every owner route at the cell aspect
	costs navigation.Costs

	// Pity tracking per species type and roster slot
	pity map[pityKey]*pityState

	loots []core.Entity // the tick's loot, detached: collectLoot destroys mid-iteration

	// Telemetry
	statDrops       *atomic.Int64
	statActive      *atomic.Int64
	statCollects    *atomic.Int64
	statUnreachable *atomic.Int64
	buffers         bufferTelemetry
	motion          bounceTelemetry

	toggle
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
	s.statUnreachable = world.Resources.Status.Ints.Get("loot.unreachable")
	s.buffers = newBufferTelemetry(world.Resources.Status, "loot", "pity")
	s.motion = newBounceTelemetry(world.Resources.Status, "loot")

	return s
}

func (s *LootSystem) Init() {
	s.costs = navigation.CostsFor(s.world.Resources.Config.CellAspect)
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.pity = make(map[pityKey]*pityState)
	s.statDrops.Store(0)
	s.statActive.Store(0)
	s.statCollects.Store(0)
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
		event.EventSpeciesKillConfirmed,
		event.EventLootSpawnRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *LootSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventSpeciesKilled, event.EventSpeciesKillConfirmed:
		// A drop is player-domain and no correction can take it back, so a death
		// this instance is only predicting waits: the ledger raises the confirmed
		// form once an authoritative world proves the species gone.
		if ev.Phase == event.PhasePredicted {
			return
		}
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			s.onSpeciesKilled(payload)
		}

	case event.EventLootSpawnRequest:
		if payload, ok := ev.Payload.(*event.LootSpawnRequestPayload); ok {
			s.onEventDrop(payload)
		}
	}
}

func (s *LootSystem) Update() {
	if !s.enabled {
		return
	}

	s.loots = append(s.loots[:0], s.world.Components.Loot.Entities()...)
	if len(s.loots) == 0 {
		s.statActive.Store(0)
		s.statUnreachable.Store(0)
		return
	}

	config := s.world.Resources.Config
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), parameter.MaxSimulationDeltaSeconds)

	var activeCount int64
	var unreachable int64
	for _, lootEntity := range s.loots {
		lootComp, ok := s.world.Components.Loot.GetPtr(lootEntity)
		if !ok {
			continue
		}

		kineticComp, ok := s.world.Components.Kinetic.GetPtr(lootEntity)
		if !ok {
			continue
		}

		curX, curY := physics.GridPos(&kineticComp.Kinetic)
		// A transition or correction can shrink the shared map under private loot.
		if s.world.Positions.IsOutOfBounds(curX, curY) {
			curX, curY = min(max(curX, 0), config.MapWidth-1), min(max(curY, 0), config.MapHeight-1)
			kineticComp.PreciseX, kineticComp.PreciseY = (vmath.Point{X: curX, Y: curY}).Center()
			kineticComp.VelX, kineticComp.VelY = 0, 0
			s.world.Positions.SetPosition(lootEntity, component.PositionComponent{X: curX, Y: curY})
		}
		// A wall that arrived without displacing the drop — a correction installs
		// shared walls under player-domain loot — claims the cell as a spawn would.
		if s.world.Positions.IsBlocked(curX, curY, component.WallBlockKinetic) {
			if x, y, moved := s.world.PushEntityFromBlocked(lootEntity, component.WallBlockKinetic); moved {
				curX, curY = x, y
			}
		}
		owner := lootComp.Owner
		ownerPos, hasOwner := s.world.Positions.GetPosition(owner)

		if hasOwner && vmath.IntAbs(curX-ownerPos.X) <= parameter.LootCollectRadius &&
			vmath.IntAbs(curY-ownerPos.Y) <= parameter.LootCollectRadius {
			s.collectLoot(owner, lootEntity, lootComp.Type)
			continue
		}

		targetX, targetY, routed := s.homingTarget(lootComp, kineticComp, curX, curY, ownerPos, hasOwner)
		if routed {
			// Cornering brake, as drains, swarms, quasars and eyes apply it:
			// acceleration alone conserves the sideways component of an approach,
			// which leaves a drop circling its own cursor until something else moves it.
			turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
				parameter.NavCorneringBase, 1.0)
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
			if vmath.Abs(kineticComp.VelX) < parameter.LootStopSpeed &&
				vmath.Abs(kineticComp.VelY) < parameter.LootStopSpeed {
				kineticComp.VelX, kineticComp.VelY = 0, 0
			}
		}

		newGridX, newGridY, motion := physics.IntegrateWithBounceStats(
			&kineticComp.Kinetic,
			dtSec,
			0, 0,
			0, config.MapWidth-1,
			0, config.MapHeight-1,
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

// A drop follows its owner directly when visible, otherwise its private field.
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

	ownerCenterX, ownerCenterY := vmath.Point{X: ownerPos.X, Y: ownerPos.Y}.Center()
	if s.world.Positions.HasLineOfSight(curX, curY, ownerPos.X, ownerPos.Y, component.WallBlockKinetic) {
		return ownerCenterX, ownerCenterY, true
	}

	slot, ok := s.world.CursorSlot(lootComp.Owner)
	if !ok {
		return 0, 0, false
	}
	field := s.world.Resources.Pursuit.Field(slot)
	if field == nil {
		return 0, 0, false
	}
	flowX, flowY := s.costs.InterpolatedDirection(field, kineticComp.PreciseX, kineticComp.PreciseY)
	if flowX == 0 && flowY == 0 {
		return 0, 0, false
	}

	lookahead := vmath.Clamp(
		vmath.Magnitude(ownerCenterX-kineticComp.PreciseX, ownerCenterY-kineticComp.PreciseY),
		1.0, parameter.LootFlowLookahead)
	return kineticComp.PreciseX + flowX*lookahead, kineticComp.PreciseY + flowY*lookahead, true
}

// --- Drop Resolution ---

// onEventDrop is a scenario's reward rather than a roll: one drop of the named kind
// for every cursor this instance simulates, as each instance rolls a kill for its own
func (s *LootSystem) onEventDrop(payload *event.LootSpawnRequestPayload) {
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		if cursor != 0 && s.world.SimulatesLocally(cursor) {
			s.spawnLootMulti([]component.LootType{payload.Type}, payload.X, payload.Y, cursor)
		}
	}
}

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

// spawnLootMulti scatters drops in a fixed pattern, each bursting away from the centre
func (s *LootSystem) spawnLootMulti(loots []component.LootType, cx, cy int, owner core.Entity) {
	count := len(loots)
	if count == 0 {
		return
	}

	patternIdx := count
	if patternIdx >= len(spawnOffsets) {
		patternIdx = len(spawnOffsets) - 1
	}
	pattern := spawnOffsets[patternIdx]

	for i, lootType := range loots {
		offset := pattern[i%len(pattern)]
		spawnX, spawnY := cx+offset.dx, cy+offset.dy

		// The burst follows the pattern offset, before validation can move the drop
		burstDirX, burstDirY := offset.dx, offset.dy

		if !s.isValidSpawnPos(spawnX, spawnY) {
			spawnX, spawnY = cx, cy
			if !s.isValidSpawnPos(spawnX, spawnY) {
				if freeX, freeY, found := s.world.Positions.FindFreeFromPattern(
					cx, cy, 1, 1,
					engine.PatternCardinalFirst,
					1, 5, true,
					component.WallBlockKinetic, nil,
				); found {
					spawnX, spawnY = freeX, freeY
					burstDirX, burstDirY = freeX-cx, freeY-cy
				} else {
					continue
				}
			}
		}

		s.spawnLootWithBurst(lootType, spawnX, spawnY, burstDirX, burstDirY, owner)
		s.statDrops.Add(1)
	}
}

// spawnLootWithBurst creates one owned drop moving in its burst direction
func (s *LootSystem) spawnLootWithBurst(lootType component.LootType, x, y, burstDirX, burstDirY int, owner core.Entity) {
	if lootType >= component.LootCount {
		return
	}

	entity := s.world.CreateEntity(core.DomainPlayer)
	preciseX, preciseY := vmath.Point{X: x, Y: y}.Center()

	var velX, velY float64
	if burstDirX != 0 || burstDirY != 0 {
		dirX, dirY := vmath.Normalize2D(float64(burstDirX), float64(burstDirY))
		velX = dirX * parameter.LootBurstSpeed
		velY = dirY * parameter.LootBurstSpeed
	}

	s.world.Components.Loot.SetComponent(entity, component.LootComponent{
		Type:     lootType,
		Owner:    owner,
		LastIntX: x,
		LastIntY: y,
	})

	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
			VelX:     velX,
			VelY:     velY,
		},
	})

	cfg := visual.ShieldConfigFor(component.ShieldTypeLoot, s.world.Resources.Config.CellAspect)
	s.world.Components.Shield.SetComponent(entity, component.ShieldComponent{
		Active:  true,
		Type:    component.ShieldTypeLoot,
		RadiusX: cfg.RadiusX,
		RadiusY: cfg.RadiusY,
		InvRxSq: cfg.InvRxSq,
		InvRySq: cfg.InvRySq,
	})

	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectFromSpecies | component.ProtectFromParticle,
	})
}

func (s *LootSystem) isValidSpawnPos(x, y int) bool {
	config := s.world.Resources.Config
	if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
		return false
	}
	return !s.world.Positions.IsBlocked(x, y, component.WallBlockKinetic)
}

// rollDropTable rolls a species' tiers for one cursor with pity and fallback
// accumulation; a data species rolls its prototype's tiers under a pity of its own
func (s *LootSystem) rollDropTable(speciesType component.SpeciesType, cursor core.Entity, slot uint8) []DropResult {
	if speciesType >= component.SpeciesMax {
		return nil
	}
	tiers := profile.Drops[speciesType]
	if i := int(speciesType) - int(component.SpeciesDataFirst); i >= 0 && i < len(s.world.Resources.Behaviors) {
		tiers = profile.Drops[s.world.Resources.Behaviors[i].Drops]
	}
	if len(tiers) == 0 {
		return nil
	}

	key := pityKey{species: speciesType, slot: slot}
	state := s.pity[key]
	if state == nil {
		state = &pityState{}
		s.pity[key] = state
		s.buffers.Observe(0, len(s.pity))
	}

	activeLoot := s.activeLootTypes(cursor)

	isOwned := func(lt component.LootType) bool {
		if activeLoot[lt] {
			return true
		}
		spec := &component.LootSpecs[lt]
		if spec.Reward != component.RewardWeapon {
			return false
		}

		// Max-charge check, repeats drops until this cursor is capped
		weapons, ok := s.world.Components.Weapon.GetPtr(cursor)
		return ok && weapons.Charges[spec.Weapon] >= component.WeaponSpecs[spec.Weapon].MaxCharges
	}

	var results []DropResult
	fallbackBonus := 0

	for _, tier := range tiers {
		if tier.Unique {
			allOwned := true
			for _, entry := range tier.Entries {
				if !isOwned(entry.Loot) {
					allOwned = false
					break
				}
			}
			if allOwned {
				for _, entry := range tier.Entries {
					fallbackBonus += entry.FallbackCount
				}
				continue
			}
		}

		var buf [component.LootCount]candidate
		candidates := buf[:0]
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

		if totalRate >= 1.0 {
			for i := range candidates {
				candidates[i].rate /= totalRate
			}
		}

		roll := s.rng.Float64()
		var cumulative float64
		var dropped *profile.DropEntry

		for _, c := range candidates {
			cumulative += c.rate
			if roll < cumulative {
				dropped = c.entry
				break
			}
		}

		for _, c := range candidates {
			if dropped != nil && c.entry.Loot == dropped.Loot {
				state.misses[c.entry.Loot] = 0
			} else {
				state.misses[c.entry.Loot]++
			}
		}

		switch {
		case dropped != nil:
			count := max(dropped.Count, 1)
			// Every non-unique tier after a skipped weapon carries its fallback
			if !tier.Unique {
				count += fallbackBonus
			}
			results = append(results, DropResult{Loot: dropped.Loot, Count: count})
		case tier.Unique:
			for _, c := range candidates {
				fallbackBonus += c.entry.FallbackCount
			}
		}
	}

	return results
}

// candidate holds entry with pity-adjusted rate
type candidate struct {
	entry *profile.DropEntry
	rate  float64
}

// activeLootTypes reports the loot types this cursor already has on the map
func (s *LootSystem) activeLootTypes(cursor core.Entity) (active [component.LootCount]bool) {
	for _, entity := range s.world.Components.Loot.Entities() {
		lootComp, ok := s.world.Components.Loot.GetPtr(entity)
		if ok && lootComp.Owner == cursor && lootComp.Type < component.LootCount {
			active[lootComp.Type] = true
		}
	}
	return active
}

// --- Collection ---

func (s *LootSystem) collectLoot(cursor, entity core.Entity, lootType component.LootType) {
	if lootType >= component.LootCount {
		s.world.DestroyEntity(entity)
		return
	}

	switch spec := &component.LootSpecs[lootType]; spec.Reward {
	case component.RewardWeapon:
		s.world.PushLocal(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{
			Entity: cursor,
			Weapon: spec.Weapon,
		})

	case component.RewardEnergy:
		s.world.PushLocal(event.EventEnergyAddRequest, &event.EnergyAddPayload{
			Entity: cursor,
			Delta:  spec.Delta,
			Type:   component.EnergyDeltaReward,
		})

	case component.RewardHeat:
		s.world.PushLocal(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Entity: cursor,
			Delta:  spec.Delta,
		})
	}

	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		s.world.PushLocal(event.EventFlashSpawnOneRequest, &event.FlashRequestPayload{
			X: pos.X, Y: pos.Y, Char: visual.LootVisuals[lootType].Rune,
		})
	}

	s.world.DestroyEntity(entity)
	s.statCollects.Add(1)
}

type lootPityLocal struct {
	Species component.SpeciesType    `json:"species"`
	Slot    uint8                    `json:"slot"`
	Misses  [component.LootCount]int `json:"misses"`
}

func (s *LootSystem) SaveLocal() ([]byte, error) {
	rows := make([]lootPityLocal, 0, len(s.pity))
	for k, p := range s.pity {
		rows = append(rows, lootPityLocal{Species: k.species, Slot: k.slot, Misses: p.misses})
	}
	slices.SortFunc(rows, func(a, b lootPityLocal) int {
		return cmp.Or(cmp.Compare(a.Species, b.Species), cmp.Compare(a.Slot, b.Slot))
	})
	return json.Marshal(rows)
}

func (s *LootSystem) LoadLocal(data []byte) error {
	var rows []lootPityLocal
	if err := json.Unmarshal(data, &rows); err != nil {
		return fmt.Errorf("loot: %w", err)
	}
	clear(s.pity)
	for _, p := range rows {
		s.pity[pityKey{species: p.Species, slot: p.Slot}] = &pityState{misses: p.Misses}
	}
	return nil
}
