package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// SwarmSystem manages the elite enemy entity lifecycle
// Swarm is a 3x5 animated composite, spawned by materialize, that tracks cursor at 2x drain speed, charges the cursor and doesn't get deflected by shield when charging
// Removes one heat on direct cursor collision without shield, only despawns after hitpoints reach zero or by FSM, when dies spawns 2 drains
type SwarmSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewSwarmSystem creates a new quasar system
func NewSwarmSystem(world *engine.World) engine.System {
	s := &SwarmSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("swarm.active")
	s.statCount = world.Resources.Status.Ints.Get("swarm.count")

	s.Init()
	return s
}

func (s *SwarmSystem) Init() {
	s.active = false
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

func (s *SwarmSystem) Priority() int {
	return constant.PrioritySwarm
}

func (s *SwarmSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSwarmSpawnRequest,
		event.EventGameReset,
	}
}

func (s *SwarmSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		if s.active {
			s.Init()
			return
		}

		if !s.enabled {
			return
		}

		switch ev.Type {
		case event.EventSwarmSpawnRequest:

		case event.EventSwarmCancel:
		}
	}
}

func (s *SwarmSystem) Update() {
	if !s.enabled {
		return
	}

}