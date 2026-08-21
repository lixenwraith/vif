package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// hitComposite groups an explosion's member hits under their header, in the
// order headers were first encountered
type hitComposite struct {
	header  core.Entity
	members []core.Entity
}

// ExplosionSystem handles explosion triggering and glyph-to-dust transformation
type ExplosionSystem struct {
	world *engine.World

	baseRadius float64 // Default radius in cells
	radiusCap  float64 // Maximum radius after merges (cells)

	// Reusable buffers to avoid allocation in hot path
	entityBuf    []core.Entity
	dustEntryBuf []event.DustSpawnEntry
	centerBuf    []vmath.Point

	drainBuf     []core.Entity
	compositeBuf []hitComposite
	compositeIdx map[core.Entity]int
	seenCells    map[uint64]bool

	statTriggered     *atomic.Int64
	statConverted     *atomic.Int64
	statMerged        *atomic.Int64
	statCursorRejects *atomic.Int64
	statDisabled      *atomic.Int64
	buffers           bufferTelemetry

	enabled bool
}

func NewExplosionSystem(world *engine.World) engine.System {
	s := &ExplosionSystem{
		world: world,
	}

	s.statTriggered = world.Resources.Status.Ints.Get("explosion.triggered")
	s.statConverted = world.Resources.Status.Ints.Get("explosion.converted")
	s.statMerged = world.Resources.Status.Ints.Get("explosion.merged")
	s.statCursorRejects = world.Resources.Status.Ints.Get("explosion.cursor_rejects")
	s.statDisabled = world.Resources.Status.Ints.Get("explosion.disabled_rejects")
	s.buffers = newBufferTelemetry(world.Resources.Status, "explosion", "entities", "dust_entries", "centers", "drains", "composites", "composite_index", "seen_cells")

	s.Init()
	return s
}

func (s *ExplosionSystem) Init() {
	s.baseRadius = parameter.ExplosionFieldRadius
	s.radiusCap = parameter.ExplosionRadiusCapFixed

	// Reset buffers
	s.entityBuf = make([]core.Entity, 0, 256)
	s.dustEntryBuf = make([]event.DustSpawnEntry, 0, 256)
	s.centerBuf = make([]vmath.Point, 0, 64)

	s.drainBuf = make([]core.Entity, 0, 64)
	s.compositeBuf = make([]hitComposite, 0, 16)
	s.compositeIdx = make(map[core.Entity]int, 16)
	s.seenCells = make(map[uint64]bool, 256)

	s.statTriggered.Store(0)
	s.statConverted.Store(0)
	s.statMerged.Store(0)
	s.statCursorRejects.Store(0)
	s.statDisabled.Store(0)
	s.buffers.Reset()
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
		event.EventGameResetRequest,
	}
}

