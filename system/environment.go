package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Environment holds global environmental effects
// Applied to composites during movement integration
type EnvironmentSystem struct {
	world *engine.World

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statGrayoutActive    *atomic.Bool
	statGrayoutTotalTime *atomic.Int64

	enabled bool
}

// NewEnvironmentSystem creates a new quasar system
func NewEnvironmentSystem(world *engine.World) engine.System {
	s := &EnvironmentSystem{
		world: world,
	}

	s.statGrayoutActive = world.Resources.Status.Bools.Get("environment.grayout_active")
	s.statGrayoutTotalTime = world.Resources.Status.Ints.Get("environment.grayout_total_time")

	s.Init()
	return s
}

func (s *EnvironmentSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statGrayoutActive.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *EnvironmentSystem) Name() string {
	return "environment"
}

func (s *EnvironmentSystem) Priority() int {
	return parameter.PrioritySwarm
}

func (s *EnvironmentSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGrayoutStart,
		event.EventGrayoutEnd,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *EnvironmentSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventGrayoutStart:
		s.StartGrayout()

	case event.EventGrayoutEnd:
		s.EndGrayout()
	}
}

func (s *EnvironmentSystem) Update() {
	if !s.enabled {
		return
	}

	envEntities := s.world.Components.Environment.GetAllEntities()
	if len(envEntities) == 0 {
		return
	}
	envEntity := envEntities[0]
	envComp, _ := s.world.Components.Environment.GetComponent(envEntity)

	if envComp.GrayoutActive {
		dt := s.world.Resources.Time.DeltaTime
		envComp.GrayoutDuration += dt
		s.world.Components.Environment.GetComponent(envEntity)
		s.statGrayoutTotalTime.Add(int64(dt))
	}
}

// StartGrayout activates persistent grayscale effect
func (s *EnvironmentSystem) StartGrayout() {
	envEntities := s.world.Components.Environment.GetAllEntities()
	if len(envEntities) == 0 {
		return
	}
	// TODO: multi env profile selection, active flag in component?
	envEntity := envEntities[0]
	envComp, ok := s.world.Components.Environment.GetComponent(envEntity)
	if !ok {
		return
	}

	envComp.GrayoutActive = true
	envComp.GrayoutIntensity = 1.0
	s.world.Components.Environment.SetComponent(envEntity, envComp)
	s.statGrayoutActive.Store(true)
}

// EndGrayout deactivates persistent grayscale effect
func (s *EnvironmentSystem) EndGrayout() {
	envEntities := s.world.Components.Environment.GetAllEntities()
	if len(envEntities) == 0 {
		return
	}
	envEntity := envEntities[0]
	envComp, ok := s.world.Components.Environment.GetComponent(envEntity)
	if !ok {
		return
	}

	envComp.GrayoutActive = false
	s.world.Components.Environment.SetComponent(envEntity, envComp)
	s.statGrayoutActive.Store(false)
}