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

	case event.EventWeaponAddRequest:
		if payload, ok := ev.Payload.(*event.WeaponAddRequestPayload); ok {
			s.addToBlacklist(payload.Weapon)
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

	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	cursorCenterX, cursorCenterY := vmath.CenteredFromGrid(cursorPos.X, cursorPos.Y)
	var activeCount int64

	for _, entity := range lootEntities {
		loot, ok := s.world.Components.Loot.GetComponent(entity)
		if !ok {
			continue
		}

		gridX := vmath.ToInt(loot.PreciseX)
		gridY := vmath.ToInt(loot.PreciseY)

		// Collection check: Chebyshev distance <= 1
		dx := gridX - cursorPos.X
		dy := gridY - cursorPos.Y
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		dist := dx
		if dy > dist {
			dist = dy
		}

		if dist <= parameter.LootCollectRadius {
			s.collectLoot(entity, &loot)
			continue
		}

		// LOS check for homing activation
		if !loot.Homing && s.world.Positions.HasLineOfSight(gridX, gridY, cursorPos.X, cursorPos.Y, component.WallBlockKinetic) {
			loot.Homing = true
		}

		// Homing integration
		if loot.Homing {
			dirX := cursorCenterX - loot.PreciseX
			dirY := cursorCenterY - loot.PreciseY

			if dirX != 0 || dirY != 0 {
				nX, nY := vmath.Normalize2D(dirX, dirY)
				accel := vmath.FromFloat(parameter.LootHomingAccel)

				loot.VelX += vmath.Mul(vmath.Mul(nX, accel), dtFixed)
				loot.VelY += vmath.Mul(vmath.Mul(nY, accel), dtFixed)

				// Clamp speed
				speed := vmath.DistanceApprox(loot.VelX, loot.VelY)
				maxSpd := vmath.FromFloat(parameter.LootHomingMaxSpeed)
				if speed > maxSpd && speed > 0 {
					scale := vmath.Div(maxSpd, speed)
					loot.VelX = vmath.Mul(loot.VelX, scale)
					loot.VelY = vmath.Mul(loot.VelY, scale)
				}
			}

			loot.PreciseX += vmath.Mul(loot.VelX, dtFixed)
			loot.PreciseY += vmath.Mul(loot.VelY, dtFixed)

			// Clamp to game bounds
			config := s.world.Resources.Config
			loot.PreciseX = max(0, min(loot.PreciseX, vmath.FromInt(config.GameWidth-1)))
			loot.PreciseY = max(0, min(loot.PreciseY, vmath.FromInt(config.GameHeight-1)))
		}

		// Grid sync
		newX := vmath.ToInt(loot.PreciseX)
		newY := vmath.ToInt(loot.PreciseY)
		if newX != loot.LastIntX || newY != loot.LastIntY {
			loot.LastIntX = newX
			loot.LastIntY = newY
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: newX, Y: newY})
		}

		s.world.Components.Loot.SetComponent(entity, loot)
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

	// Build active rates (skip blacklisted)
	type candidate struct {
		index int
		rate  float64
	}
	var candidates []candidate
	var totalRate float64

	for i := range state.entries {
		e := &state.entries[i]
		if s.blacklist[e.Type] {
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

	// Update miss counters
	for i := range state.entries {
		if i == droppedIndex {
			state.entries[i].Misses = 0
		} else if !s.blacklist[state.entries[i].Type] {
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

	s.world.Components.Loot.SetComponent(entity, component.LootComponent{
		Type:     lootType,
		Rune:     vis.Rune,
		PreciseX: preciseX,
		PreciseY: preciseY,
		LastIntX: x,
		LastIntY: y,
	})

	// Shield component for visual halo
	cfg := visual.LootShieldConfig
	s.world.Components.Shield.SetComponent(entity, component.ShieldComponent{
		Active:        true,
		Color:         cfg.Color,
		Palette256:    cfg.Palette256,
		GlowColor:     vis.GlowColor, // Use type-specific glow
		GlowIntensity: cfg.GlowIntensity,
		GlowPeriod:    cfg.GlowPeriod,
		MaxOpacity:    cfg.MaxOpacity,
		RadiusX:       cfg.RadiusX,
		RadiusY:       cfg.RadiusY,
		InvRxSq:       cfg.InvRxSq,
		InvRySq:       cfg.InvRySq,
	})

	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// Sigil for center rune visualization
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

// --- Blacklist ---

func (s *LootSystem) addToBlacklist(weapon component.WeaponType) {
	// Map weapon type back to loot type
	switch weapon {
	case component.WeaponLauncher:
		s.blacklist[component.LootLauncher] = true
	}
}

// RemoveFromBlacklist restores a loot type to the table (called on weapon loss, future)
func (s *LootSystem) RemoveFromBlacklist(lootType component.LootType) {
	delete(s.blacklist, lootType)
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