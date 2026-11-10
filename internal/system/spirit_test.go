package system

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// spiritWorld is a bare world whose spirit system takes requests by hand
func spiritWorld() (*engine.World, *timerTick, *SpiritSystem) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	return w, newTimerTick(w), ready(NewSpiritSystem(w)).(*SpiritSystem)
}

// flights is each flight's part count, in store order
func flights(w *engine.World) (parts []int) {
	w.Components.Spirit.Each(func(_ core.Entity, s *component.SpiritComponent) bool {
		parts = append(parts, len(s.Parts))
		return true
	})
	return parts
}

// TestAFlightLandsWhatItNamesAsItEnds: a flight naming a species sends one land
// request with the species and its header cell on the tick its deadline runs out,
// the tick every flight sent with it leaves; one naming none sends nothing
func TestAFlightLandsWhatItNamesAsItEnds(t *testing.T) {
	w, tick, spirits := spiritWorld()
	spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: core.DomainPlayer,
		Payload: &event.SpiritSpawnRequestPayload{StartX: 2, StartY: 2, TargetX: 30, TargetY: 10, Char: 'x',
			Species: component.SpeciesSwarm, LandX: 28, LandY: 9}})
	spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: core.DomainPlayer,
		Payload: &event.SpiritSpawnRequestPayload{StartX: 60, StartY: 2, TargetX: 31, TargetY: 10, Char: 'x'}})

	var arrivals []event.GameEvent
	arrivedOn, leftOn := 0, 0
	for n := 1; n <= 20; n++ {
		tick.open()
		spirits.Update()
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type == event.EventSpeciesLandRequest {
				arrivals = append(arrivals, ev)
				arrivedOn = n
			}
		}
		if leftOn == 0 && w.Components.Spirit.CountEntities() == 0 {
			leftOn = n
		}
	}
	if len(arrivals) != 1 {
		t.Fatalf("land requests = %+v, want one", arrivals)
	}
	if p, _ := arrivals[0].Payload.(*event.SpeciesLandRequestPayload); p == nil || *p != (event.SpeciesLandRequestPayload{Species: component.SpeciesSwarm, X: 28, Y: 9}) {
		t.Fatalf("the land request carries %+v, want the swarm headed at (28, 9)", arrivals[0].Payload)
	}
	if want := int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval); arrivedOn != want || leftOn != want {
		t.Fatalf("arrived on tick %d and the flights left on %d, want both on %d", arrivedOn, leftOn, want)
	}
	if n := w.Components.Timer.CountEntities(); n != 0 {
		t.Fatalf("%d timers outlived their flights", n)
	}
}

// TestATicksPartsRideOneFlight: the parts one tick sends that travel alike are one
// flight and the next tick's another, a part naming a species, another color or
// another domain flies apart, and a flight that ended lends its storage to the next
func TestATicksPartsRideOneFlight(t *testing.T) {
	w, tick, spirits := spiritWorld()
	fly := func(domain core.Domain, c component.SpiritColor, species component.SpeciesType) {
		spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: domain,
			Payload: &event.SpiritSpawnRequestPayload{StartX: 2, StartY: 2, TargetX: 30, TargetY: 10, Char: 'x',
				BaseColor: c, Species: species}})
	}
	update := func() {
		tick.open()
		spirits.Update()
		w.Resources.Event.Queue.Consume()
	}

	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesNone)
	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesSwarm)
	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesNone)
	fly(core.DomainPlayer, component.SpiritRed, component.SpeciesNone)
	fly(core.DomainShared, component.SpiritCyan, component.SpeciesNone)
	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesNone)
	update()
	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesNone)
	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesNone)
	if got, want := flights(w), []int{3, 1, 1, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("flights hold %v parts, want %v: a tick's cyan three, the carrier, the red, the Shared, then the next tick's two", got, want)
	}
	if e := w.Components.Spirit.Entities()[3]; e.Domain() != core.DomainShared {
		t.Fatalf("the Shared request's flight is %v", e.Domain())
	}
	var storage []*component.SpiritPart
	w.Components.Spirit.Each(func(_ core.Entity, s *component.SpiritComponent) bool {
		storage = append(storage, unsafe.SliceData(s.Parts))
		return true
	})

	for range int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval) {
		update()
	}
	if got := flights(w); len(got) != 0 {
		t.Fatalf("flights of %v parts outlived their deadlines", got)
	}
	fly(core.DomainPlayer, component.SpiritCyan, component.SpeciesNone)
	again, _ := w.Components.Spirit.GetPtr(w.Components.Spirit.Entities()[0])
	if !slices.Contains(storage, unsafe.SliceData(again.Parts)) {
		t.Fatal("a new flight did not take the storage of one that ended")
	}
}

