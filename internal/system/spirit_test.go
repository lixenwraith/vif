package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestASpiritLandsWhatItNamesOnItsArrivalFrame: a spirit naming a species sends one
// arrival, in its own domain, with the species and its footprint, on the tick it
// draws its last frame on its target; a spirit naming none sends nothing
func TestASpiritLandsWhatItNamesOnItsArrivalFrame(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	tick := newTimerTick(w)
	spirits := ready(NewSpiritSystem(w)).(*SpiritSystem)
	spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: core.DomainPlayer,
		Payload: &event.SpiritSpawnRequestPayload{StartX: 2, StartY: 2, TargetX: 30, TargetY: 10, Char: 'x',
			Species: component.SpeciesSwarm, LandX: 28, LandY: 9}})
	spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: core.DomainPlayer,
		Payload: &event.SpiritSpawnRequestPayload{StartX: 60, StartY: 2, TargetX: 31, TargetY: 10, Char: 'x'}})
	carrier := w.Components.Spirit.Entities()[0]

	var arrivals []event.GameEvent
	arrivedOn, lastFrame := 0, 0
	for n := 1; n <= 20; n++ {
		tick.open()
		// Drawn as the spirit renderer draws it
		if timer, ok := w.Components.Timer.GetPtr(carrier); ok && !timer.Expired(component.TimerLifetime) {
			lastFrame = n
		}
		spirits.Update()
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch ev.Type {
			case event.EventSpiritArrived:
				arrivals = append(arrivals, ev)
				arrivedOn = n
			case event.EventDeathBatch:
				tick.deaths.HandleEvent(ev)
			}
		}
	}
	if len(arrivals) != 1 || arrivals[0].Domain != core.DomainPlayer {
		t.Fatalf("arrivals = %+v, want one in the player domain", arrivals)
	}
	if p, _ := arrivals[0].Payload.(*event.SpiritArrivedPayload); p == nil || *p != (event.SpiritArrivedPayload{X: 28, Y: 9, Species: component.SpeciesSwarm}) {
		t.Fatalf("the arrival carries %+v, want the swarm on the footprint at (28, 9)", arrivals[0].Payload)
	}
	if want := int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval); arrivedOn != want || lastFrame != want {
		t.Fatalf("arrived on tick %d with the last frame on %d, want both on %d", arrivedOn, lastFrame, want)
	}
	if n := w.Components.Spirit.CountEntities(); n != 0 {
		t.Fatalf("%d spirits outlived their flight", n)
	}
}
