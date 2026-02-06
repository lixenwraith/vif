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
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

type lootEntry struct {
	Type     component.LootType
	BaseRate float64
	Misses   int // Consecutive misses for this item
}

type lootTableState struct {
	entries []lootEntry
}

// TODO: change this design
// lootVisualDef maps loot type to visual properties
type lootVisualDef struct {
	Rune       rune
	InnerColor terminal.RGB
	GlowColor  terminal.RGB
}

var lootVisuals = map[component.LootType]lootVisualDef{
	component.LootLauncher: {'M', visual.RgbOrbLauncher, terminal.RGB{R: 180, G: 100}},
	component.LootRod:      {'L', visual.RgbOrbRod, terminal.RGB{G: 180, B: 180}},
}

type LootSystem struct {
	world *engine.World

	tables    map[component.EnemyType]*lootTableState
	blacklist map[component.LootType]bool

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
	s.blacklist = make(map[component.LootType]bool)
	s.initTables()
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
		event.EventWeaponAddRequest,
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
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventEnemyKilled:
		if payload, ok := ev.Payload.(*event.EnemyKilledPayload); ok {
			s.onEnemyKilled(payload)
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

		// Current grid pos for collection and LOS checks
		curX, curY := vmath.ToInt(kineticComp.PreciseX), vmath.ToInt(kineticComp.PreciseY)

		// 1. Collection check (Grid-based Chebyshev)
		if vmath.IntAbs(curX-cursorPos.X) <= parameter.LootCollectRadius &&
			vmath.IntAbs(curY-cursorPos.Y) <= parameter.LootCollectRadius {
			s.collectLoot(lootEntity, &lootComp)
			continue
		}

		// 2. Movement Logic
		if !lootComp.Homing {
			// Trigger homing if LOS is established
			if s.world.Positions.HasLineOfSight(curX, curY, cursorPos.X, cursorPos.Y, component.WallBlockKinetic) {
				lootComp.Homing = true
				s.world.Components.Loot.SetComponent(lootEntity, lootComp)
			}
		}

		if lootComp.Homing {
			// A. Update Velocity
			if s.world.Positions.HasLineOfSight(curX, curY, cursorPos.X, cursorPos.Y, component.WallBlockKinetic) {
				// Normal pursuit
				physics.ApplyHoming(&kineticComp.Kinetic, cursorCenterX, cursorCenterY, &physics.LootHoming, dtFixed)
			} else {
				// Glide to a halt if LOS lost (friction only)
				bleedFactor := vmath.FromFloat(6.0)
				kineticComp.VelX -= vmath.Mul(vmath.Mul(kineticComp.VelX, bleedFactor), dtFixed)
				kineticComp.VelY -= vmath.Mul(vmath.Mul(kineticComp.VelY, bleedFactor), dtFixed)
				if vmath.Abs(kineticComp.VelX) < vmath.FromFloat(0.1) && vmath.Abs(kineticComp.VelY) < vmath.FromFloat(0.1) {
					kineticComp.VelX, kineticComp.VelY = 0, 0
				}
			}

			// B. Integrate with Bounce & Wall Detection
			// 1x1 entity centered at PreciseX/Y means header offset is 0.
			newGridX, newGridY, _ := physics.IntegrateWithBounce(
				&kineticComp.Kinetic,
				dtFixed,
				0, 0, // No offset from center to hitbox top-left
				0, config.GameWidth,
				0, config.GameHeight,
				vmath.FromFloat(0.4), // Lose 60% velocity on bounce
				func(tx, ty int) bool {
					return s.world.Positions.IsBlocked(tx, ty, component.WallBlockKinetic)
				},
			)

			// C. Sync Components
			s.world.Components.Kinetic.SetComponent(lootEntity, kineticComp)
			if newGridX != curX || newGridY != curY {
				s.world.Positions.SetPosition(lootEntity, component.PositionComponent{X: newGridX, Y: newGridY})
			}
		}
		activeCount++
	}
	s.statActive.Store(activeCount)
}

// --- Kill / Roll ---

func (s *LootSystem) onEnemyKilled(payload *event.EnemyKilledPayload) {
	lootType, dropped := s.rollTable(payload.EnemyType)
	if !dropped {
		return
	}

	s.spawnLoot(lootType, payload.X, payload.Y)
	s.statDrops.Add(1)
}

