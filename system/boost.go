package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

type BoostSystem struct {
	world *engine.World
	res   engine.Resources

	boostStore *engine.Store[component.BoostComponent]

	// Cached metric pointers
	statActive    *atomic.Bool
	statRemaining *atomic.Int64
}

func NewBoostSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &BoostSystem{
		world: world,
		res:   res,

		boostStore: engine.GetStore[component.BoostComponent](world),

		statActive:    res.Status.Bools.Get("boost.active"),
		statRemaining: res.Status.Ints.Get("boost.remaining"),
	}

	return s
}

// Init
func (s *BoostSystem) Init() {}

func (s *BoostSystem) Priority() int {
	return constant.PriorityBoost
}

func (s *BoostSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBoostActivate,
		event.EventBoostDeactivate,
		event.EventBoostExtend,
	}
}

func (s *BoostSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventBoostActivate:
		if payload, ok := ev.Payload.(*event.BoostActivatePayload); ok {
			s.activate(payload.Duration)
		}
	case event.EventBoostDeactivate:
		s.deactivate()
	case event.EventBoostExtend:
		if payload, ok := ev.Payload.(*event.BoostExtendPayload); ok {
			s.extend(payload.Duration)
		}
	}
}

// Update handles boost duration decrement using Delta Time
func (s *BoostSystem) Update() {
	dt := s.res.Time.DeltaTime
	cursorEntity := s.res.Cursor.Entity

	boost, ok := s.boostStore.Get(cursorEntity)
	if !ok || !boost.Active {
		return
	}

	boost.Remaining -= dt
	if boost.Remaining <= 0 {
		boost.Remaining = 0
		boost.Active = false
	}

	s.boostStore.Add(cursorEntity, boost)

	s.statActive.Store(boost.Active)
	s.statRemaining.Store(int64(boost.Remaining))
}

func (s *BoostSystem) activate(duration time.Duration) {
	cursorEntity := s.res.Cursor.Entity

	boost, ok := s.boostStore.Get(cursorEntity)
	if !ok {
		boost = component.BoostComponent{}
	}

	// If already active, this resets/overwrites duration (or adds? usually activate implies fresh start)
	// Design choice: Activate overwrites. Extend adds.
	boost.Active = true
	boost.Remaining = duration
	boost.TotalDuration = duration // Reset total for UI progress bar if applicable

	s.boostStore.Add(cursorEntity, boost)
}

func (s *BoostSystem) deactivate() {
	cursorEntity := s.res.Cursor.Entity

	boost, ok := s.boostStore.Get(cursorEntity)
	if !ok {
		return
	}
	boost.Active = false
	boost.Remaining = 0
	s.boostStore.Add(cursorEntity, boost)
}

func (s *BoostSystem) extend(duration time.Duration) {
	cursorEntity := s.res.Cursor.Entity

	boost, ok := s.boostStore.Get(cursorEntity)
	if !ok || !boost.Active {
		return
	}

	boost.Remaining += duration
	// Optionally cap at TotalDuration or allow it to grow?
	// Allowing growth is standard for extensions
	if boost.Remaining > boost.TotalDuration {
		boost.TotalDuration = boost.Remaining
	}

	s.boostStore.Add(cursorEntity, boost)
}