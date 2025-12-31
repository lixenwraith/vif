package system
// @lixen: #dev{feat[drain(render,system)]}

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MaterializeSystem manages materializer animations and triggering spawnLightning completion
type MaterializeSystem struct {
	world *engine.World
	res   engine.Resources

	matStore  *engine.Store[component.MaterializeComponent]
	protStore *engine.Store[component.ProtectionComponent]

	enabled bool
}

// NewMaterializeSystem creates a new materialize system
func NewMaterializeSystem(world *engine.World) engine.System {
	s := &MaterializeSystem{
		world: world,
		res:   engine.GetResources(world),

		matStore:  engine.GetStore[component.MaterializeComponent](world),
		protStore: engine.GetStore[component.ProtectionComponent](world),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *MaterializeSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *MaterializeSystem) initLocked() {
	s.enabled = true
}

// Priority returns the system's priority
// Must run before DrainSystem which listens to completion
func (s *MaterializeSystem) Priority() int {
	return constant.PriorityMaterialize
}

// EventTypes returns event types handled
func (s *MaterializeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventMaterializeRequest,
	}
}

// HandleEvent processes requests to spawnLightning visual effects
func (s *MaterializeSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventMaterializeRequest {
		if payload, ok := ev.Payload.(*event.MaterializeRequestPayload); ok {
			s.spawnMaterializeEffect(payload.X, payload.Y, payload.Type)
		}
	}
}

// Update updates materialize spawner entities and triggers spawnLightning completion events
func (s *MaterializeSystem) Update() {
	if !s.enabled {
		return
	}

	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Progress velocity in Q16.16: full progress (Scale) over duration
	durationFixed := vmath.FromFloat(constant.MaterializeAnimationDuration.Seconds())
	progressDelta := vmath.Div(dtFixed, durationFixed)

	entities := s.matStore.All()
	if len(entities) == 0 {
		return
	}

	for _, entity := range entities {
		mat, ok := s.matStore.Get(entity)
		if !ok {
			continue
		}

		mat.Progress += progressDelta

		if mat.Progress >= vmath.Scale {
			s.world.PushEvent(event.EventMaterializeComplete, &event.SpawnCompletePayload{
				X:    mat.TargetX,
				Y:    mat.TargetY,
				Type: mat.Type,
			})
			s.world.DestroyEntity(entity)
			continue
		}

		s.matStore.Set(entity, mat)
	}
}

// spawnMaterializeEffect creates a single materialize effect entity
func (s *MaterializeSystem) spawnMaterializeEffect(targetX, targetY int, spawnType component.SpawnType) {
	config := s.res.Config

	// Clamp target coordinates
	if targetX < 0 {
		targetX = 0
	}
	if targetX >= config.GameWidth {
		targetX = config.GameWidth - 1
	}
	if targetY < 0 {
		targetY = 0
	}
	if targetY >= config.GameHeight {
		targetY = config.GameHeight - 1
	}

	entity := s.world.CreateEntity()

	// TODO: add protection
	s.matStore.Set(entity, component.MaterializeComponent{
		TargetX:  targetX,
		TargetY:  targetY,
		Progress: 0,
		Width:    1,
		Type:     spawnType,
	})
}