package system

import (
	"slices"
	"testing"

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
	spirits     engine.System // updated each tick when the world flies spirits
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

// tick ages the beams and spirits one step and delivers what their ends raised
func (b *beamWorld) tick() {
	b.timers.Update()
	b.materialize.Update()
	if b.spirits != nil {
		b.spirits.Update()
	}
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
	spirits := ready(NewSpiritSystem(w))
	b := newBeamWorld(w, ready(NewFuseSystem(w)), spirits)
	b.spirits = spirits
	return b, cursor, drains
}

// TestADrainPairsSwarmLandsAsItsSpiritsArrive: a drain pair's fusion flies its
// drains' spirits and plays no beam, the first spirit naming the swarm and the
// footprint the others fly onto, and the swarm crosses on that footprint the tick
// the spirits draw their arrival frame, where the timer the beam replaced landed it
func TestADrainPairsSwarmLandsAsItsSpiritsArrive(t *testing.T) {
	for _, c := range []struct {
		name   string
		effect event.FuseEffect
	}{
		{"spirit", event.FuseEffectSpirit},
		{"none", event.FuseEffectNone},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, _, drains := newFusionWorld(t)
			w := b.w
			b.deliver(event.GameEvent{Type: event.EventFuseSwarmRequest, Domain: core.DomainPlayer,
				Payload: &event.FuseSwarmRequestPayload{DrainA: drains[0], DrainB: drains[1], Effect: c.effect}})
			if n := w.Components.Materialize.CountEntities(); n != 0 {
				t.Fatalf("%d beams, want the spirits alone", n)
			}
			spirits := w.Components.Spirit.Entities()
			if len(spirits) != 2 || w.Components.Drain.CountEntities() != 0 {
				t.Fatalf("%d spirits with %d drains left, want 2 and none", len(spirits), w.Components.Drain.CountEntities())
			}
			var carrier component.SpiritComponent
			carriers := 0
			for _, e := range spirits {
				if sp, _ := w.Components.Spirit.GetComponent(e); sp.Species != component.SpeciesNone {
					carrier = sp
					carriers++
				}
			}
			if carriers != 1 || carrier.Species != component.SpeciesSwarm || spirits[0].Domain() != core.DomainPlayer {
				t.Fatalf("%d spirits name a species (the first %+v), want one player-domain spirit naming the swarm", carriers, carrier)
			}
			for _, e := range spirits {
				sp, _ := w.Components.Spirit.GetComponent(e)
				if x, y := int(sp.TargetX), int(sp.TargetY); x < carrier.LandX || x >= carrier.LandX+parameter.SwarmWidth ||
					y < carrier.LandY || y >= carrier.LandY+parameter.SwarmHeight {
					t.Fatalf("a spirit flies to (%d, %d), off the footprint at (%d, %d)", x, y, carrier.LandX, carrier.LandY)
				}
			}

			mark := len(b.delivered)
			flight := int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval)
			for range flight - 1 {
				b.tick()
			}
			if n := len(b.sent(event.EventSwarmSpawnRequest, mark)); n != 0 {
				t.Fatalf("%d swarms before the spirits arrived", n)
			}
			b.tick()
			spawns := b.sent(event.EventSwarmSpawnRequest, mark)
			if len(spawns) != 1 || spawns[0].Domain != core.DomainPlayer {
				t.Fatalf("spawns = %+v as the spirits arrived, want one crossing", spawns)
			}
			if x, y := spawnCell(spawns[0].Payload); x != carrier.LandX+parameter.SwarmHeaderOffsetX || y != carrier.LandY+parameter.SwarmHeaderOffsetY {
				t.Fatalf("the swarm lands at (%d, %d), off the footprint at (%d, %d)", x, y, carrier.LandX, carrier.LandY)
			}
			for _, e := range spirits {
				if timer, ok := w.Components.Timer.GetPtr(e); !ok || timer.Expired(component.TimerLifetime) ||
					timer.Elapsed(component.TimerLifetime) != parameter.SpiritAnimationDuration {
					t.Fatal("the swarm landed off the spirits' arrival frame")
				}
			}

			b.tick()
			if n := len(b.sent(event.EventSwarmSpawnRequest, mark)); n != 1 || w.Components.Spirit.CountEntities() != 0 {
				t.Fatalf("%d swarms and %d spirits a tick on, want one and none", n, w.Components.Spirit.CountEntities())
			}
		})
	}
}

// TestOnlyAPlayerSpiritLandsAFusion: a fusion's spirits run on one instance alone,
// so a Shared spirit's arrival crosses nothing (D-7)
func TestOnlyAPlayerSpiritLandsAFusion(t *testing.T) {
	b, _, _ := newFusionWorld(t)
	b.deliver(event.GameEvent{Type: event.EventSpiritArrived, Domain: core.DomainShared,
		Payload: &event.SpiritArrivedPayload{X: 10, Y: 10, Species: component.SpeciesSwarm}})
	if n := len(b.sent(event.EventSwarmSpawnRequest, 0)); n != 0 {
		t.Fatalf("a Shared spirit crossed %d swarms", n)
	}
	b.deliver(event.GameEvent{Type: event.EventSpiritArrived, Domain: core.DomainPlayer,
		Payload: &event.SpiritArrivedPayload{X: 10, Y: 10, Species: component.SpeciesSwarm}})
	if n := len(b.sent(event.EventSwarmSpawnRequest, 0)); n != 1 {
		t.Fatalf("a player spirit crossed %d swarms, want one", n)
	}
}

// TestAFusionLandsWhenItsBeamEnds: a fusion a beam plays, a quasar's or a drain
// pair's asking for the beam, flies no spirits and plays one player-domain beam
// naming its species over the footprint, and its spawn crosses on that footprint
// the tick the beam, and so its visual, ends; nothing waits beside the beam
func TestAFusionLandsWhenItsBeamEnds(t *testing.T) {
	for _, c := range []struct {
		name          string
		request       func(cursor core.Entity, drains [2]core.Entity) event.GameEvent
		species       component.SpeciesType
		spawn         event.EventType
		width, height int
		headX, headY  int
	}{
		{
			name: "drain pair, the beam",
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
			if n := len(b.sent(event.EventSpiritSpawnRequest, 0)); n != 0 || w.Components.Drain.CountEntities() != 0 {
				t.Fatalf("%d spirits with %d drains left, want none and none", n, w.Components.Drain.CountEntities())
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
// drain pair's spirits hold back nothing
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
		t.Fatalf("%d quasar beams beside a drain pair's spirits, want one", n)
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
