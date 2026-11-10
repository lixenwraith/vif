package system

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// FadeoutSystem holds the cells fading out. The cells a tick sends fade together as
// one batch, which ends at its deadline.
type FadeoutSystem struct {
	world *engine.World

	ended []core.Entity
	// spare is the storage of the batches that ended, which the next ones fill
	spare [][]component.EffectCell

	toggle
}

func NewFadeoutSystem(world *engine.World) engine.System {
	s := &FadeoutSystem{
		world: world,
	}
	return s
}

func (s *FadeoutSystem) Init() {
	s.enabled = true
}

func (s *FadeoutSystem) Name() string {
	return "fadeout"
}

func (s *FadeoutSystem) Priority() int {
	return parameter.PriorityFadeout
}

func (s *FadeoutSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFadeoutSpawnBatch,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *FadeoutSystem) HandleEvent(ev event.GameEvent) {
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

	if batch, ok := ev.Payload.(*event.BatchPayload[component.EffectCell]); ok {
		s.fade(batch.Entries)
		event.EffectBatchPool.Release(batch)
	}
}

// Update ends the batches whose fade has run, a disabled system's too
func (s *FadeoutSystem) Update() {
	s.ended = s.ended[:0]
	s.world.Components.Fadeout.Each(func(e core.Entity, f *component.FadeoutComponent) bool {
		if !timerRunning(s.world, e, component.TimerDeadline) {
			s.spare = append(s.spare, f.Cells[:0])
			s.ended = append(s.ended, e)
		}
		return true
	})
	s.world.DestroyEntitiesBatch(s.ended)
}

// fade adds cells to the batch this tick opened, opening it with the first of them
func (s *FadeoutSystem) fade(cells []component.EffectCell) {
	fadeouts, timers := s.world.Components.Fadeout, s.world.Components.Timer
	for _, e := range fadeouts.Entities() {
		// A deadline no tick has aged yet is this tick's
		timer, ok := timers.GetPtr(e)
		if !ok || timer.Slots[component.TimerDeadline].Remaining != parameter.FadeoutDuration {
			continue
		}
		if f, ok := fadeouts.GetPtr(e); ok {
			f.Cells = append(f.Cells, cells...)
			return
		}
	}

	var kept []component.EffectCell
	if n := len(s.spare); n > 0 {
		kept, s.spare = s.spare[n-1], s.spare[:n-1]
	}
	e := s.world.CreateEntity(core.DomainPlayer)
	fadeouts.SetComponent(e, component.FadeoutComponent{Cells: append(kept, cells...)})
	armTimer(s.world, e, component.TimerDeadline, parameter.FadeoutDuration)
}
