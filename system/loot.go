package system

import (
	"math/rand"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// pityState tracks consecutive misses per loot type for an enemy type
type pityState struct {
	misses [component.LootCount]int
}

type LootSystem struct {
	world *engine.World

	// Pity tracking per enemy type
	pity map[component.CombatEntityType]*pityState

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
	s.pity = make(map[component.CombatEntityType]*pityState)
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
		event.EventGameReset,
	}
}

func (s *LootSystem) HandleEvent(ev event.GameEvent) {
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
			s.spawnLoot(payload.Type, payload.X, payload.Y)
			s.statDrops.Add(1)
		}
	}
}

func (s *LootSystem) Update() {
	if !s.enabled {
		return
	}

	lootEntities := s.world.Components.Loot.GetAllEntities()
	if len(lootEntities) == 0 {
		s.statActive.Store(0)
		return
	}

	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	config := s.world.Resources.Config
	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	cursorCenterX, cursorCenterY := vmath.CenteredFromGrid(cursorPos.X, cursorPos.Y)

	var activeCount int64
	for _, lootEntity := range lootEntities {
		lootComp, ok := s.world.Components.Loot.GetComponent(lootEntity)
		if !ok {
			continue
		}

		kineticComp, ok := s.world.Components.Kinetic.GetComponent(lootEntity)
		if !ok {
			continue
		}

		curX, curY := vmath.ToInt(kineticComp.PreciseX), vmath.ToInt(kineticComp.PreciseY)

		// Collection check
		if vmath.IntAbs(curX-cursorPos.X) <= parameter.LootCollectRadius &&
			vmath.IntAbs(curY-cursorPos.Y) <= parameter.LootCollectRadius {
			s.collectLoot(lootEntity, lootComp.Type)
			continue
		}

		// Movement logic
		if !lootComp.Homing {
			if s.world.Positions.HasLineOfSight(curX, curY, cursorPos.X, cursorPos.Y, component.WallBlockKinetic) {
				lootComp.Homing = true
				s.world.Components.Loot.SetComponent(lootEntity, lootComp)
			}
		}

		if lootComp.Homing {
			if s.world.Positions.HasLineOfSight(curX, curY, cursorPos.X, cursorPos.Y, component.WallBlockKinetic) {
				physics.ApplyHoming(&kineticComp.Kinetic, cursorCenterX, cursorCenterY, &physics.LootHoming, dtFixed)
			} else {
				bleedFactor := vmath.FromFloat(6.0)
				kineticComp.VelX -= vmath.Mul(vmath.Mul(kineticComp.VelX, bleedFactor), dtFixed)
				kineticComp.VelY -= vmath.Mul(vmath.Mul(kineticComp.VelY, bleedFactor), dtFixed)
				if vmath.Abs(kineticComp.VelX) < vmath.FromFloat(0.1) && vmath.Abs(kineticComp.VelY) < vmath.FromFloat(0.1) {
					kineticComp.VelX, kineticComp.VelY = 0, 0
				}
			}

			newGridX, newGridY, _ := physics.IntegrateWithBounce(
				&kineticComp.Kinetic,
				dtFixed,
				0, 0,
				0, config.GameWidth,
				0, config.GameHeight,
				vmath.FromFloat(0.4),
				func(tx, ty int) bool {
					return s.world.Positions.IsBlocked(tx, ty, component.WallBlockKinetic)
				},
			)

			s.world.Components.Kinetic.SetComponent(lootEntity, kineticComp)
			if newGridX != curX || newGridY != curY {
				s.world.Positions.SetPosition(lootEntity, component.PositionComponent{X: newGridX, Y: newGridY})
			}
		}
		activeCount++
	}
	s.statActive.Store(activeCount)
}

// --- Drop Resolution ---

func (s *LootSystem) onEnemyKilled(payload *event.EnemyKilledPayload) {
	lootType, dropped := s.rollDropTable(payload.EnemyType)
	if !dropped {
		return
	}

	s.spawnLoot(lootType, payload.X, payload.Y)
	s.statDrops.Add(1)
}