func (s *ExplosionSystem) HandleEvent(ev event.GameEvent) {
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
		if ev.Type == event.EventFireSpecialRequest || ev.Type == event.EventExplosionRequest {
			s.statDisabled.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventFireSpecialRequest:
		if payload, ok := ev.Payload.(*event.FireSpecialRequestPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.fireFromDust(cursor)
			} else {
				s.statCursorRejects.Add(1)
			}
		}

	case event.EventExplosionRequest:
		if p, ok := ev.Payload.(*event.ExplosionRequestPayload); ok {
			var cursor core.Entity
			if p.Type != event.ExplosionTypeEye {
				cursor = s.world.ResolveCursor(p.Entity)
				if cursor == 0 {
					s.statCursorRejects.Add(1)
					return
				}
			}
			radius := p.Radius
			if radius == 0 {
				radius = s.baseRadius
			}
			s.addCenter(cursor, p.X, p.Y, radius, p.Type)
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

	dtNano := s.world.Resources.Time.DeltaTimeNano()

	write := 0
	for i := range transRes.ExplosionCount {
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

// fireFromDust converts every dust particle into an explosion center.
// Centers are collected in dense store order: addCenter merges against centers
// already placed and emits combat events, so a map's order would decide which
// merges happen and which entities take knockback.
func (s *ExplosionSystem) fireFromDust(cursor core.Entity) {
	dustEntities := s.world.Components.Dust.Entities()
	if len(dustEntities) == 0 {
		return
	}

	clear(s.seenCells)
	s.centerBuf = s.centerBuf[:0]

	for _, e := range dustEntities {
		p, ok := s.world.Positions.GetPosition(e)
		if !ok {
			continue
		}
		key := posKey(p.X, p.Y)
		if s.seenCells[key] {
			continue
		}
		s.seenCells[key] = true
		s.centerBuf = append(s.centerBuf, vmath.Point{X: p.X, Y: p.Y})
	}
	s.buffers.Observe(2, len(s.centerBuf))
	s.buffers.Observe(6, len(s.seenCells))

	event.EmitDeath(s.world.Resources.Event.Queue, 0, dustEntities...)

	for _, p := range s.centerBuf {
		s.addCenter(cursor, p.X, p.Y, s.baseRadius, event.ExplosionTypeDust)
	}
}

func (s *ExplosionSystem) addCenter(cursor core.Entity, x, y int, radius float64, explosionType event.ExplosionType) {
	transRes := s.world.Resources.Transient

	// Merge check - only merge same type
	for i := range transRes.ExplosionCount {
		c := &transRes.ExplosionBacking[i]
		if c.Type != explosionType {
			continue
		}

		dx := float64(x - c.X)
		dy := float64(y - c.Y)
		distSq := vmath.MagnitudeSqF(dx, dy)

		if distSq <= parameter.ExplosionMergeThresholdSq {
			c.Age = 0

			c.Intensity = min(c.Intensity+parameter.ExplosionIntensityBoost, parameter.ExplosionIntensityCap)
			c.Radius = min(max(c.Radius, radius)+parameter.ExplosionRadiusBoost, s.radiusCap)

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
		Intensity: 1.0,
		Age:       0,
		Type:      explosionType,
	}

	// Process area effects (combat + optional glyph conversion)
	s.processExplosionArea(cursor, x, y, radius, explosionType)

	s.statTriggered.Add(1)
}

// processExplosionArea handles entity collection and event emission for explosion effects
// Single-pass sweep: collects combat entities (always), converts glyphs (dust only)
func (s *ExplosionSystem) processExplosionArea(cursorEntity core.Entity, centerX, centerY int, radius float64, explosionType event.ExplosionType) {
	config := s.world.Resources.Config

	// Determine behavior based on explosion type
	var attackType component.CombatAttackType
	convertGlyphs := false

	switch explosionType {
	case event.ExplosionTypeDust:
		attackType = component.CombatAttackExplosion
		convertGlyphs = true
	case event.ExplosionTypeMissile:
		attackType = component.CombatAttackMissile
	case event.ExplosionTypeEye:
		return // Visual only, combat handled by EyeSystem
	default:
		return
	}

	// Calculate bounds with aspect correction
	radiusCells := int(radius)
	radiusCellsY := radiusCells / 2

	minX := max(0, centerX-radiusCells)
	maxX := min(config.MapWidth-1, centerX+radiusCells)
	minY := max(0, centerY-radiusCellsY)
	maxY := min(config.MapHeight-1, centerY+radiusCellsY)

	radiusSq := radius * radius

	// Clear reuse buffers
	s.entityBuf = s.entityBuf[:0]
	s.dustEntryBuf = s.dustEntryBuf[:0]

	// Combat entity collectors
	s.drainBuf = s.drainBuf[:0]
	s.compositeBuf = s.compositeBuf[:0]
	clear(s.compositeIdx)

	// Single-pass area sweep
	var entityBuf [parameter.MaxEntitiesPerCell]core.Entity
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := float64(x - centerX)
			dyCirc := vmath.ScaleToCircularF(float64(y - centerY))
			distSq := vmath.CircleDistSqF(dx, dyCirc)

			if distSq > radiusSq {
				continue
			}

			count := s.world.Positions.GetAllEntitiesAtInto(x, y, entityBuf[:])
			for i := range count {
				entity := entityBuf[i]
				// Drain - collect for combat
				if s.world.Components.Drain.HasEntity(entity) {
					s.drainBuf = append(s.drainBuf, entity)
					continue
				}

				// Composite member - collect by header
				if memberComp, ok := s.world.Components.Member.GetPtr(entity); ok {
					headerEntity := memberComp.HeaderEntity
					headerComp, ok := s.world.Components.Header.GetPtr(headerEntity)
					if !ok {
						continue
					}

					switch headerComp.Type {
					case component.CompositeTypeUnit, component.CompositeTypeAblative:
						if ci, ok := s.compositeIdx[headerEntity]; ok {
							s.compositeBuf[ci].members = append(s.compositeBuf[ci].members, entity)
							break
						}
						s.compositeIdx[headerEntity] = len(s.compositeBuf)
						// members backs a queued payload, so it must stay a fresh allocation
						s.compositeBuf = append(s.compositeBuf, hitComposite{
							header:  headerEntity,
							members: []core.Entity{entity},
						})
					}
					continue
				}

				// Glyph - convert to dust (dust explosion only)
				if convertGlyphs {
					glyphComp, ok := s.world.Components.Glyph.GetPtr(entity)
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
	s.buffers.Observe(0, len(s.entityBuf))
	s.buffers.Observe(1, len(s.dustEntryBuf))
	s.buffers.Observe(3, len(s.drainBuf))
	s.buffers.Observe(4, len(s.compositeBuf))
	s.buffers.Observe(5, len(s.compositeIdx))

	// Emit combat events for drains; the implicit single-hit form avoids a slice per drain
	for _, drainEntity := range s.drainBuf {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attackType,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: drainEntity,
			HasOrigin:    true,
			OriginX:      centerX,
			OriginY:      centerY,
		})
	}

	// Emit combat events for composites (batched by header)
	for i := range s.compositeBuf {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attackType,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: s.compositeBuf[i].header,
			HitEntities:  s.compositeBuf[i].members,
			HasOrigin:    true,
			OriginX:      centerX,
			OriginY:      centerY,
		})
	}

	// Glyph death and dust spawn (dust only)
	if convertGlyphs && len(s.entityBuf) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, s.entityBuf...)

		event.EmitBatch(s.world.Resources.Event.Queue, event.DustBatchPool, event.EventDustSpawnBatchRequest, s.dustEntryBuf)

		s.statConverted.Add(int64(len(s.entityBuf)))
	}
}
