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

// TestASweepNamingNoCursorTakesTheFirstParticipantsDrains: two guests of a host that
// drives no cursor take a Shared sweep naming none on one instance between them, the
// lowest slot's that plays: the first's, the second's while the first is away, and
// the first's again once it is back
func TestASweepNamingNoCursorTakesTheFirstParticipantsDrains(t *testing.T) {
	var guests [2]*materializeWorld
	for i := range guests {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		w.AdoptSession(engine.SessionState{Live: true, Participant: uint32(i + 2), Authority: engine.CoordinatorParticipant})
		guests[i] = newMaterializeWorld(w, ready(NewFuseSystem(w)), ready(NewCursorSystem(w)))
		w.Resources.Player.SetLocal(uint8(i))
	}
	// arrive lands slot's participant, a Wraith's return, on both instances at once
	arrive := func(slot uint8) {
		for i, g := range guests {
			control := component.ControlRemote
			if i == int(slot) {
				control = component.ControlLocal
			}
			g.deliver(event.GameEvent{Type: event.EventCursorSpawnRequest, Payload: &event.CursorSpawnRequestPayload{
				X: 5 + 10*int(slot), Y: 5, Slot: slot, Control: uint8(control), PeerID: uint32(slot) + 2}})
		}
	}
	arrive(0)
	arrive(1)

	for _, step := range []struct {
		name   string
		roster func()
		sweeps int // the guest whose drains go
	}{
		{"both playing", func() {}, 0},
		{"the first away", func() {
			// Guest 2's own world takes the departure too, which a live session never
			// shows the departed instance: a Wraith is the first participant nowhere
			for _, g := range guests {
				g.deliver(event.GameEvent{Type: event.EventCursorWraithRequest, Payload: &event.CursorWraithRequestPayload{Slot: 0}})
			}
		}, 1},
		{"the first back", func() { arrive(0) }, 0},
	} {
		step.roster()
		for _, g := range guests {
			// Each guest's own drains, as its DrainSystem raises them from its heat:
			// no other instance holds them, so only the elected one can sweep
			for _, x := range []int{20, 24} {
				d := g.w.CreateEntity(core.DomainPlayer)
				g.w.Positions.SetPosition(d, component.PositionComponent{X: x, Y: 12})
				g.w.Components.Drain.SetComponent(d, component.DrainComponent{})
			}
			g.deliver(event.GameEvent{Type: event.EventFuseRequest, Domain: core.DomainShared, Payload: &event.FuseRequestPayload{
				Sources: []event.FuseSource{{Species: component.SpeciesDrain}}, Target: component.SpeciesQuasar,
				Effect: event.FuseEffectMaterialize}})
		}
		for i, g := range guests {
			if swept := g.w.Components.Drain.CountEntities() == 0; swept != (i == step.sweeps) {
				t.Fatalf("%s: guest %d swept its drains %t, want guest %d's alone", step.name, i+2, swept, step.sweeps+2)
			}
			for range materializeTicks() {
				g.tick()
			}
			for _, d := range g.w.Components.Drain.Entities() {
				g.w.DestroyEntity(d)
			}
		}
	}
}

// standHeader stands a composite's bare phantom head: a Shared header of behavior
// b and type typ at (x, y), under parent unless that is 0
func standHeader(w *engine.World, b component.Behavior, typ component.CompositeType, parent core.Entity, x, y int) core.Entity {
	e := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.Header.SetComponent(e, component.HeaderComponent{Behavior: b, Type: typ, ParentHeader: parent})
	return e
}

