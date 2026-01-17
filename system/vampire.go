package system

import (
	"fmt"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// VampireSystem manages energy drain on hit
type VampireSystem struct {
	world *engine.World

	// Telemetry
	statCount *atomic.Int64

	enabled bool
}

// NewVampireSystem creates a new quasar system
func NewVampireSystem(world *engine.World) engine.System {
	s := &VampireSystem{
		world: world,
	}

	s.statCount = world.Resources.Status.Ints.Get("vampire.count")

	s.Init()
	return s
}

func (s *VampireSystem) Init() {
	s.statCount.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *VampireSystem) Name() string {
	return "vampire"
}

func (s *VampireSystem) Priority() int {
	return constant.PriorityVampire
}

func (s *VampireSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventVampireDrainRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *VampireSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventVampireDrainRequest:
		if payload, ok := ev.Payload.(*event.VampireDrainRequestPayload); ok {
			s.vampiricDrain(payload)
		}
	}
}

func (s *VampireSystem) Update() {
	if !s.enabled {
		return
	}
}

func (s *VampireSystem) vampiricDrain(payload *event.VampireDrainRequestPayload) {
	targetEntity := payload.TargetEntity
	drainAmount := payload.DrainAmount
	cursorEntity := s.world.Resources.Cursor.Entity
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok || drainAmount == 0 {
		return
	}

	// Determine energy polarity for rewarding drain
	// TODO: change energy system event and logic to handle this and gold reward
	currentEnergy := energyComp.Current

	// Emit events to energy and lightning systems
	s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Delta:      drainAmount,
		Spend:      false,
		Reward:     true,
		Convergent: false,
	})

	s.world.DebugPrint(fmt.Sprintf("delta: %d, energy: %d", drainAmount, currentEnergy))

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	lightningColor := component.LightningGold
	if currentEnergy < 0 {
		lightningColor = component.LightningPurple
	}

	s.world.PushEvent(event.EventLightningSpawn, &event.LightningSpawnPayload{
		Owner:     targetEntity,
		OriginX:   cursorPos.X,
		OriginY:   cursorPos.Y,
		TargetX:   targetPos.X,
		TargetY:   targetPos.Y,
		ColorType: lightningColor,
		Duration:  constant.GameUpdateInterval,
		Tracked:   false,
	})

	s.statCount.Add(1)
}