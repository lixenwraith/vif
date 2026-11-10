package system

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// FlashSystem holds the cells flashing away. The cells a tick sends flash together
// as one batch, which ends at its deadline.
type FlashSystem struct {
	world *engine.World

	ended []core.Entity
	// spare is the storage of the batches that ended, which the next ones fill
	spare [][]component.EffectCell

	toggle
}

func NewFlashSystem(world *engine.World) engine.System {
	s := &FlashSystem{
		world: world,
	}
	return s
}

// Init resets session state for new game
func (s *FlashSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *FlashSystem) Name() string {
	return "flash"
}

func (s *FlashSystem) Priority() int {
	return parameter.PriorityFlash
}

func (s *FlashSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFlashSpawnOneRequest,
		event.EventFlashSpawnBatchRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *FlashSystem) HandleEvent(ev event.GameEvent) {
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

	switch ev.Type {
	case event.EventFlashSpawnOneRequest:
		if payload, ok := ev.Payload.(*event.FlashRequestPayload); ok {
			s.flash(component.EffectCell{X: payload.X, Y: payload.Y, Rune: payload.Char})
		}

	case event.EventFlashSpawnBatchRequest:
		if batch, ok := ev.Payload.(*event.BatchPayload[component.EffectCell]); ok {
			s.flash(batch.Entries...)
			event.EffectBatchPool.Release(batch)
		}
	}
}

// Update ends the batches whose flash has run, a disabled system's too
func (s *FlashSystem) Update() {
	s.ended = s.ended[:0]
	s.world.Components.Flash.Each(func(e core.Entity, f *component.FlashComponent) bool {
		if !timerRunning(s.world, e, component.TimerDeadline) {
			s.spare = append(s.spare, f.Cells[:0])
			s.ended = append(s.ended, e)
		}
		return true
	})
	s.world.DestroyEntitiesBatch(s.ended)
}

// flash adds cells to the batch this tick opened, opening it with the first of them
func (s *FlashSystem) flash(cells ...component.EffectCell) {
	flashes, timers := s.world.Components.Flash, s.world.Components.Timer
	for _, e := range flashes.Entities() {
		// A deadline no tick has aged yet is this tick's
		timer, ok := timers.GetPtr(e)
		if !ok || timer.Slots[component.TimerDeadline].Remaining != parameter.DestructionFlashDuration {
			continue
		}
		if f, ok := flashes.GetPtr(e); ok {
			f.Cells = append(f.Cells, cells...)
			return
		}
	}

	var kept []component.EffectCell
	if n := len(s.spare); n > 0 {
		kept, s.spare = s.spare[n-1], s.spare[:n-1]
	}
	e := s.world.CreateEntity(core.DomainPlayer)
	flashes.SetComponent(e, component.FlashComponent{Cells: append(kept, cells...)})
	armTimer(s.world, e, component.TimerDeadline, parameter.DestructionFlashDuration)
}
