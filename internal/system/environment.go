package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Environment holds global environmental effects
// Applied to composites during movement integration
type EnvironmentSystem struct {
	world *engine.World

	rng *vmath.FastRand

	WindActive bool
	// Global wind velocity
	WindVelX float64
	WindVelY float64

	// Telemetry
	statWindActive *atomic.Bool

	enabled bool
}

// NewEnvironmentSystem creates a new quasar system
func NewEnvironmentSystem(world *engine.World) engine.System {
	s := &EnvironmentSystem{
		world: world,
	}

	s.statWindActive = world.Resources.Status.Bools.Get("environment.wind_active")

	s.Init()
	return s
}

func (s *EnvironmentSystem) Init() {
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.statWindActive.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *EnvironmentSystem) Name() string {
	return "environment"
}

// Domain reports shared: it draws the shared stream and writes no player state.
func (s *EnvironmentSystem) Domain() engine.SystemDomain { return engine.SystemShared }

func (s *EnvironmentSystem) Priority() int {
	return parameter.PrioritySwarm
}

func (s *EnvironmentSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *EnvironmentSystem) HandleEvent(ev event.GameEvent) {
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
	}

	if !s.enabled {
		return
	}

	switch ev.Type {

	}
}

func (s *EnvironmentSystem) Update() {
	if !s.enabled {
		return
	}
	s.statWindActive.Store(s.WindActive)
}
