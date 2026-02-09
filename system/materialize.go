package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MaterializeSystem manages materializer animations and triggering spawn completion
type MaterializeSystem struct {
	world *engine.World

	enabled bool
}

// NewMaterializeSystem creates a new materialize system
func NewMaterializeSystem(world *engine.World) engine.System {
	s := &MaterializeSystem{
		world: world,
	}

	s.Init()
	return s
}

// Init resets session state for new game
func (s *MaterializeSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *MaterializeSystem) Name() string {
	return "materialize"
}

// Priority returns the system's priority
func (s *MaterializeSystem) Priority() int {
	return parameter.PriorityMaterialize
}

// EventTypes returns event types handled
func (s *MaterializeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMaterializeRequest,
		event.EventMaterializeAreaRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes requests to spawn visual effects
func (s *MaterializeSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventMaterializeRequest:
		if payload, ok := ev.Payload.(*event.MaterializeRequestPayload); ok {
			s.spawnMaterializeEffect(payload.X, payload.Y, 1, 1, 1, payload.Type)
		}

	case event.EventMaterializeAreaRequest:
		if payload, ok := ev.Payload.(*event.MaterializeAreaRequestPayload); ok {
			width := payload.AreaWidth
			height := payload.AreaHeight
			if width < 1 {
				width = 1
			}
			if height < 1 {
				height = 1
			}
			s.spawnMaterializeEffect(payload.X, payload.Y, width, height, 1, payload.Type)
		}
	}
}

// Update updates materialize spawner entities and triggers spawn completion events
func (s *MaterializeSystem) Update() {
	if !s.enabled {
		return
	}

	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Progress velocity in Q32.32: full progress (Scale) over duration
	durationFixed := vmath.FromFloat(parameter.MaterializeAnimationDuration.Seconds())
	progressDelta := vmath.Div(dtFixed, durationFixed)

	matEntities := s.world.Components.Materialize.GetAllEntities()
	if len(matEntities) == 0 {
		return
	}

	for _, matEntity := range matEntities {
		matComp, ok := s.world.Components.Materialize.GetComponent(matEntity)
		if !ok {
			continue
		}

		matComp.Progress += progressDelta

		if matComp.Progress >= vmath.Scale {
			s.world.PushEvent(event.EventMaterializeComplete, &event.SpawnCompletePayload{
				X:    matComp.TargetX,
				Y:    matComp.TargetY,
				Type: matComp.Type,
				// Note: SpawnCompletePayload may need AreaWidth/Height if consumers need it
			})
			s.world.DestroyEntity(matEntity)
			continue
		}

		s.world.Components.Materialize.SetComponent(matEntity, matComp)
	}
}

// spawnMaterializeEffect creates a single materialize effect entity
func (s *MaterializeSystem) spawnMaterializeEffect(targetX, targetY, areaWidth, areaHeight, beamWidth int, spawnType component.SpawnType) {
	config := s.world.Resources.Config

	// Clamp target coordinates
	if targetX < 0 {
		targetX = 0
	}
	if targetX >= config.MapWidth {
		targetX = config.MapWidth - 1
	}
	if targetY < 0 {
		targetY = 0
	}
	if targetY >= config.MapHeight {
		targetY = config.MapHeight - 1
	}

	entity := s.world.CreateEntity()

	// TODO: add protection
	s.world.Components.Materialize.SetComponent(entity, component.MaterializeComponent{
		TargetX:    targetX,
		TargetY:    targetY,
		AreaWidth:  areaWidth,
		AreaHeight: areaHeight,
		Progress:   0,
		BeamWidth:  beamWidth,
		Type:       spawnType,
	})
}