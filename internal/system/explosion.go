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

// ExplosionSystem resolves shared combat geometry. Presentation belongs to
// TransientSystem in the player domain and can never suppress this resolver.
type ExplosionSystem struct {
	world *engine.World

	// Reusable collectors for the area sweep
	compositeBuf []hitComposite
	compositeIdx map[core.Entity]int

	statTriggered *atomic.Int64
	rejects       rejectionTelemetry
	buffers       bufferTelemetry

	enabled bool
}

func NewExplosionSystem(world *engine.World) engine.System {
	s := &ExplosionSystem{world: world}

	reg := world.Resources.Status
	s.statTriggered = reg.Ints.Get("explosion.triggered")
	s.rejects = newRejectionTelemetry(reg, "explosion")
	s.buffers = newBufferTelemetry(reg, "explosion", "composites")

	s.Init()
	return s
}

func (s *ExplosionSystem) Init() {
	s.compositeBuf = make([]hitComposite, 0, 16)
	s.compositeIdx = make(map[core.Entity]int, 16)

	s.statTriggered.Store(0)
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
			s.resolve(p.Entity, one[:], p.Radius, p.Attack)
		}

	case event.EventExplosionBatchRequest:
		if p, ok := ev.Payload.(*event.ExplosionBatchRequestPayload); ok {
			s.resolve(p.Entity, p.Centers, p.Radius, p.Attack)
			event.ReleaseExplosionBatchRequest(p)
		}
	}
}

func (s *ExplosionSystem) Update() {}

// resolve applies request defaults and damage credit, then sweeps every center.
// In particular, it reads no TransientResource state.
func (s *ExplosionSystem) resolve(owner core.Entity, centers []event.ExplosionCenterEntry,
	radius float64, attack component.CombatAttackType) {

	if len(centers) == 0 || attack == component.CombatAttackNone {
		return
	}
	if radius <= 0 {
		radius = parameter.ExplosionFieldRadius
	}

	cursor := s.world.ResolveCursor(owner)
	if cursor == 0 {
		s.rejects.cursor.Add(1)
		return
	}

	for i := range centers {
		s.resolveArea(cursor, centers[i].X, centers[i].Y, radius, attack)
	}
	s.statTriggered.Add(int64(len(centers)))
}

// resolveArea emits one area attack per shared composite inside the blast ellipse.
// Player entities are never selected here; their producer resolves its own domain
// before the request crosses. The header test stands in for an entity-domain test and
// can become one once a player-domain composite exists.
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
