package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ExplosionSystem handles explosion triggering and glyph-to-dust transformation
type ExplosionSystem struct {
	world *engine.World

	baseRadius int64 // Default radius Q32.32
	radiusCap  int64 // Maximum radius after merges Q32.32

	// Reusable buffers to avoid allocation in hot path
	entityBuf    []core.Entity
	dustEntryBuf []event.DustSpawnEntry

	// Random source for orbit radius and direction
	rng *vmath.FastRand

	statTriggered *atomic.Int64
	statConverted *atomic.Int64
	statMerged    *atomic.Int64

	enabled bool
}

func NewExplosionSystem(world *engine.World) engine.System {
	s := &ExplosionSystem{
		world: world,
	}

	s.statTriggered = world.Resources.Status.Ints.Get("explosion.triggered")
	s.statConverted = world.Resources.Status.Ints.Get("explosion.converted")
	s.statMerged = world.Resources.Status.Ints.Get("explosion.merged")

	s.Init()
	return s
}

func (s *ExplosionSystem) Init() {
	s.baseRadius = parameter.ExplosionFieldRadius
	s.radiusCap = parameter.ExplosionRadiusCapFixed

	// Reset buffers
	s.entityBuf = make([]core.Entity, 0, 256)
	s.dustEntryBuf = make([]event.DustSpawnEntry, 0, 256)

	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))

	s.statTriggered.Store(0)
	s.statConverted.Store(0)
	s.statMerged.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *ExplosionSystem) Name() string {
	return "explosion"
}

func (s *ExplosionSystem) Priority() int {
	return parameter.PriorityExplosion
}

func (s *ExplosionSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFireSpecialRequest,
		event.EventExplosionRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *ExplosionSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventFireSpecialRequest:
		s.fireFromDust()

	case event.EventExplosionRequest:
		if p, ok := ev.Payload.(*event.ExplosionRequestPayload); ok {
			radius := p.Radius
			if radius == 0 {
				radius = s.baseRadius
			}
			s.addCenter(p.X, p.Y, radius, p.Type)
		}
	}
}

func (s *ExplosionSystem) Update() {
	if !s.enabled {
		return
	}

	transRes := s.world.Resources.Transient
	if transRes.ExplosionCount == 0 {
		return
	}

	dtNano := s.world.Resources.Time.DeltaTime.Nanoseconds()

	write := 0
	for i := 0; i < transRes.ExplosionCount; i++ {
		transRes.ExplosionBacking[i].Age += dtNano
		if transRes.ExplosionBacking[i].Age < transRes.ExplosionDurNano {
			if write != i {
				transRes.ExplosionBacking[write] = transRes.ExplosionBacking[i]
			}
			write++
		}
	}
	transRes.ExplosionCount = write
}

func (s *ExplosionSystem) fireFromDust() {
	dustEntities := s.world.Components.Dust.GetAllEntities()
	if len(dustEntities) == 0 {
		return
	}

	type pos struct{ x, y int }
	positions := make(map[pos]bool, len(dustEntities))

	for _, e := range dustEntities {
		if p, ok := s.world.Positions.GetPosition(e); ok {
			positions[pos{p.X, p.Y}] = true
		}
	}

	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, dustEntities)

	for p := range positions {
		s.addCenter(p.x, p.y, s.baseRadius, event.ExplosionTypeDust)
	}
}

func (s *ExplosionSystem) addCenter(x, y int, radius int64, explosionType event.ExplosionType) {
	transRes := s.world.Resources.Transient
	centerX := vmath.FromInt(x)
	centerY := vmath.FromInt(y)

	// Merge check - only merge same type
	for i := 0; i < transRes.ExplosionCount; i++ {
		c := &transRes.ExplosionBacking[i]
		if c.Type != explosionType {
			continue
		}

		dx := centerX - vmath.FromInt(c.X)
		dy := centerY - vmath.FromInt(c.Y)
		distSq := vmath.Mul(dx, dx) + vmath.Mul(dy, dy)

		if distSq <= parameter.ExplosionMergeThresholdSq {
			c.Age = 0
			c.Intensity += parameter.ExplosionIntensityBoost
			if c.Intensity > parameter.ExplosionIntensityCap {
				c.Intensity = parameter.ExplosionIntensityCap
			}
			newRadius := c.Radius
			if radius > newRadius {
				newRadius = radius
			}
			newRadius += parameter.ExplosionRadiusBoost
			if newRadius > s.radiusCap {
				newRadius = s.radiusCap
			}
			c.Radius = newRadius

			s.statMerged.Add(1)
			return
		}
	}

	// No merge - add new center
	var idx int
	if transRes.ExplosionCount < parameter.ExplosionCenterCap {
		idx = transRes.ExplosionCount
		transRes.ExplosionCount++
	} else {
		// Overflow: overwrite oldest
		idx = 0
		maxAge := transRes.ExplosionBacking[0].Age
		for i := 1; i < parameter.ExplosionCenterCap; i++ {
			if transRes.ExplosionBacking[i].Age > maxAge {
				maxAge = transRes.ExplosionBacking[i].Age
				idx = i
			}
		}
	}

	transRes.ExplosionBacking[idx] = engine.ExplosionCenter{
		X:         x,
		Y:         y,
		Radius:    radius,
		Intensity: vmath.Scale,
		Age:       0,
		Type:      explosionType,
	}

	// Process area effects (combat + optional glyph conversion)
	s.processExplosionArea(x, y, radius, explosionType)

	s.statTriggered.Add(1)
}

