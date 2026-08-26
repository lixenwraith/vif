package system

import (
	"sync/atomic"
	"time"

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

// ExplosionSystem owns explosion centers: merge, lifetime and shared combat resolution.
// It resolves geometry only; the producer names the combat family and visual variant.
type ExplosionSystem struct {
	world *engine.World

	baseRadius float64 // Default radius in cells
	radiusCap  float64 // Maximum radius after merges (cells)

	// Rotating eviction cursor, used only once the center array is full
	evictNext int

	// Reusable collectors for the area sweep
	compositeBuf []hitComposite
	compositeIdx map[core.Entity]int

	statTriggered *atomic.Int64
	statMerged    *atomic.Int64
	statEvicted   *atomic.Int64
	rejects       rejectionTelemetry
	buffers       bufferTelemetry

	enabled bool
}

func NewExplosionSystem(world *engine.World) engine.System {
	s := &ExplosionSystem{world: world}

	reg := world.Resources.Status
	s.statTriggered = reg.Ints.Get("explosion.triggered")
	s.statMerged = reg.Ints.Get("explosion.merged")
	s.statEvicted = reg.Ints.Get("explosion.evicted")
	s.rejects = newRejectionTelemetry(reg, "explosion")
	s.buffers = newBufferTelemetry(reg, "explosion", "composites")

	s.Init()
	return s
}

func (s *ExplosionSystem) Init() {
	s.baseRadius = parameter.ExplosionFieldRadius
	s.radiusCap = parameter.ExplosionRadiusCapFixed
	s.evictNext = 0

	s.compositeBuf = make([]hitComposite, 0, 16)
	s.compositeIdx = make(map[core.Entity]int, 16)

	// Centers are this system's state, so this system clears them
	s.world.Resources.Transient.ClearExplosions()

	s.statTriggered.Store(0)
	s.statMerged.Store(0)
	s.statEvicted.Store(0)
	s.rejects.Reset()
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
		event.EventExplosionRequest,
		event.EventExplosionBatchRequest,
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
		return
	}

	// A pooled payload is released on every path, disabled included
	if !s.enabled {
		s.rejects.disabled.Add(1)
		if p, ok := ev.Payload.(*event.ExplosionBatchRequestPayload); ok {
			event.ReleaseExplosionBatchRequest(p)
		}
		return
	}

	switch ev.Type {
	case event.EventExplosionRequest:
		if p, ok := ev.Payload.(*event.ExplosionRequestPayload); ok {
			one := [1]event.ExplosionCenterEntry{{X: p.X, Y: p.Y}}
			s.spawn(p.Entity, one[:], p.Radius, p.Duration, p.Type, p.Attack)
		}

	case event.EventExplosionBatchRequest:
		if p, ok := ev.Payload.(*event.ExplosionBatchRequestPayload); ok {
			s.spawn(p.Entity, p.Centers, p.Radius, p.Duration, p.Type, p.Attack)
			event.ReleaseExplosionBatchRequest(p)
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
		c := &transRes.ExplosionBacking[i]
		c.Age += dtNano
		if c.Age < c.DurNano {
			if write != i {
				transRes.ExplosionBacking[write] = *c
			}
			write++
		}
	}
	transRes.ExplosionCount = write
}

// spawn applies request defaults and damage credit, then places one center per position
func (s *ExplosionSystem) spawn(owner core.Entity, centers []event.ExplosionCenterEntry,
	radius float64, duration time.Duration, visual event.ExplosionType, attack component.CombatAttackType) {

	if len(centers) == 0 {
		return
	}
	if radius <= 0 {
		radius = s.baseRadius
	}
	if duration <= 0 {
		duration = parameter.ExplosionFieldDuration
	}

	// Damage needs a live cursor for kill credit; a visual-only blast does not
	var cursor core.Entity
	if attack != component.CombatAttackNone {
		cursor = s.world.ResolveCursor(owner)
		if cursor == 0 {
			s.rejects.cursor.Add(1)
			return
		}
	}

	durNano := duration.Nanoseconds()
	for i := range centers {
		s.addCenter(cursor, centers[i].X, centers[i].Y, radius, durNano, visual, attack)
	}
}

// addCenter merges into a live center of the same visual variant or places a new one.
// A merged request resolves no combat: the absorbing center already swept its area.
func (s *ExplosionSystem) addCenter(cursor core.Entity, x, y int, radius float64, durNano int64,
	visual event.ExplosionType, attack component.CombatAttackType) {

	transRes := s.world.Resources.Transient

	for i := range transRes.ExplosionCount {
		c := &transRes.ExplosionBacking[i]
		if c.Type != visual {
			continue
		}

		dx := float64(x - c.X)
		dy := float64(y - c.Y)
		if vmath.MagnitudeSqF(dx, dy) <= parameter.ExplosionMergeThresholdSq {
			c.Age = 0
			c.Intensity = min(c.Intensity+parameter.ExplosionIntensityBoost, parameter.ExplosionIntensityCap)
			c.Radius = min(max(c.Radius, radius)+parameter.ExplosionRadiusBoost, s.radiusCap)
			s.statMerged.Add(1)
			return
		}
	}

	var idx int
	if transRes.ExplosionCount < parameter.ExplosionCenterCap {
		idx = transRes.ExplosionCount
		transRes.ExplosionCount++
	} else {
		// Full: evict in rotation. Every entry is mid-lifetime, so scanning for the oldest buys nothing.
		idx = s.evictNext
		s.evictNext = (s.evictNext + 1) % parameter.ExplosionCenterCap
		s.statEvicted.Add(1)
	}

	transRes.ExplosionBacking[idx] = engine.ExplosionCenter{
		X:         x,
		Y:         y,
		Radius:    radius,
		Intensity: 1.0,
		Age:       0,
		DurNano:   durNano,
		Type:      visual,
	}

	if attack != component.CombatAttackNone {
		s.resolveArea(cursor, x, y, radius, attack)
	}
	s.statTriggered.Add(1)
}

// resolveArea emits one area attack per shared composite inside the blast ellipse.
// Player entities are never selected here; their producer resolves its own domain
// before the request crosses. Phase 3 replaces the header test with an entity-domain test.
func (s *ExplosionSystem) resolveArea(cursor core.Entity, centerX, centerY int, radius float64, attack component.CombatAttackType) {
	config := s.world.Resources.Config

	radiusCells := int(radius)
	radiusCellsY := radiusCells / 2

	minX := max(0, centerX-radiusCells)
	maxX := min(config.MapWidth-1, centerX+radiusCells)
	minY := max(0, centerY-radiusCellsY)
	maxY := min(config.MapHeight-1, centerY+radiusCellsY)

	radiusSq := radius * radius

	s.compositeBuf = s.compositeBuf[:0]
	clear(s.compositeIdx)

	var cellBuf [parameter.MaxEntitiesPerCell]core.Entity
	for y := minY; y <= maxY; y++ {
		dyCirc := vmath.ScaleToCircularF(float64(y - centerY))
		for x := minX; x <= maxX; x++ {
			dx := float64(x - centerX)
			if vmath.CircleDistSqF(dx, dyCirc) > radiusSq {
				continue
			}

			// Shared only: this enumeration decides shared combat, so it must not
			// observe player entities (D-1)
			count := s.world.Positions.GetEntitiesAtInto(x, y, engine.ScopeShared, cellBuf[:])
			for i := range count {
				entity := cellBuf[i]
				memberComp, ok := s.world.Components.Member.GetPtr(entity)
				if !ok {
					continue
				}
				headerEntity := memberComp.HeaderEntity
				headerComp, ok := s.world.Components.Header.GetPtr(headerEntity)
				if !ok {
					continue
				}
				switch headerComp.Type {
				case component.CompositeTypeUnit, component.CompositeTypeAblative:
				default:
					continue
				}

				if ci, ok := s.compositeIdx[headerEntity]; ok {
					s.compositeBuf[ci].members = append(s.compositeBuf[ci].members, entity)
					continue
				}
				s.compositeIdx[headerEntity] = len(s.compositeBuf)
				// members backs a queued payload, so it must stay a fresh allocation
				s.compositeBuf = append(s.compositeBuf, hitComposite{
					header:  headerEntity,
					members: []core.Entity{entity},
				})
			}
		}
	}
	s.buffers.Observe(0, len(s.compositeBuf))

	for i := range s.compositeBuf {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   attack,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: s.compositeBuf[i].header,
			HitEntities:  s.compositeBuf[i].members,
			HasOrigin:    true,
			OriginX:      centerX,
			OriginY:      centerY,
		})
	}
}
