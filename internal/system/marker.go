package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
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

// Domain reports shared: it creates shared marker entities.
func (s *MarkerSystem) Domain() engine.SystemDomain { return engine.SystemShared }

// Requires nothing: markers arrive by request.
func (s *MarkerSystem) Requires() engine.SystemDependencies { return nil }

func (s *MarkerSystem) Priority() int {
	return parameter.PrioritySplash - 10 // Before splash, after game logic
}

func (s *MarkerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMarkerSpawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *MarkerSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
	dtSeconds := dt.Seconds()

	markers := s.world.Components.Marker
	for _, markerEntity := range markers.Entities() {
		markerComp, ok := markers.GetPtr(markerEntity)
		if !ok {
			continue
		}

		// Pulse update
		if markerComp.PulseRate > 0 {
			// PulseRate is cycles per second; SinF accepts radians.
			gameTime := s.world.Resources.Time.GameTimeNano()
			pulseAngle := float64(gameTime) / 1e9 * markerComp.PulseRate * vmath.TwoPi
			pulseMod := vmath.SinF(pulseAngle)
			// Preserve the fixed-path range: [-1,1] maps to [0.25,0.75].
			markerComp.Intensity = 0.5 + pulseMod*0.25
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
					fadeRate := 1.0 / timer.Remaining.Seconds()
					markerComp.Intensity -= fadeRate * dtSeconds
					if markerComp.Intensity < 0 {
						markerComp.Intensity = 0
					}
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

	// Timer for auto-destruction
	if p.Duration > 0 {
		s.world.Components.Timer.SetComponent(entity, component.TimerComponent{
			Remaining: p.Duration,
		})
	}
}

func (s *MarkerSystem) destroyAllMarkers() {
	// Batch destruction mutates the live marker slice, so detach it first.
	entities := s.world.Components.Marker.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}
