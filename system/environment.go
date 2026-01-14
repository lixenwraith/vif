package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Environment holds global environmental effects
// Applied to composites during movement integration
type EnvironmentSystem struct {
	world *engine.World

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statGrayoutActive *atomic.Bool

	enabled bool
}

// NewEnvironmentSystem creates a new quasar system
func NewEnvironmentSystem(world *engine.World) engine.System {
	s := &EnvironmentSystem{
		world: world,
	}

	s.statGrayoutActive = world.Resources.Status.Bools.Get("environment.grayout")

	s.Init()
	return s
}

func (s *EnvironmentSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statGrayoutActive.Store(false)
	s.enabled = true
}

func (s *EnvironmentSystem) Priority() int {
	return constant.PrioritySwarm
}

func (s *EnvironmentSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGrayoutStart,
		event.EventGrayoutEnd,
		event.EventGameReset,
	}
}

func (s *EnvironmentSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
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

}

// StartGrayout activates persistent grayscale effect
func (s *EnvironmentSystem) StartGrayout() {
	envEntity := s.world.Resources.Environment.Entity
	envComp, _ := s.world.Components.Environment.GetComponent(envEntity)

	envComp.GrayoutActive = true
	envComp.GrayoutIntensity = 1.0
	s.world.Components.Environment.SetComponent(envEntity, envComp)
	s.statGrayoutActive.Store(true)
}

// EndGrayout deactivates persistent grayscale effect
func (s *EnvironmentSystem) EndGrayout() {
	envEntity := s.world.Resources.Environment.Entity
	envComp, _ := s.world.Components.Environment.GetComponent(envEntity)

	envComp.GrayoutActive = false
	s.world.Components.Environment.SetComponent(envEntity, envComp)
	s.statGrayoutActive.Store(false)
}