// TestAScatterThrowsABodyOutFromItsMiddle: a body's glyphs leave their own cells in
// their own colors, each away from the body's middle and within the reach its size
// gives, a cell with no glyph stays behind, and the bodies of one tick are one
// player flight that lands nothing and ends with the scatter's duration
func TestAScatterThrowsABodyOutFromItsMiddle(t *testing.T) {
	w, tick, spirits := spiritWorld()
	red := color.RGB{R: 200}
	scatter := func(left, top int) {
		batch := event.EffectBatchPool.Acquire()
		for dy := range 2 {
			for dx := range 4 {
				batch.Entries = append(batch.Entries, component.EffectCell{X: left + dx, Y: top + dy, Rune: rune('a' + dx), Fg: red})
			}
		}
		batch.Entries = append(batch.Entries, component.EffectCell{X: left, Y: top, Bg: red, Solid: true})
		spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritScatterRequest, Domain: core.DomainPlayer, Payload: batch})
	}
	scatter(10, 5)
	scatter(40, 12)

	entities := w.Components.Spirit.Entities()
	if len(entities) != 1 || entities[0].Domain() != core.DomainPlayer {
		t.Fatalf("flights = %v, want the two bodies on one player flight", entities)
	}
	flight, _ := w.Components.Spirit.GetPtr(entities[0])
	if flight.Pattern != component.SpiritScatter || len(flight.Parts) != 16 {
		t.Fatalf("the flight is pattern %d with %d parts, want a scatter of the 16 glyphs", flight.Pattern, len(flight.Parts))
	}

	aspect := w.Resources.Config.CellAspect
	reach := visual.SpiritScatterReach + visual.SpiritScatterGrowth*vmath.Magnitude(2, aspect.Circular(1))
	for i, part := range flight.Parts {
		left, top := 10, 5
		if i >= 8 {
			left, top = 40, 12
		}
		cell := vmath.Point{X: left + i%4, Y: top + i%8/4}
		if fromX, fromY := cell.Center(); part.FromX != fromX || part.FromY != fromY || part.Rune != rune('a'+i%4) || part.Color != red {
			t.Fatalf("part %d is %+v, want the glyph of cell %+v in red", i, part, cell)
		}
		outX, outY := part.FromX-float64(left+2), aspect.Circular(part.FromY-float64(top+1))
		wayX, wayY := part.ToX-part.FromX, aspect.Circular(part.ToY-part.FromY)
		if throw := vmath.Magnitude(wayX, wayY); vmath.DotProduct(outX, outY, wayX, wayY) <= 0 ||
			throw < reach-1e-9 || throw > reach+visual.SpiritScatterSpread+1e-9 {
			t.Fatalf("part %d of cell %+v flies (%.2f, %.2f), %.2f cells: want away from the middle and %.2f to %.2f cells",
				i, cell, wayX, wayY, throw, reach, reach+visual.SpiritScatterSpread)
		}
	}

	leftOn := 0
	for n := 1; n <= 20 && leftOn == 0; n++ {
		tick.open()
		spirits.Update()
		if w.Components.Spirit.CountEntities() == 0 {
			leftOn = n
		}
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type == event.EventSpeciesLandRequest {
				t.Fatal("a scatter landed a species")
			}
		}
	}
	if want := int(parameter.SpiritScatterDuration / parameter.GameUpdateInterval); leftOn != want {
		t.Fatalf("the scatter left on tick %d, want %d", leftOn, want)
	}
}