func (s *LootSystem) rollDropTable(enemyType component.CombatEntityType) (component.LootType, bool) {
	if int(enemyType) >= len(component.EnemyDropTables) {
		return 0, false
	}

	dropTable := component.EnemyDropTables[enemyType]
	if len(dropTable) == 0 {
		return 0, false
	}

	// Get or create pity state for this enemy type
	state := s.pity[enemyType]
	if state == nil {
		state = &pityState{}
		s.pity[enemyType] = state
	}

	// Dynamic blacklist: skip loot types player already has
	cursorEntity := s.world.Resources.Player.Entity
	weaponComp, hasWeapons := s.world.Components.Weapon.GetComponent(cursorEntity)

	isBlacklisted := func(lt component.LootType) bool {
		if !hasWeapons {
			return false
		}
		profile := component.LootProfiles[lt]
		if profile.Reward == nil || profile.Reward.Type != component.RewardWeapon {
			return false
		}
		return weaponComp.Active[profile.Reward.WeaponType]
	}

	// Build candidates with pity-adjusted rates
	type candidate struct {
		entry *component.DropEntry
		rate  float64
	}
	var candidates []candidate
	var totalRate float64

	for i := range dropTable {
		entry := &dropTable[i]
		if isBlacklisted(entry.Loot) {
			continue
		}
		rate := entry.BaseRate * float64(1+state.misses[entry.Loot])
		candidates = append(candidates, candidate{entry, rate})
		totalRate += rate
	}

	if len(candidates) == 0 {
		return 0, false
	}

	// Normalize if exceeds 1.0
	if totalRate >= 1.0 {
		for i := range candidates {
			candidates[i].rate /= totalRate
		}
		totalRate = 1.0
	}

	// Roll
	roll := rand.Float64()
	var cumulative float64
	var dropped *component.DropEntry

	for _, c := range candidates {
		cumulative += c.rate
		if roll < cumulative {
			dropped = c.entry
			break
		}
	}

	// Update pity counters
	for i := range dropTable {
		entry := &dropTable[i]
		if isBlacklisted(entry.Loot) {
			continue
		}
		if dropped != nil && entry.Loot == dropped.Loot {
			state.misses[entry.Loot] = 0
		} else {
			state.misses[entry.Loot]++
		}
	}

	if dropped != nil {
		return dropped.Loot, true
	}
	return 0, false
}

// --- Spawn ---

func (s *LootSystem) spawnLoot(lootType component.LootType, x, y int) {
	vis, ok := component.LootVisuals[lootType]
	if !ok {
		return
	}
	entity := s.world.CreateEntity()
	preciseX, preciseY := vmath.CenteredFromGrid(x, y)

	// Loot component
	s.world.Components.Loot.SetComponent(entity, component.LootComponent{
		Type: lootType,
	})

	// Kinetic
	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	})

	// Shield (uses shared config, loot-specific glow color)
	cfg := visual.LootShieldConfig
	s.world.Components.Shield.SetComponent(entity, component.ShieldComponent{
		Active:        true,
		Color:         cfg.Color,
		Palette256:    cfg.Palette256,
		GlowColor:     vis.GlowColor,
		GlowIntensity: cfg.GlowIntensity,
		GlowPeriod:    cfg.GlowPeriod,
		MaxOpacity:    cfg.MaxOpacity,
		RadiusX:       cfg.RadiusX,
		RadiusY:       cfg.RadiusY,
		InvRxSq:       cfg.InvRxSq,
		InvRySq:       cfg.InvRySq,
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
		Mask: component.ProtectFromDrain | component.ProtectFromDecay | component.ProtectFromDelete,
	})
}

// --- Collection ---

func (s *LootSystem) collectLoot(entity core.Entity, lootType component.LootType) {
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
				Weapon: profile.Reward.WeaponType,
			})

		case component.RewardEnergy:
			s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Delta: profile.Reward.Delta,
				Type:  event.EnergyDeltaReward,
			})

		case component.RewardHeat:
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Delta: profile.Reward.Delta,
			})
		}
	}

	// Visual feedback
	vis := component.LootVisuals[lootType]
	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		s.world.PushEvent(event.EventFlashSpawnOneRequest, &event.FlashRequestPayload{
			X: pos.X, Y: pos.Y, Char: vis.Rune,
		})
	}

	s.world.DestroyEntity(entity)
	s.statCollects.Add(1)
}