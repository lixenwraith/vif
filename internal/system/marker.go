package system

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// MarkerSystem manages visual area indicators
type MarkerSystem struct {
	world *engine.World
	toggle
}

func NewMarkerSystem(world *engine.World) engine.System {
	s := &MarkerSystem{world: world}
	return s
}

func (s *MarkerSystem) Init() {
	s.enabled = true
}

func (s *MarkerSystem) Name() string {
	return "marker"
}

func (s *MarkerSystem) Priority() int {
	return parameter.PrioritySplash - 10 // Before splash, after game logic
}

func (s *MarkerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMarkerSpawnRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *MarkerSystem) HandleEvent(ev event.GameEvent) {

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

	if ev.Type == event.EventMarkerSpawnRequest {
		if payload, ok := ev.Payload.(*event.MarkerSpawnRequestPayload); ok {
			s.spawnMarker(payload)
		}
	}
}

func (s *MarkerSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSeconds := dt.Seconds()

	markers := s.world.Components.Marker
	for _, markerEntity := range markers.Entities() {
		markerComp, ok := markers.GetPtr(markerEntity)
		if !ok {
			continue
		}

		// Pulse update
		if markerComp.PulseRate > 0 {
			// PulseRate is cycles per second; Sin accepts radians.
			gameTime := s.world.Resources.Time.GameTimeNano()
			pulseAngle := float64(gameTime) / 1e9 * markerComp.PulseRate * vmath.TwoPi
			pulseMod := vmath.Sin(pulseAngle)
			// Preserve the fixed-path range: [-1,1] maps to [0.25,0.75].
			markerComp.Intensity = 0.5 + pulseMod*0.25
		}

		// FadeMode 1 fades out over the marker's remaining lifetime; 2 (fade in) is unimplemented
		if markerComp.FadeMode == 1 {
			if timer, ok := s.world.Components.Timer.GetPtr(markerEntity); ok {
				if remaining := timer.Slots[component.TimerLifetime].Remaining; remaining > 0 {
					markerComp.Intensity = max(markerComp.Intensity-dtSeconds/remaining.Seconds(), 0)
				}
			}
		}

	}
}

func (s *MarkerSystem) spawnMarker(p *event.MarkerSpawnRequestPayload) {
	width := p.Width
	height := p.Height
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	intensity := p.Intensity
	if intensity <= 0 {
		intensity = 1.0
	}

	entity := s.world.CreateEntity(core.DomainShared)

	s.world.Components.Marker.SetComponent(entity, component.MarkerComponent{
		X:         p.X,
		Y:         p.Y,
		Width:     width,
		Height:    height,
		Shape:     p.Shape,
		Color:     p.Color,
		Intensity: intensity,
		PulseRate: p.PulseRate,
		FadeMode:  p.FadeMode,
	})

	if p.Duration > 0 {
		armTimer(s.world, entity, component.TimerLifetime, p.Duration)
	}
}

func (s *MarkerSystem) destroyAllMarkers() {
	// Batch destruction mutates the live marker slice, so detach it first.
	entities := s.world.Components.Marker.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}
