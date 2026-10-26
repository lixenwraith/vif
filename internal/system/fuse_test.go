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

// beamWorld runs the systems a beam passes through, the timers, materialize and
// death, and the others a test names, over a world it built
type beamWorld struct {
	w           *engine.World
	timers      engine.System
	materialize engine.System
	handlers    []event.Handler
	delivered   []event.GameEvent // every event delivered, in order
}

func newBeamWorld(w *engine.World, others ...engine.System) *beamWorld {
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	b := &beamWorld{w: w, timers: ready(NewTimerSystem(w)), materialize: ready(NewMaterializeSystem(w))}
	for _, s := range append([]engine.System{b.timers, b.materialize, ready(NewDeathSystem(w))}, others...) {
		if h, ok := s.(event.Handler); ok {
			b.handlers = append(b.handlers, h)
		}
	}
	return b
}

// newEntranceWorld runs a data species' entrance over an 80x40 world whose one
// program is the warden
func newEntranceWorld(t *testing.T) *beamWorld {
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
	return newBeamWorld(w, ready(NewFuseSystem(w)), ready(NewBehaviorSystem(w, set)))
}

// deliver hands each event, then each one its handling raised, to the systems
// that take it
func (b *beamWorld) deliver(events ...event.GameEvent) {
	for len(events) > 0 {
		b.delivered = append(b.delivered, events...)
		for _, ev := range events {
			for _, h := range b.handlers {
				if slices.Contains(h.EventTypes(), ev.Type) {
					h.HandleEvent(ev)
				}
			}
		}
		events = b.w.Resources.Event.Queue.Consume()
	}
}

// tick ages the beams one step and delivers what their completion raised
func (b *beamWorld) tick() {
	b.timers.Update()
	b.materialize.Update()
	b.deliver(b.w.Resources.Event.Queue.Consume()...)
}

// sent returns the events of type et delivered since the mark
func (b *beamWorld) sent(et event.EventType, mark int) []event.GameEvent {
	var out []event.GameEvent
	for _, ev := range b.delivered[mark:] {
		if ev.Type == et {
			out = append(out, ev)
		}
	}
	return out
}

// beamTicks is how many steps the swarm-style beam every spawn behind one rides runs
func beamTicks() int {
	return int(component.MaterializeStyles[component.SpawnTypeSwarm].Duration() / parameter.GameUpdateInterval)
}