// processExplosionArea handles entity collection and event emission for explosion effects
// Single-pass sweep: collects combat entities (always), converts glyphs (dust only)
func (s *ExplosionSystem) processExplosionArea(centerX, centerY int, radius int64, explosionType event.ExplosionType) {
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Player.Entity

	// Determine behavior based on explosion type
	var attackType component.CombatAttackType
	convertGlyphs := false

	switch explosionType {
	case event.ExplosionTypeDust:
		attackType = component.CombatAttackExplosion
		convertGlyphs = true
	case event.ExplosionTypeMissile:
		attackType = component.CombatAttackMissile
	default:
		return
	}

	// Calculate bounds with aspect correction
	radiusCells := vmath.ToInt(radius)
	radiusCellsY := radiusCells / 2

	minX := max(0, centerX-radiusCells)
	maxX := min(config.MapWidth-1, centerX+radiusCells)
	minY := max(0, centerY-radiusCellsY)
	maxY := min(config.MapHeight-1, centerY+radiusCellsY)

	radiusSq := vmath.Mul(radius, radius)

	// Clear reuse buffers
	s.entityBuf = s.entityBuf[:0]
	s.dustEntryBuf = s.dustEntryBuf[:0]

	// Combat entity collectors
	var hitDrains []core.Entity
	hitComposites := make(map[core.Entity][]core.Entity)

	// Single-pass area sweep
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := vmath.FromInt(x - centerX)
			dy := vmath.FromInt(y - centerY)
			dyCirc := vmath.ScaleToCircular(dy)
			distSq := vmath.CircleDistSq(dx, dyCirc)

			if distSq > radiusSq {
				continue
			}

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, entity := range entities {
				// Drain - collect for combat
				if s.world.Components.Drain.HasEntity(entity) {
					hitDrains = append(hitDrains, entity)
					continue
				}

				// Composite member - collect by header
				if memberComp, ok := s.world.Components.Member.GetComponent(entity); ok {
					headerEntity := memberComp.HeaderEntity
					headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
					if !ok {
						continue
					}

					switch headerComp.Behavior {
					case component.BehaviorQuasar, component.BehaviorSwarm, component.BehaviorStorm:
						hitComposites[headerEntity] = append(hitComposites[headerEntity], entity)
					}
					continue
				}

				// Glyph - convert to dust (dust explosion only)
				if convertGlyphs {
					glyphComp, ok := s.world.Components.Glyph.GetComponent(entity)
					if !ok || s.world.Components.Death.HasEntity(entity) {
						continue
					}

					s.world.Components.Death.SetComponent(entity, component.DeathComponent{})
					s.entityBuf = append(s.entityBuf, entity)
					s.dustEntryBuf = append(s.dustEntryBuf, event.DustSpawnEntry{
						X:     x,
						Y:     y,
						Char:  glyphComp.Rune,
						Level: glyphComp.Level,
					})
				}
			}
		}
	}

	// Emit combat events for drains
	for _, drainEntity := range hitDrains {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attackType,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: drainEntity,
			HitEntities:  []core.Entity{drainEntity},
			OriginX:      centerX,
			OriginY:      centerY,
		})
	}

	// Emit combat events for composites (batched by header)
	for headerEntity, hitMembers := range hitComposites {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attackType,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: headerEntity,
			HitEntities:  hitMembers,
			OriginX:      centerX,
			OriginY:      centerY,
		})
	}

	// Glyph death and dust spawn (dust only)
	if convertGlyphs && len(s.entityBuf) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, s.entityBuf)

		event.EmitBatch(s.world.Resources.Event.Queue, event.DustBatchPool, event.EventDustSpawnBatchRequest, s.dustEntryBuf)

		s.statConverted.Add(int64(len(s.entityBuf)))
	}
}