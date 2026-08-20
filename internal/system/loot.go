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

// pityState tracks consecutive misses per loot type for an enemy type
type pityState struct {
	misses [component.LootCount]int
}

type LootSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Pity tracking per enemy type
	pity map[component.SpeciesType]*pityState

	// Telemetry
	statDrops    *atomic.Int64
	statActive   *atomic.Int64
	statCollects *atomic.Int64

	enabled bool
}

func NewLootSystem(world *engine.World) engine.System {
	s := &LootSystem{
		world: world,
	}

	s.statDrops = world.Resources.Status.Ints.Get("loot.drops")
	s.statActive = world.Resources.Status.Ints.Get("loot.active")
	s.statCollects = world.Resources.Status.Ints.Get("loot.collects")

	s.Init()
	return s
}

func (s *LootSystem) Init() {
	s.rng = s.world.Rand(s.Name())
	s.pity = make(map[component.SpeciesType]*pityState)
	s.statDrops.Store(0)
	s.statActive.Store(0)
	s.statCollects.Store(0)

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
		event.EventEnemyKilled,
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
	case event.EventEnemyKilled:
		if payload, ok := ev.Payload.(*event.EnemyKilledPayload); ok {
			s.onEnemyKilled(payload)
		}

	case event.EventLootSpawnRequest:
		if payload, ok := ev.Payload.(*event.LootSpawnRequestPayload); ok {
			s.spawnLootMulti([]component.LootType{payload.Type}, payload.X, payload.Y)
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
		return
	}

	config := s.world.Resources.Config
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	var activeCount int64
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
		cursor, cursorX, cursorY, hasCursor := ClosestCursor(s.world, curX, curY)

		// Collection check
		if hasCursor && vmath.IntAbs(curX-cursorX) <= parameter.LootCollectRadius &&
			vmath.IntAbs(curY-cursorY) <= parameter.LootCollectRadius {
			s.collectLoot(cursor, lootEntity, lootComp.Type)
			continue
		}

		// Movement logic - always process, navigation handles LOS internally
		navComp, hasNav := s.world.Components.Navigation.GetComponent(lootEntity)

		if hasCursor && hasNav && navComp.HasDirectPath {
			// Direct LOS: standard homing
			cursorCenterX, cursorCenterY := vmath.Point{X: cursorX, Y: cursorY}.CenterF()
			physics.ApplyHoming(&kineticComp.Kinetic, cursorCenterX, cursorCenterY, &profile.LootHoming, dtSec)
		} else if hasNav && (navComp.FlowX != 0 || navComp.FlowY != 0) {
			// No LOS but have flow field: follow flow with lookahead
			targetX := kineticComp.PreciseX + navComp.FlowX*parameter.LootFlowLookahead
			targetY := kineticComp.PreciseY + navComp.FlowY*parameter.LootFlowLookahead
			physics.ApplyHoming(&kineticComp.Kinetic, targetX, targetY, &profile.LootHoming, dtSec)
		} else {
			// No nav or no flow: velocity bleed (stuck/lost)
			physics.ApplyLinearDrag(&kineticComp.Kinetic, parameter.LootVelocityBleed, dtSec)
			if vmath.AbsF(kineticComp.VelX) < parameter.LootStopSpeed &&
				vmath.AbsF(kineticComp.VelY) < parameter.LootStopSpeed {
				kineticComp.VelX, kineticComp.VelY = 0, 0
			}
		}

		newGridX, newGridY, _ := physics.IntegrateWithBounce(
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

		if newGridX != curX || newGridY != curY {
			s.world.Positions.SetPosition(lootEntity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		activeCount++
	}
	s.statActive.Store(activeCount)
}

// --- Drop Resolution ---

// onEnemyKilled processes multi-drop loot spawning
func (s *LootSystem) onEnemyKilled(payload *event.EnemyKilledPayload) {
	results := s.rollDropTable(payload.Species)
	if len(results) == 0 {
		return
	}

	// Flatten results into spawn list
	var spawns []component.LootType
	for _, r := range results {
		for range r.Count {
			spawns = append(spawns, r.Loot)
		}
	}

	if len(spawns) == 0 {
		return
	}

	// Spawn with offset pattern
	s.spawnLootMulti(spawns, payload.X, payload.Y)
}

// --- Spawn ---

// spawnLootMulti spawns multiple loot items with scatter pattern and initial burst velocity
func (s *LootSystem) spawnLootMulti(loots []component.LootType, cx, cy int) {
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

		s.spawnLootWithBurst(lootType, spawnX, spawnY, burstDirX, burstDirY)
		s.statDrops.Add(1)
	}
}

// spawnLootWithBurst creates loot entity with initial velocity in burst direction
func (s *LootSystem) spawnLootWithBurst(lootType component.LootType, x, y, burstDirX, burstDirY int) {
	vis, ok := visual.LootVisuals[lootType]
	if !ok {
		return
	}

	entity := s.world.CreateEntity()
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

	// Navigation for wall-aware pathfinding (no GA tracking - loot doesn't emit EnemyCreated)
	s.world.Components.Navigation.SetComponent(entity, component.NavigationComponent{
		Width:         1,
		Height:        1,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
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
// Returns slice of drop results (may be empty)
func (s *LootSystem) rollDropTable(speciesType component.SpeciesType) []DropResult {
	table, ok := component.DropTables[speciesType]
	if !ok || len(table.Tiers) == 0 {
		return nil
	}

	state := s.pity[speciesType]
	if state == nil {
		state = &pityState{}
		s.pity[speciesType] = state
	}

	activeLoot := s.getActiveLootTypes()

	isOwned := func(lt component.LootType) bool {
		if activeLoot[lt] {
			return true
		}
		profile := component.LootProfiles[lt]
		if profile.Reward == nil || profile.Reward.Type != component.RewardWeapon {
			return false
		}

		// Max-charge check, repeats drops until capped
		wt := profile.Reward.WeaponType
		return s.allPlayersCapped(wt)
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

// getActiveLootTypes returns set of loot types currently on map
func (s *LootSystem) getActiveLootTypes() map[component.LootType]bool {
	active := make(map[component.LootType]bool)
	lootEntities := s.world.Components.Loot.Entities()
	for _, entity := range lootEntities {
		lootComp, ok := s.world.Components.Loot.GetPtr(entity)
		if !ok {
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
			s.world.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{
				Entity: cursor,
				Weapon: profile.Reward.WeaponType,
			})

		case component.RewardEnergy:
			s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Entity: cursor,
				Delta:  profile.Reward.Delta,
				Type:   component.EnergyDeltaReward,
			})

		case component.RewardHeat:
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: cursor,
				Delta:  profile.Reward.Delta,
			})
		}
	}

	// Visual feedback
	vis := visual.LootVisuals[lootType]
	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		s.world.PushEvent(event.EventFlashSpawnOneRequest, &event.FlashRequestPayload{
			X: pos.X, Y: pos.Y, Char: vis.Rune,
		})
	}

	s.world.DestroyEntity(entity)
	s.statCollects.Add(1)
}
