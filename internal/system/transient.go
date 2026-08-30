package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// TransientSystem manages player-domain presentation: screen overlays and
// short-lived spatial explosion centers.
type TransientSystem struct {
	world *engine.World

	statGrayoutActive  *atomic.Bool
	statStrobeActive   *atomic.Bool
	statExplosionMerge *atomic.Int64
	statExplosionEvict *atomic.Int64

	evictNext int

	enabled bool
}

func NewTransientSystem(world *engine.World) engine.System {
	s := &TransientSystem{
		world: world,
	}

	s.statGrayoutActive = world.Resources.Status.Bools.Get("effects.grayout_active")
	s.statStrobeActive = world.Resources.Status.Bools.Get("effects.strobe_active")
	s.statExplosionMerge = world.Resources.Status.Ints.Get("transient.explosion_merged")
	s.statExplosionEvict = world.Resources.Status.Ints.Get("transient.explosion_evicted")

	s.Init()
	return s
}

func (s *TransientSystem) Init() {
	s.world.Resources.View.Reset()
	s.world.Resources.Transient.ClearExplosions()
	s.statGrayoutActive.Store(false)
	s.statStrobeActive.Store(false)
	s.statExplosionMerge.Store(0)
	s.statExplosionEvict.Store(0)
	s.evictNext = 0
	s.enabled = true
}

func (s *TransientSystem) Name() string {
	return "transient"
}

func (s *TransientSystem) Priority() int {
	return parameter.PriorityEffect
}

func (s *TransientSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGrayoutStart,
		event.EventGrayoutEnd,
		event.EventStrobeRequest,
		event.EventExplosionVisualRequest,
		event.EventExplosionVisualBatchRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *TransientSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventGrayoutStart:
		s.world.Resources.View.Grayout = engine.GrayoutState{
			Active:    true,
			Intensity: 1.0,
		}
		s.statGrayoutActive.Store(true)

	case event.EventGrayoutEnd:
		s.world.Resources.View.Grayout.Active = false
		s.statGrayoutActive.Store(false)

	case event.EventStrobeRequest:
		if payload, ok := ev.Payload.(*event.StrobeRequestPayload); ok {
			s.handleStrobeRequest(payload)
		}

	case event.EventExplosionVisualRequest:
		if p, ok := ev.Payload.(*event.ExplosionVisualRequestPayload); ok {
			s.addExplosionCenter(p.X, p.Y, p.Radius, p.Duration, p.Type)
		}

	case event.EventExplosionVisualBatchRequest:
		if p, ok := ev.Payload.(*event.ExplosionVisualBatchRequestPayload); ok {
			for i := range p.Centers {
				s.addExplosionCenter(p.Centers[i].X, p.Centers[i].Y, p.Radius, p.Duration, p.Type)
			}
		}
	}
}

func (s *TransientSystem) Update() {
	if !s.enabled {
		return
	}
	s.updateExplosions()

	strobe := &s.world.Resources.View.Strobe
	if !strobe.Active {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	strobe.Remaining -= dt

	if strobe.Remaining <= 0 {
		strobe.Active = false
		s.statStrobeActive.Store(false)
	}
}

func (s *TransientSystem) updateExplosions() {
	transient := s.world.Resources.Transient
	if transient.ExplosionCount == 0 {
		return
	}

	dtNano := s.world.Resources.Time.DeltaTimeNano()
	write := 0
	for i := range transient.ExplosionCount {
		c := &transient.ExplosionBacking[i]
		c.Age += dtNano
		if c.Age < c.DurNano {
			if write != i {
				transient.ExplosionBacking[write] = *c
			}
			write++
		}
	}
	transient.ExplosionCount = write
}

// addExplosionCenter owns the visual-only merge and bounded center array. Neither
// decision is observable by ExplosionSystem's shared combat resolver.
func (s *TransientSystem) addExplosionCenter(x, y int, radius float64, duration time.Duration, kind event.ExplosionType) {
	if radius <= 0 {
		radius = parameter.ExplosionFieldRadius
	}
	if duration <= 0 {
		duration = parameter.ExplosionFieldDuration
	}

	transient := s.world.Resources.Transient
	for i := range transient.ExplosionCount {
		c := &transient.ExplosionBacking[i]
		if c.Type != kind {
			continue
		}
		dx, dy := float64(x-c.X), float64(y-c.Y)
		if vmath.MagnitudeSqF(dx, dy) > parameter.ExplosionMergeThresholdSq {
			continue
		}
		c.Age = 0
		c.Intensity = min(c.Intensity+parameter.ExplosionIntensityBoost, parameter.ExplosionIntensityCap)
		c.Radius = min(max(c.Radius, radius)+parameter.ExplosionRadiusBoost, parameter.ExplosionRadiusCapFixed)
		s.statExplosionMerge.Add(1)
		return
	}

	var idx int
	if transient.ExplosionCount < parameter.ExplosionCenterCap {
		idx = transient.ExplosionCount
		transient.ExplosionCount++
	} else {
		idx = s.evictNext
		s.evictNext = (s.evictNext + 1) % parameter.ExplosionCenterCap
		s.statExplosionEvict.Add(1)
	}
	transient.ExplosionBacking[idx] = engine.ExplosionCenter{
		X: x, Y: y, Radius: radius, Intensity: 1,
		DurNano: duration.Nanoseconds(), Type: kind,
	}
}

func (s *TransientSystem) handleStrobeRequest(req *event.StrobeRequestPayload) {
	current := &s.world.Resources.View.Strobe

	duration := time.Duration(req.DurationMs) * time.Millisecond
	if duration == 0 {
		duration = visual.StrobeDefaultDuration
	}

	// Max stacking: compare intensity * remaining seconds
	if current.Active {
		currentWeight := current.Intensity * current.Remaining.Seconds()
		incomingWeight := req.Intensity * duration.Seconds()
		if currentWeight >= incomingWeight {
			return // Keep current
		}
	}

	current.Active = true
	current.Color = req.Color
	current.Intensity = req.Intensity
	current.InitialDuration = duration
	current.Remaining = duration

	s.statStrobeActive.Store(true)
}
