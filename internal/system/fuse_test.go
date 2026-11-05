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

// materializeWorld runs the systems a materialize passes through, the timers,
// materialize and death, and the others a test names, over a world it built
type materializeWorld struct {
	w           *engine.World
	timers      engine.System
	materialize engine.System
	spirits     engine.System // updated each tick when the world flies spirits
	handlers    []event.Handler
	delivered   []event.GameEvent // every event delivered, in order
}

func newMaterializeWorld(w *engine.World, others ...engine.System) *materializeWorld {
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	b := &materializeWorld{w: w, timers: ready(NewTimerSystem(w)), materialize: ready(NewMaterializeSystem(w))}
	for _, s := range append([]engine.System{b.timers, b.materialize, ready(NewDeathSystem(w))}, others...) {
		if h, ok := s.(event.Handler); ok {
			b.handlers = append(b.handlers, h)
		}
	}
	return b
}

// deliver hands each event, then each one its handling raised, to the systems
// that take it
func (b *materializeWorld) deliver(events ...event.GameEvent) {
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

// tick ages the materializes and spirits one step and delivers what their ends raised
func (b *materializeWorld) tick() {
	b.timers.Update()
	b.materialize.Update()
	if b.spirits != nil {
		b.spirits.Update()
	}
	b.deliver(b.w.Resources.Event.Queue.Consume()...)
}

// sent returns the events of type et delivered since the mark
func (b *materializeWorld) sent(et event.EventType, mark int) []event.GameEvent {
	var out []event.GameEvent
	for _, ev := range b.delivered[mark:] {
		if ev.Type == et {
			out = append(out, ev)
		}
	}
	return out
}

// materializeTicks is how many steps the swarm-style materialize runs, which every
// spawn behind one rides
func materializeTicks() int {
	return int(component.MaterializeStyles[component.SpawnTypeSwarm].Duration() / parameter.GameUpdateInterval)
}

// landings returns the land requests naming the species delivered since the mark
func (b *materializeWorld) landings(species component.SpeciesType, mark int) []event.GameEvent {
	var out []event.GameEvent
	for _, ev := range b.sent(event.EventSpeciesLandRequest, mark) {
		if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok && p.Species == species {
			out = append(out, ev)
		}
	}
	return out
}

// landCell is the header cell a land request names
func landCell(ev event.GameEvent) (int, int) {
	if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok {
		return p.X, p.Y
	}
	return -1, -1
}

// pairRequest is the drain system's: the two drains into a swarm
func pairRequest(drains [2]core.Entity, effect event.FuseEffect) event.GameEvent {
	return event.GameEvent{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
		Entities: drains[:], Target: component.SpeciesSwarm, Effect: effect,
	}}
}

// quasarRequest is the quasar region's: every drain of the cursor into a quasar
func quasarRequest(cursor core.Entity) event.GameEvent {
	return event.GameEvent{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
		Sources: []event.FuseSource{{Species: component.SpeciesDrain}}, Cursor: cursor,
		Target: component.SpeciesQuasar, Effect: event.FuseEffectMaterialize,
	}}
}

// newFusionWorld is a 40x24 world with two cursors and two drains, running the
// fuse and the spirits; it returns the first cursor and the drains
func newFusionWorld(t *testing.T) (*materializeWorld, core.Entity, [2]core.Entity) {
	t.Helper()
	w, cursor, _ := testCursorWorld(t)
	var drains [2]core.Entity
	for i, x := range []int{20, 24} {
		drains[i] = w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(drains[i], component.PositionComponent{X: x, Y: 12})
		w.Components.Drain.SetComponent(drains[i], component.DrainComponent{})
	}
	spirits := ready(NewSpiritSystem(w))
	b := newMaterializeWorld(w, ready(NewFuseSystem(w)), spirits)
	b.spirits = spirits
	return b, cursor, drains
}

