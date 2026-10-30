package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// PulseSystem advances every pulse in either domain: a mount's Shared one identically
// on every instance, each hit on a cursor its owner's to apply; a cursor's on its
// owner alone, its Shared targets re-derived from the geometry it crosses (D-3).
type PulseSystem struct {
	world *engine.World

	area  blastArea
	ended []core.Entity

	statCount   *atomic.Int64
	statStrikes *atomic.Int64

	toggle
}

func NewPulseSystem(world *engine.World) engine.System {
	s := &PulseSystem{world: world}
	s.statCount = world.Resources.Status.Ints.Get("pulse.count")
	s.statStrikes = world.Resources.Status.Ints.Get("pulse.strikes")
	return s
}

func (s *PulseSystem) Init() {
	// Batch destruction mutates the live slice, so detach it first
	s.world.DestroyEntitiesBatch(s.world.Components.Pulse.GetAllEntities())
	s.statCount.Store(0)
	s.statStrikes.Store(0)
	s.enabled = true
}

func (s *PulseSystem) Name() string { return "pulse" }

func (s *PulseSystem) Priority() int { return parameter.PriorityPulse }

func (s *PulseSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventMetaSystemCommandRequest}
}

func (s *PulseSystem) HandleEvent(ev event.GameEvent) {
	if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && payload.SystemName == s.Name() {
		s.enabled = payload.Enabled
	}
}

// Update advances each pulse independently of the others, so store order, which
// mixes the domains, decides nothing
func (s *PulseSystem) Update() {
	if !s.enabled {
		return
	}
	dt := s.world.Resources.Time.DeltaTime
	pulses := s.world.Components.Pulse
	s.statCount.Store(int64(pulses.CountEntities()))

	s.ended = s.ended[:0]
	for _, e := range pulses.Entities() {
		if p, ok := pulses.GetPtr(e); ok && !s.advance(e, p, dt) {
			s.ended = append(s.ended, e)
		}
	}
	s.world.DestroyEntitiesBatch(s.ended)
}

// advance carries one pulse a tick on, reporting whether it lasts. A ridden pulse
// follows its host, ends with the mount that names it, and holds, neither ageing
// nor striking, while that mount waits to be armed.
func (s *PulseSystem) advance(e core.Entity, p *component.PulseComponent, dt time.Duration) bool {
	if p.Anchor != 0 {
		pos, placed := s.world.Positions.GetPosition(p.Anchor)
		mount, mounted := s.world.Components.Mount.GetComponent(p.Anchor)
		if !placed || !mounted || mount.Live != e {
			return false
		}
		p.X, p.Y = pos.X, pos.Y
		if mount.Trigger == component.MountArmed && !mount.Armed {
			return true
		}
	}

	first := p.Elapsed == 0
	p.Elapsed += dt
	if p.HitTimer -= dt; p.HitTimer <= 0 && (first || p.HitInterval > 0) {
		s.strike(p)
		p.HitTimer = p.HitInterval
	}
	return p.Sustained || p.Elapsed < p.Duration
}

// strike hits what lies inside the size the pulse has swollen to
func (s *PulseSystem) strike(p *component.PulseComponent) {
	radius := p.Radius * p.Size()
	s.area.resetOne(p.X, p.Y, radius, s.world.Resources.Config.CellAspect)
	s.statStrikes.Add(1)
	if p.Hostile {
		strikeCursorsIn(s.world, s.area.contains, p.Damage)
		return
	}
	// Player targets resolve here; Shared targets are re-derived from the crossing geometry
	strikePlayerTargets(s.world, p.Owner, &s.area, p.Attack, p.Charged)
	s.world.PushCrossing(event.EventExplosionRequest, &event.ExplosionRequestPayload{
		Entity:  p.Owner,
		X:       p.X,
		Y:       p.Y,
		Radius:  radius,
		Attack:  p.Attack,
		Charged: p.Charged,
	})
}
