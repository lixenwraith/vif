package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

type BoostSystem struct {
	world *engine.World
	res   engine.CoreResources

	boostStore *engine.Store[components.BoostComponent]
}

func NewBoostSystem(world *engine.World) engine.System {
	return &BoostSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		boostStore: engine.GetStore[components.BoostComponent](world),
	}
}

// Init
func (s *BoostSystem) Init() {}

func (s *BoostSystem) Priority() int {
	return constants.PriorityBoost
}

func (s *BoostSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventBoostActivate,
		events.EventBoostDeactivate,
		events.EventBoostExtend,
	}
}

func (s *BoostSystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventBoostActivate:
		if payload, ok := event.Payload.(*events.BoostActivatePayload); ok {
			s.activate(payload.Duration)
		}
	case events.EventBoostDeactivate:
		s.deactivate()
	case events.EventBoostExtend:
		if payload, ok := event.Payload.(*events.BoostExtendPayload); ok {
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
}

func (s *BoostSystem) activate(duration time.Duration) {
	cursorEntity := s.res.Cursor.Entity

	boost, ok := s.boostStore.Get(cursorEntity)
	if !ok {
		boost = components.BoostComponent{}
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