// TestADrainPairsSwarmLandsAsItsSpiritsArrive: a drain pair's fusion flies its
// drains' spirits and plays no materialize, the first spirit naming the swarm and
// the footprint the others fly onto, and the swarm crosses on that footprint the
// tick the spirits draw their arrival frame, where the timer the materialize
// replaced landed it
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
			b.deliver(pairRequest(drains, c.effect))
			if n := w.Components.Materialize.CountEntities(); n != 0 {
				t.Fatalf("%d materializes, want the spirits alone", n)
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
			area := component.Footprints[component.SpeciesSwarm].At(carrier.LandX, carrier.LandY)
			for _, e := range spirits {
				sp, _ := w.Components.Spirit.GetComponent(e)
				if x, y := int(sp.TargetX), int(sp.TargetY); x < area.X || x >= area.X+area.Width ||
					y < area.Y || y >= area.Y+area.Height {
					t.Fatalf("a spirit flies to (%d, %d), off the footprint headed at (%d, %d)", x, y, carrier.LandX, carrier.LandY)
				}
			}

			mark := len(b.delivered)
			flight := int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval)
			for range flight - 1 {
				b.tick()
			}
			if n := len(b.landings(component.SpeciesSwarm, mark)); n != 0 {
				t.Fatalf("%d swarms before the spirits arrived", n)
			}
			b.tick()
			spawns := b.landings(component.SpeciesSwarm, mark)
			if len(spawns) != 1 || spawns[0].Domain != core.DomainPlayer {
				t.Fatalf("spawns = %+v as the spirits arrived, want one crossing", spawns)
			}
			if x, y := landCell(spawns[0]); x != carrier.LandX || y != carrier.LandY {
				t.Fatalf("the swarm lands at (%d, %d), off the header the spirit names (%d, %d)", x, y, carrier.LandX, carrier.LandY)
			}
			for _, e := range spirits {
				if timer, ok := w.Components.Timer.GetPtr(e); !ok || timer.Expired(component.TimerLifetime) ||
					timer.Elapsed(component.TimerLifetime) != parameter.SpiritAnimationDuration {
					t.Fatal("the swarm landed off the spirits' arrival frame")
				}
			}

			b.tick()
			if n := len(b.landings(component.SpeciesSwarm, mark)); n != 1 || w.Components.Spirit.CountEntities() != 0 {
				t.Fatalf("%d swarms and %d spirits a tick on, want one and none", n, w.Components.Spirit.CountEntities())
			}
		})
	}
}

// TestAFusionLandsWhenItsMaterializeEnds: a fusion a materialize plays, a quasar's
// or a drain pair's asking for one, flies no spirits and plays one player-domain
// materialize naming its species over the footprint, and its spawn crosses on that
// footprint the tick the materialize, and so its visual, ends; nothing waits beside it
func TestAFusionLandsWhenItsMaterializeEnds(t *testing.T) {
	for _, c := range []struct {
		name    string
		request func(cursor core.Entity, drains [2]core.Entity) event.GameEvent
		species component.SpeciesType
	}{
		{
			name: "drain pair, the materialize",
			request: func(_ core.Entity, d [2]core.Entity) event.GameEvent {
				return pairRequest(d, event.FuseEffectMaterialize)
			},
			species: component.SpeciesSwarm,
		},
		{
			name: "quasar",
			request: func(cursor core.Entity, _ [2]core.Entity) event.GameEvent {
				return quasarRequest(cursor)
			},
			species: component.SpeciesQuasar,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, cursor, drains := newFusionWorld(t)
			w := b.w
			b.deliver(c.request(cursor, drains))
			materializes := w.Components.Materialize.Entities()
			if len(materializes) != 1 || materializes[0].Domain() != core.DomainPlayer {
				t.Fatalf("materializes = %v, want one player-domain materialize", materializes)
			}
			materialize, _ := w.Components.Materialize.GetComponent(materializes[0])
			if foot := component.Footprints[c.species]; materialize.Species != c.species || materialize.Footprint != foot {
				t.Fatalf("materialize = %+v, want species %d over its footprint %+v", materialize, c.species, foot)
			}
			if n := len(b.sent(event.EventSpiritSpawnRequest, 0)); n != 0 || w.Components.Drain.CountEntities() != 0 {
				t.Fatalf("%d spirits with %d drains left, want none and none", n, w.Components.Drain.CountEntities())
			}

			mark := len(b.delivered)
			for range materializeTicks() - 1 {
				b.tick()
			}
			if n := len(b.landings(c.species, mark)); n != 0 || w.Components.Materialize.CountEntities() != 1 {
				t.Fatalf("%d spawns before the materialize ended", n)
			}
			b.tick()
			spawns := b.landings(c.species, mark)
			if len(spawns) != 1 || spawns[0].Domain != core.DomainPlayer || w.Components.Materialize.CountEntities() != 0 {
				t.Fatalf("spawns = %+v as the materialize ended, want one crossing", spawns)
			}
			if x, y := landCell(spawns[0]); x != materialize.X || y != materialize.Y {
				t.Fatalf("the spawn names (%d, %d), off the materialize's header (%d, %d)", x, y, materialize.X, materialize.Y)
			}
		})
	}
}

