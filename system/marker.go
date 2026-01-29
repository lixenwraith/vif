package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MarkerSystem manages visual area indicators
type MarkerSystem struct {
	world   *engine.World
	enabled bool
}

func NewMarkerSystem(world *engine.World) engine.System {
	s := &MarkerSystem{world: world}
	s.Init()
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
		event.EventGameReset,
	}
}

func (s *MarkerSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.destroyAllMarkers()
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
	dtFixed := vmath.FromFloat(dt.Seconds())

	markerEntities := s.world.Components.Marker.GetAllEntities()
	for _, markerEntity := range markerEntities {
		markerComp, ok := s.world.Components.Marker.GetComponent(markerEntity)
		if !ok {
			continue
		}

		// Pulse update
		if markerComp.PulseRate > 0 {
			// Sine pulse modulation
			gameTime := s.world.Resources.Time.GameTime.UnixNano()
			pulsePhase := vmath.FromFloat(float64(gameTime) / 1e9)
			pulseAngle := vmath.Mul(pulsePhase, markerComp.PulseRate)
			pulseMod := vmath.Sin(pulseAngle)
			// Map [-Scale, Scale] to [0.5, 1.0]
			markerComp.Intensity = vmath.Scale/2 + vmath.Div(pulseMod, 4)
		}

		// Fade update
		if markerComp.FadeMode != 0 {
			timer, hasTimer := s.world.Components.Timer.GetComponent(markerEntity)
			if hasTimer && timer.Remaining > 0 {
				// Calculate fade progress based on remaining time
				// FadeMode 1 = fade out (1.0 -> 0.0)
				// FadeMode 2 = fade in (0.0 -> 1.0)
				// Note: actual timer countdown handled by TimeKeeper
				if markerComp.FadeMode == 1 {
					// Intensity decreases as timer expires
					fadeRate := vmath.Div(vmath.Scale, vmath.FromFloat(timer.Remaining.Seconds()))
					markerComp.Intensity -= vmath.Mul(fadeRate, dtFixed)
					if markerComp.Intensity < 0 {
						markerComp.Intensity = 0
					}
				}
			}
		}

		s.world.Components.Marker.SetComponent(markerEntity, markerComp)
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
		intensity = vmath.Scale
	}

	entity := s.world.CreateEntity()

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

	// Timer for auto-destruction
	if p.Duration > 0 {
		s.world.Components.Timer.SetComponent(entity, component.TimerComponent{
			Remaining: p.Duration,
		})
	}
}

func (s *MarkerSystem) destroyAllMarkers() {
	entities := s.world.Components.Marker.GetAllEntities()
	for _, entity := range entities {
		s.world.Components.Marker.RemoveEntity(entity)
		s.world.Components.Timer.RemoveEntity(entity)
		s.world.DestroyEntity(entity)
	}
}