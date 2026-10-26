package system

import (
	"slices"
	"testing"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// entranceWorld runs the systems a data species' entrance passes through, over an
// 80x40 world whose one program is the warden
type entranceWorld struct {
	w           *engine.World
	timers      engine.System
	materialize engine.System
	handlers    []event.Handler
}

func newEntranceWorld(t *testing.T) *entranceWorld {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	doc, err := toml.NewParser([]byte(wardenProgram)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"warden": doc})
	if err != nil {
		t.Fatal(err)
	}
	w.Resources.Behaviors = set.Prototypes()
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	e := &entranceWorld{w: w, timers: ready(NewTimerSystem(w)), materialize: ready(NewMaterializeSystem(w))}
	for _, s := range []engine.System{e.timers, e.materialize, ready(NewDeathSystem(w)), ready(NewFuseSystem(w)), ready(NewBehaviorSystem(w, set))} {
		if h, ok := s.(event.Handler); ok {
			e.handlers = append(e.handlers, h)
		}
	}
	return e
}

// deliver hands each event, then each one its handling raised, to the systems
// that take it
func (e *entranceWorld) deliver(events ...event.GameEvent) {
	for len(events) > 0 {
		for _, ev := range events {
			for _, h := range e.handlers {
				if slices.Contains(h.EventTypes(), ev.Type) {
					h.HandleEvent(ev)
				}
			}
		}
		events = e.w.Resources.Event.Queue.Consume()
	}
}

// tick ages the beams one step and delivers what their completion raised
func (e *entranceWorld) tick() {
	e.timers.Update()
	e.materialize.Update()
	e.deliver(e.w.Resources.Event.Queue.Consume()...)
}

// beamTicks is how many steps the swarm-style beam a data species enters by runs
func beamTicks() int {
	return int(component.MaterializeStyles[component.SpawnTypeSwarm].Duration() / parameter.GameUpdateInterval)
}

func fuseWarden(e *entranceWorld) {
	e.deliver(event.GameEvent{
		Type:    event.EventFuseBehaviorRequest,
		Payload: &event.FuseBehaviorRequestPayload{Program: "warden", X: wardenX, Y: wardenY},
	})
}

// TestAFusedDataSpeciesLandsWhenItsBeamEnds: the request spawns nothing, the beam
// is a Shared entity over the footprint naming the species, and the warden lands
// on that footprint the tick the beam completes
func TestAFusedDataSpeciesLandsWhenItsBeamEnds(t *testing.T) {
	e := newEntranceWorld(t)
	w := e.w
	fuseWarden(e)
	beams := w.Components.Materialize.Entities()
	if len(beams) != 1 || beams[0].Domain() != core.DomainShared {
		t.Fatalf("beams = %v, want one Shared beam", beams)
	}
	beam, _ := w.Components.Materialize.GetComponent(beams[0])
	p := w.Resources.Behaviors[0]
	if beam.Species != p.Species || beam.AreaWidth != p.Width || beam.AreaHeight != p.Height {
		t.Fatalf("beam = %+v, want the warden's species over its %dx%d footprint", beam, p.Width, p.Height)
	}

	for range beamTicks() - 1 {
		e.tick()
	}
	if n := w.Components.Behavior.CountEntities(); n != 0 || w.Components.Materialize.CountEntities() != 1 {
		t.Fatalf("%d wardens landed before the beam ended", n)
	}
	e.tick()
	headers := w.Components.Behavior.Entities()
	if len(headers) != 1 || w.Components.Materialize.CountEntities() != 0 {
		t.Fatalf("%d wardens landed as the beam ended, want one", len(headers))
	}
	if pos, _ := w.Positions.GetPosition(headers[0]); pos.X != beam.TargetX+p.HeaderX || pos.Y != beam.TargetY+p.HeaderY {
		t.Fatalf("the warden landed at (%d, %d), off the beam's footprint at (%d, %d)", pos.X, pos.Y, beam.TargetX, beam.TargetY)
	}
}

// TestACancelEndsItsProgramsBeamsInFlight: a beam is the pending spawn, so the
// cancel that ends a program's instances ends it too, and nothing lands after
func TestACancelEndsItsProgramsBeamsInFlight(t *testing.T) {
	e := newEntranceWorld(t)
	w := e.w
	fuseWarden(e)
	e.tick()
	e.deliver(event.GameEvent{Type: event.EventBehaviorCancelRequest, Payload: &event.BehaviorCancelRequestPayload{Program: "warden"}})
	if n := w.Components.Materialize.CountEntities(); n != 0 {
		t.Fatalf("%d beams outlived the cancel", n)
	}
	for range beamTicks() {
		e.tick()
	}
	if n := w.Components.Behavior.CountEntities(); n != 0 {
		t.Fatalf("%d wardens landed after their cancel", n)
	}
}

// TestOnlyASharedBeamLandsADataSpecies: a player-domain beam ran on one instance
// alone, so its completion names a species no other instance spawns (D-7)
func TestOnlyASharedBeamLandsADataSpecies(t *testing.T) {
	e := newEntranceWorld(t)
	w := e.w
	e.deliver(event.GameEvent{
		Type:   event.EventMaterializeComplete,
		Domain: core.DomainPlayer,
		Payload: &event.MaterializeCompletedPayload{
			X: wardenX, Y: wardenY, Type: component.SpawnTypeSwarm, Species: w.Resources.Behaviors[0].Species,
		},
	})
	if n := w.Components.Behavior.CountEntities(); n != 0 {
		t.Fatalf("a player beam landed %d wardens", n)
	}
}