func (s *LootSystem) rollTable(enemyType component.EnemyType) (component.LootType, bool) {
	state, ok := s.tables[enemyType]
	if !ok {
		return 0, false
	}

	// Dynamic Blacklist Check: Query Weapon Component
	cursorEntity := s.world.Resources.Player.Entity
	weaponComp, hasWeapons := s.world.Components.Weapon.GetComponent(cursorEntity)

	isBlacklisted := func(lt component.LootType) bool {
		if !hasWeapons {
			return false
		}
		// Map loot type to weapon type and check active status
		switch lt {
		case component.LootRod:
			return weaponComp.Active[component.WeaponRod]
		case component.LootLauncher:
			return weaponComp.Active[component.WeaponLauncher]
		}
		return false
	}

	// Build active candidates
	type candidate struct {
		index int
		rate  float64
	}
	var candidates []candidate
	var totalRate float64

	for i := range state.entries {
		e := &state.entries[i]
		if isBlacklisted(e.Type) {
			continue
		}
		rate := e.BaseRate * float64(1+e.Misses)
		candidates = append(candidates, candidate{i, rate})
		totalRate += rate
	}

	if len(candidates) == 0 {
		return 0, false
	}

	// Normalize if total >= 1.0
	if totalRate >= 1.0 {
		for j := range candidates {
			candidates[j].rate /= totalRate
		}
		totalRate = 1.0
	}

	// Roll
	roll := rand.Float64()
	var cumulative float64
	droppedIndex := -1

	for _, c := range candidates {
		cumulative += c.rate
		if roll < cumulative {
			droppedIndex = c.index
			break
		}
	}

	// Update miss counters (skip blacklisted)
	for i := range state.entries {
		if isBlacklisted(state.entries[i].Type) {
			continue
		}
		if i == droppedIndex {
			state.entries[i].Misses = 0
		} else {
			state.entries[i].Misses++
		}
	}

	if droppedIndex >= 0 {
		return state.entries[droppedIndex].Type, true
	}
	return 0, false
}

// --- Spawn ---

func (s *LootSystem) spawnLoot(lootType component.LootType, x, y int) {
	vis, ok := lootVisuals[lootType]
	if !ok {
		return
	}

	entity := s.world.CreateEntity()
	preciseX, preciseY := vmath.CenteredFromGrid(x, y)

	// 1. Metadata
	s.world.Components.Loot.SetComponent(entity, component.LootComponent{
		Type: lootType,
		Rune: vis.Rune,
	})

	// 2. Kinetic (Initialized with zero velocity)
	s.world.Components.Kinetic.SetComponent(entity, component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	})

	// 3. Visuals & Spatial Grid
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

	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  vis.Rune,
		Color: vis.InnerColor,
	})

	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectFromDrain | component.ProtectFromDecay | component.ProtectFromDelete,
	})
}

// --- Collection ---

func (s *LootSystem) collectLoot(entity core.Entity, loot *component.LootComponent) {
	weaponType := lootToWeapon(loot.Type)

	// Grant weapon
	s.world.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{
		Weapon: weaponType,
	})

	// Flash at loot position
	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		s.world.PushEvent(event.EventFlashSpawnOneRequest, &event.FlashRequestPayload{
			X: pos.X, Y: pos.Y, Char: loot.Rune,
		})
	}

	s.world.DestroyEntity(entity)
	s.statCollects.Add(1)
}

// lootToWeapon maps loot type to weapon type for acquisition
// Moved from component to system to maintain pure ECS data-only components
func lootToWeapon(lt component.LootType) component.WeaponType {
	switch lt {
	case component.LootLauncher:
		return component.WeaponLauncher
	case component.LootRod:
		return component.WeaponRod
	default:
		return component.WeaponLauncher
	}
}

// --- Table Init ---

func (s *LootSystem) initTables() {
	s.tables = map[component.EnemyType]*lootTableState{
		component.EnemySwarm: {
			entries: []lootEntry{
				{Type: component.LootLauncher, BaseRate: parameter.LootDropRateLauncher},
			},
		},
		component.EnemyQuasar: {
			entries: []lootEntry{
				{Type: component.LootRod, BaseRate: parameter.LootDropRateRod},
			},
		},
	}
}