// TestASharedFusionTakesItsCompositesOnEveryInstance: a Shared request, which every
// instance derives, takes the unit composites at their root its rows sweep, the
// oldest first, unrewarded, and lands the target at their centroid by a Shared
// spirit on each instance alike, whoever its cursor elects and whatever else flies;
// the elected instance alone sweeps its drains, which fly in as visuals
func TestASharedFusionTakesItsCompositesOnEveryInstance(t *testing.T) {
	for _, c := range []struct {
		name            string
		elected, flying bool
	}{{"elected", true, false}, {"another's cursor", false, false}, {"a fusion in flight", true, true}} {
		t.Run(c.name, func(t *testing.T) {
			b, cursor, _ := newFusionWorld(t)
			w := b.w
			w.Resources.Behaviors = compileWarden(t).Prototypes()
			b.handlers = append(b.handlers, ready(NewCompositeSystem(w)).(event.Handler))
			// The oldest swarm is dying: its kill is its own system's to report
			dying := standHeader(w, component.BehaviorSwarm, component.CompositeTypeUnit, 0, 14, 14)
			w.Components.Combat.SetComponent(dying, component.CombatComponent{})
			swarms := []core.Entity{
				standHeader(w, component.BehaviorSwarm, component.CompositeTypeUnit, 0, 8, 6),
				standHeader(w, component.BehaviorSwarm, component.CompositeTypeUnit, 0, 32, 6),
			}
			warden := standHeader(w, component.BehaviorData, component.CompositeTypeUnit, 0, 20, 18)
			w.Components.Behavior.SetComponent(warden, component.BehaviorComponent{})
			kept := []core.Entity{
				swarms[1], dying,
				standHeader(w, component.BehaviorSnake, component.CompositeTypeUnit, swarms[1], 10, 10),
				standHeader(w, component.BehaviorPylon, component.CompositeTypeAblative, 0, 12, 12),
			}
			if !c.elected {
				cursor = spawnRemoteCursor(t, w, 2, 1, 1, 7)
			}
			if c.flying {
				b.deliver(quasarRequest(cursor))
			}
			mark, materializes := len(b.delivered), w.Components.Materialize.CountEntities()

			b.deliver(event.GameEvent{Type: event.EventFuseRequest, Domain: core.DomainShared, Payload: &event.FuseRequestPayload{
				Sources: []event.FuseSource{
					{Species: component.SpeciesSwarm, Count: 1}, {Species: component.SpeciesDrain}, {Species: component.SpeciesDataFirst},
					{Species: component.SpeciesSnake}, {Species: component.SpeciesPylon},
				},
				Cursor: cursor, Target: component.SpeciesQuasar,
			}})
			for _, e := range []core.Entity{swarms[0], warden} {
				if w.Components.Header.HasEntity(e) {
					t.Fatalf("composite %v stands after the fusion", e)
				}
			}
			for _, e := range kept {
				if !w.Components.Header.HasEntity(e) {
					t.Fatalf("composite %v went, which the request does not take", e)
				}
			}
			for _, ev := range b.sent(event.EventCompositeDestroyRequest, 0) {
				if ev.Domain != core.DomainShared {
					t.Fatalf("a composite went by a %v request, want Shared", ev.Domain)
				}
			}
			for _, ev := range b.sent(event.EventSpeciesKilled, 0) {
				if p := ev.Payload.(*event.SpeciesKilledPayload); p.Species != component.SpeciesDrain {
					t.Fatalf("a kill of species %d reported, want the drains' alone", p.Species)
				}
			}

			spirits := b.sent(event.EventSpiritSpawnRequest, mark)
			wantSpirits, wantDrains := 2, 2
			switch {
			case c.flying:
				wantDrains = 0 // the fusion in flight took them
			case c.elected:
				wantSpirits, wantDrains = 4, 0
			}
			if len(spirits) != wantSpirits || w.Components.Drain.CountEntities() != wantDrains {
				t.Fatalf("%d spirits with %d drains left, want %d and %d",
					len(spirits), w.Components.Drain.CountEntities(), wantSpirits, wantDrains)
			}
			first := spirits[0].Payload.(*event.SpiritSpawnRequestPayload)
			if first.StartX != 8 || first.StartY != 6 {
				t.Fatalf("the first spirit flies from (%d, %d), want the swarm's header (8, 6)", first.StartX, first.StartY)
			}
			for i, ev := range spirits {
				p := ev.Payload.(*event.SpiritSpawnRequestPayload)
				carries := i == 0
				if (p.Species == component.SpeciesQuasar) != carries || (ev.Domain == core.DomainShared) != carries ||
					(carries && (p.LandX != 14 || p.LandY != 12)) {
					t.Fatalf("spirit %d, %v, names species %d landing (%d, %d), want the first alone Shared, at the composites' centroid (14, 12)",
						i, ev.Domain, p.Species, p.LandX, p.LandY)
				}
			}
			if n := w.Components.Materialize.CountEntities(); n != materializes {
				t.Fatalf("%d materializes beside the spirits, want %d", n, materializes)
			}

			for range int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval) {
				b.tick()
			}
			var shared []event.GameEvent
			for _, ev := range b.landings(component.SpeciesQuasar, mark) {
				if ev.Domain == core.DomainShared {
					shared = append(shared, ev)
				}
			}
			if len(shared) != 1 {
				t.Fatalf("%d Shared quasar landings as the spirits arrived, want one", len(shared))
			}
			if x, y := landCell(shared[0]); x != 14 || y != 12 {
				t.Fatalf("the quasar lands at (%d, %d), want the composites' centroid (14, 12)", x, y)
			}
		})
	}
}

// TestARequestFailsWholeOnASourceItCannotTake: a participant's request names drains
// alone, a Shared one no drain by entity, whose id no other instance shares, and
// neither names an entity twice; each fails whole, consuming nothing
func TestARequestFailsWholeOnASourceItCannotTake(t *testing.T) {
	b, cursor, drains := newFusionWorld(t)
	w := b.w
	swarm := standHeader(w, component.BehaviorSwarm, component.CompositeTypeUnit, 0, 8, 6)
	for _, ev := range []event.GameEvent{
		{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
			Sources: []event.FuseSource{{Species: component.SpeciesDrain}, {Species: component.SpeciesSwarm}}, Cursor: cursor, Target: component.SpeciesQuasar}},
		{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
			Entities: []core.Entity{drains[0], swarm}, Target: component.SpeciesQuasar}},
		{Type: event.EventFuseRequest, Domain: core.DomainShared, Payload: &event.FuseRequestPayload{
			Entities: []core.Entity{swarm, drains[0]}, Cursor: cursor, Target: component.SpeciesQuasar}},
		{Type: event.EventFuseRequest, Domain: core.DomainPlayer, Payload: &event.FuseRequestPayload{
			Entities: []core.Entity{drains[0], drains[0]}, Target: component.SpeciesSwarm}},
		{Type: event.EventFuseRequest, Domain: core.DomainShared, Payload: &event.FuseRequestPayload{
			Entities: []core.Entity{swarm, swarm}, Target: component.SpeciesQuasar}},
	} {
		b.deliver(ev)
	}
	if n := w.Resources.Status.Ints.Get("fuse.spawn_failures").Load(); n != 5 {
		t.Fatalf("fuse.spawn_failures = %d, want each of the five requests", n)
	}
	if !w.Components.Header.HasEntity(swarm) || w.Components.Drain.CountEntities() != 2 ||
		len(b.sent(event.EventSpiritSpawnRequest, 0))+len(b.sent(event.EventCompositeDestroyRequest, 0)) != 0 {
		t.Fatal("a refused request consumed a source")
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