func fuseWarden(e *beamWorld) {
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

// spawnCell is the header cell a swarm or quasar spawn request names
func spawnCell(payload any) (int, int) {
	switch p := payload.(type) {
	case *event.SwarmSpawnRequestPayload:
		return p.X, p.Y
	case *event.QuasarSpawnRequestPayload:
		return p.X, p.Y
	}
	return -1, -1
}

// newFusionWorld is a 40x24 world with two cursors and two drains, running the
// fuse and the spirits; it returns the first cursor and the drains
func newFusionWorld(t *testing.T) (*beamWorld, core.Entity, [2]core.Entity) {
	t.Helper()
	w, cursor, _ := testCursorWorld(t)
	var drains [2]core.Entity
	for i, x := range []int{20, 24} {
		drains[i] = w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(drains[i], component.PositionComponent{X: x, Y: 12})
		w.Components.Drain.SetComponent(drains[i], component.DrainComponent{})
	}
	return newBeamWorld(w, ready(NewFuseSystem(w)), ready(NewSpiritSystem(w))), cursor, drains
}

// TestAFusionLandsWhenItsBeamEnds: every fusion plays a player-domain beam naming
// its species over the footprint, spirits flying the drains in unless the effect
// is the beam alone, and its spawn crosses on that footprint the tick the beam,
// and so its visual, ends; nothing waits beside the beam
func TestAFusionLandsWhenItsBeamEnds(t *testing.T) {
	for _, c := range []struct {
		name          string
		request       func(cursor core.Entity, drains [2]core.Entity) event.GameEvent
		species       component.SpeciesType
		spawn         event.EventType
		spirits       int
		width, height int
		headX, headY  int
	}{
		{
			name: "drain pair",
			request: func(_ core.Entity, d [2]core.Entity) event.GameEvent {
				return event.GameEvent{Type: event.EventFuseSwarmRequest, Domain: core.DomainPlayer,
					Payload: &event.FuseSwarmRequestPayload{DrainA: d[0], DrainB: d[1], Effect: event.FuseEffectSpirit}}
			},
			species: component.SpeciesSwarm, spawn: event.EventSwarmSpawnRequest, spirits: 2,
			width: parameter.SwarmWidth, height: parameter.SwarmHeight,
			headX: parameter.SwarmHeaderOffsetX, headY: parameter.SwarmHeaderOffsetY,
		},
		{
			name: "drain pair, the beam alone",
			request: func(_ core.Entity, d [2]core.Entity) event.GameEvent {
				return event.GameEvent{Type: event.EventFuseSwarmRequest, Domain: core.DomainPlayer,
					Payload: &event.FuseSwarmRequestPayload{DrainA: d[0], DrainB: d[1], Effect: event.FuseEffectMaterialize}}
			},
			species: component.SpeciesSwarm, spawn: event.EventSwarmSpawnRequest,
			width: parameter.SwarmWidth, height: parameter.SwarmHeight,
			headX: parameter.SwarmHeaderOffsetX, headY: parameter.SwarmHeaderOffsetY,
		},
		{
			name: "quasar",
			request: func(cursor core.Entity, _ [2]core.Entity) event.GameEvent {
				return event.GameEvent{Type: event.EventFuseQuasarRequest, Payload: &event.FuseQuasarRequestPayload{Entity: cursor}}
			},
			species: component.SpeciesQuasar, spawn: event.EventQuasarSpawnRequest,
			width: parameter.QuasarWidth, height: parameter.QuasarHeight,
			headX: parameter.QuasarHeaderOffsetX, headY: parameter.QuasarHeaderOffsetY,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, cursor, drains := newFusionWorld(t)
			w := b.w
			b.deliver(c.request(cursor, drains))
			beams := w.Components.Materialize.Entities()
			if len(beams) != 1 || beams[0].Domain() != core.DomainPlayer {
				t.Fatalf("beams = %v, want one player-domain beam", beams)
			}
			beam, _ := w.Components.Materialize.GetComponent(beams[0])
			if beam.Species != c.species || beam.AreaWidth != c.width || beam.AreaHeight != c.height {
				t.Fatalf("beam = %+v, want species %d over a %dx%d footprint", beam, c.species, c.width, c.height)
			}
			if n := len(b.sent(event.EventSpiritSpawnRequest, 0)); n != c.spirits || w.Components.Drain.CountEntities() != 0 {
				t.Fatalf("%d spirits with %d drains left, want %d and none", n, w.Components.Drain.CountEntities(), c.spirits)
			}

			mark := len(b.delivered)
			for range beamTicks() - 1 {
				b.tick()
			}
			if n := len(b.sent(c.spawn, mark)); n != 0 || w.Components.Materialize.CountEntities() != 1 {
				t.Fatalf("%d spawns before the beam ended", n)
			}
			b.tick()
			spawns := b.sent(c.spawn, mark)
			if len(spawns) != 1 || spawns[0].Domain != core.DomainPlayer || w.Components.Materialize.CountEntities() != 0 {
				t.Fatalf("spawns = %+v as the beam ended, want one crossing", spawns)
			}
			if x, y := spawnCell(spawns[0].Payload); x != beam.TargetX+c.headX || y != beam.TargetY+c.headY {
				t.Fatalf("the spawn names (%d, %d), off the beam's footprint at (%d, %d)", x, y, beam.TargetX, beam.TargetY)
			}
		})
	}
}

// TestAQuasarFusionWaitsForTheOneInFlight: the beam is the pending fusion, so a
// second request is refused while it flies and admitted once it has landed; a
// drain pair's beam holds back nothing
func TestAQuasarFusionWaitsForTheOneInFlight(t *testing.T) {
	b, cursor, drains := newFusionWorld(t)
	quasarBeams := func() (n int) {
		for _, e := range b.w.Components.Materialize.Entities() {
			if m, _ := b.w.Components.Materialize.GetComponent(e); m.Species == component.SpeciesQuasar {
				n++
			}
		}
		return n
	}
	request := event.GameEvent{Type: event.EventFuseQuasarRequest, Payload: &event.FuseQuasarRequestPayload{Entity: cursor}}
	b.deliver(event.GameEvent{Type: event.EventFuseSwarmRequest, Domain: core.DomainPlayer,
		Payload: &event.FuseSwarmRequestPayload{DrainA: drains[0], DrainB: drains[1]}})
	b.deliver(request)
	if n := quasarBeams(); n != 1 {
		t.Fatalf("%d quasar beams beside a drain pair's, want one", n)
	}
	b.tick()
	b.deliver(request)
	if n := quasarBeams(); n != 1 {
		t.Fatalf("%d quasar beams after a second request in flight, want the first alone", n)
	}
	for range beamTicks() {
		b.tick()
	}
	b.deliver(request)
	if n := quasarBeams(); n != 1 {
		t.Fatalf("%d quasar beams after a request once the first landed, want a new one", n)
	}
}