// TestASweepWaitsForTheFusionIntoItsTargetInFlight: the quasar's materialize is
// the pending fusion, so a second sweep into the quasar is refused while it flies
// and admitted once it has landed; a drain pair's spirits, naming the swarm, hold
// back nothing
func TestASweepWaitsForTheFusionIntoItsTargetInFlight(t *testing.T) {
	b, cursor, drains := newFusionWorld(t)
	quasarMaterializes := func() (n int) {
		for _, e := range b.w.Components.Materialize.Entities() {
			if m, _ := b.w.Components.Materialize.GetComponent(e); m.Species == component.SpeciesQuasar {
				n++
			}
		}
		return n
	}
	request := quasarRequest(cursor)
	b.deliver(pairRequest(drains, event.FuseEffectNone))
	b.deliver(request)
	if n := quasarMaterializes(); n != 1 {
		t.Fatalf("%d quasar materializes beside a drain pair's spirits, want one", n)
	}
	b.tick()
	b.deliver(request)
	if n := quasarMaterializes(); n != 1 {
		t.Fatalf("%d quasar materializes after a second request in flight, want the first alone", n)
	}
	for range materializeTicks() {
		b.tick()
	}
	b.deliver(request)
	if n := quasarMaterializes(); n != 1 {
		t.Fatalf("%d quasar materializes after a request once the first landed, want a new one", n)
	}
}

// TestAFusionTakesAnySharedSpeciesForItsTarget: a request fuses its sources into any
// species with a body in the Shared domain, a materialize naming it over its
// footprint, a program's in its own style; none, the drain and an undeclared
// program are refused, and nothing is consumed
func TestAFusionTakesAnySharedSpeciesForItsTarget(t *testing.T) {
	warden := component.SpeciesDataFirst
	for _, c := range []struct {
		name   string
		target component.SpeciesType
		style  component.SpawnType // -1 refuses
	}{
		{"eye", component.SpeciesEye, component.SpawnTypeSwarm},
		{"kraken", component.SpeciesKraken, component.SpawnTypeSwarm},
		{"a program", warden, component.SpawnTypeDataFirst},
		{"none", component.SpeciesNone, -1},
		{"the drain", component.SpeciesDrain, -1},
		{"an undeclared program", warden + 1, -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, _, drains := newFusionWorld(t)
			w := b.w
			w.Resources.Behaviors = compileWarden(t).Prototypes()
			b.deliver(event.GameEvent{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
				Entities: drains[:], Target: c.target, Effect: event.FuseEffectMaterialize}})
			materializes := w.Components.Materialize.Entities()
			if c.style < 0 {
				if len(materializes) != 0 || w.Components.Drain.CountEntities() != 2 {
					t.Fatalf("%d materializes with %d drains left, want none and both", len(materializes), w.Components.Drain.CountEntities())
				}
				return
			}
			if len(materializes) != 1 || w.Components.Drain.CountEntities() != 0 {
				t.Fatalf("%d materializes with %d drains left, want one and none", len(materializes), w.Components.Drain.CountEntities())
			}
			m, _ := w.Components.Materialize.GetComponent(materializes[0])
			if m.Species != c.target || m.Footprint != w.Resources.Footprint(c.target) || m.Type != c.style {
				t.Fatalf("materialize %+v, want species %d over %+v in style %d", m, c.target, w.Resources.Footprint(c.target), c.style)
			}
		})
	}
}

// TestASweepTakesItsCountOfTheOldestDrains: a source row with a count takes that
// many of the cursor's drains, the oldest first, and leaves the rest standing
func TestASweepTakesItsCountOfTheOldestDrains(t *testing.T) {
	b, cursor, drains := newFusionWorld(t)
	w := b.w
	b.deliver(event.GameEvent{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
		Sources: []event.FuseSource{{Species: component.SpeciesDrain, Count: 1}}, Cursor: cursor,
		Target: component.SpeciesEye, Effect: event.FuseEffectMaterialize,
	}})
	if left := w.Components.Drain.Entities(); len(left) != 1 || left[0] != max(drains[0], drains[1]) {
		t.Fatalf("drains %v left of %v, want the younger alone", left, drains)
	}
}

// compileWarden is the warden program's set, one declared data species
func compileWarden(t *testing.T) *BehaviorSet {
	t.Helper()
	doc, err := toml.NewParser([]byte(wardenProgram)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
