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

// ExplosionCenter represents a single explosion for rendering
type ExplosionCenter struct {
	X, Y      int
	Radius    int64               // Q32.32 cells
	Intensity int64               // Q32.32, Scale = 1.0 base
	Age       int64               // Nanoseconds since spawn
	Type      event.ExplosionType // Explosion variant for palette selection
}

// State for renderer access, System writes, renderer reads - no sync needed
// TODO: this couples renderer to system, to be refactored
var (
	ExplosionCenters      []ExplosionCenter                             // Active slice view
	ExplosionDurationNano int64                                         // For decay calculation
	explosionBacking      [parameter.ExplosionCenterCap]ExplosionCenter // Pre-allocated storage
)

// ExplosionSystem handles explosion triggering and glyph-to-dust transformation
type ExplosionSystem struct {
	world *engine.World

	activeCount int   // Number of active centers
	baseRadius  int64 // Default radius Q32.32
	radiusCap   int64 // Maximum radius after merges Q32.32

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
	s.activeCount = 0
	s.baseRadius = parameter.ExplosionFieldRadius
	s.radiusCap = parameter.ExplosionRadiusCapFixed

	// Initialize package-level state
	ExplosionDurationNano = parameter.ExplosionFieldDuration.Nanoseconds()
	ExplosionCenters = explosionBacking[:0]

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
	if !s.enabled || s.activeCount == 0 {
		return
	}

	dtNano := s.world.Resources.Time.DeltaTime.Nanoseconds()

	write := 0
	for i := 0; i < s.activeCount; i++ {
		explosionBacking[i].Age += dtNano
		if explosionBacking[i].Age < ExplosionDurationNano {
			if write != i {
				explosionBacking[write] = explosionBacking[i]
			}
			write++
		}
	}
	s.activeCount = write
	ExplosionCenters = explosionBacking[:s.activeCount]
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
	centerX := vmath.FromInt(x)
	centerY := vmath.FromInt(y)

	// Merge check - only merge same type
	for i := 0; i < s.activeCount; i++ {
		c := &explosionBacking[i]
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
	if s.activeCount < parameter.ExplosionCenterCap {
		idx = s.activeCount
		s.activeCount++
	} else {
		// Overflow: overwrite oldest
		idx = 0
		maxAge := explosionBacking[0].Age
		for i := 1; i < parameter.ExplosionCenterCap; i++ {
			if explosionBacking[i].Age > maxAge {
				maxAge = explosionBacking[i].Age
				idx = i
			}
		}
	}

	explosionBacking[idx] = ExplosionCenter{
		X:         x,
		Y:         y,
		Radius:    radius,
		Intensity: vmath.Scale,
		Age:       0,
		Type:      explosionType,
	}

	// Update exported slice
	ExplosionCenters = explosionBacking[:s.activeCount]

	// Only dust explosions transform glyphs
	if explosionType == event.ExplosionTypeDust {
		s.transformGlyphs(x, y, radius)
	}

	s.statTriggered.Add(1)
}

// TODO: this conversion must be done in dust system, doing it here results in no telemetry and duplicate logic
func (s *ExplosionSystem) transformGlyphs(centerX, centerY int, radius int64) {
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Player.Entity

	// Radius is horizontal cells; Vertical is half that to maintain aspect ratio
	radiusCells := vmath.ToInt(radius)
	radiusCellsY := radiusCells / 2

	minX := centerX - radiusCells
	maxX := centerX + radiusCells
	minY := centerY - radiusCellsY
	maxY := centerY + radiusCellsY

	if minX < 0 {
		minX = 0
	}
	if maxX >= config.GameWidth {
		maxX = config.GameWidth - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= config.GameHeight {
		maxY = config.GameHeight - 1
	}

	radiusSq := vmath.Mul(radius, radius)

	// Clear reuse buffers
	s.entityBuf = s.entityBuf[:0]
	s.dustEntryBuf = s.dustEntryBuf[:0]

	// Track combat entities for batched event emission
	var hitDrains []core.Entity
	hitComposites := make(map[core.Entity][]core.Entity) // header -> hit members

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
				// Drain - collect for batched combat event
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

				// Glyph - transform to dust
				glyphComp, ok := s.world.Components.Glyph.GetComponent(entity)
				if !ok {
					continue
				}

				if s.world.Components.Death.HasEntity(entity) {
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

	// Emit combat events for drains (individual targets)
	for _, drainEntity := range hitDrains {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackExplosion,
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
			AttackType:   component.CombatAttackExplosion,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: headerEntity,
			HitEntities:  hitMembers,
			OriginX:      centerX,
			OriginY:      centerY,
		})
	}

	// Glyph death and dust spawn
	if len(s.entityBuf) == 0 {
		return
	}

	// Use buffered entities for death batch
	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, s.entityBuf)

	// Use buffered entries for batch dust spawn
	dustBatch := event.AcquireDustSpawnBatch()
	dustBatch.Entries = append(dustBatch.Entries, s.dustEntryBuf...)

	s.world.PushEvent(event.EventDustSpawnBatchRequest, dustBatch)

	s.statConverted.Add(int64(len(s.entityBuf)))
}