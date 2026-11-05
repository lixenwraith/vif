package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestAMaterializeLastsItsRequestsDuration: a request's duration_ms times its
// materialize, which completes on the tick it ends; one of 0 or less takes its
// style's, a cell's and an area's alike
func TestAMaterializeLastsItsRequestsDuration(t *testing.T) {
	ticks := func(d time.Duration) int { return int(d / parameter.GameUpdateInterval) }
	nugget := component.MaterializeStyles[component.SpawnTypeNugget].Duration()
	swarm := component.MaterializeStyles[component.SpawnTypeSwarm].Duration()
	area := vmath.Footprint{Width: 4, Height: 2}
	for _, tc := range []struct {
		name    string
		payload *event.MaterializeRequestPayload
		want    int
	}{
		{"a cell at its style's", &event.MaterializeRequestPayload{X: 10, Y: 10, Type: component.SpawnTypeNugget}, ticks(nugget)},
		{"a cell at 150 ms", &event.MaterializeRequestPayload{X: 10, Y: 10, Type: component.SpawnTypeNugget, DurationMs: 150}, 3},
		{"an area at its style's", &event.MaterializeRequestPayload{X: 10, Y: 10, Footprint: area, Type: component.SpawnTypeSwarm}, ticks(swarm)},
		{"an area at 250 ms", &event.MaterializeRequestPayload{X: 10, Y: 10, Footprint: area, Type: component.SpawnTypeSwarm, DurationMs: 250}, 5},
		{"an area below zero", &event.MaterializeRequestPayload{X: 10, Y: 10, Footprint: area, Type: component.SpawnTypeSwarm, DurationMs: -250}, ticks(swarm)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _, _ := testCursorWorld(t)
			b := newMaterializeWorld(w)
			b.deliver(event.GameEvent{Type: event.EventMaterializeRequest, Domain: core.DomainShared, Payload: tc.payload})
			mark := len(b.delivered)
			for n := 1; n <= 2*ticks(swarm); n++ {
				b.tick()
				if len(b.sent(event.EventMaterializeComplete, mark)) > 0 {
					if n != tc.want {
						t.Fatalf("completed on tick %d, want %d", n, tc.want)
					}
					return
				}
			}
			t.Fatal("never completed")
		})
	}
}

// TestAMaterializeCoversTheFootprintItsRequestResolves: a request's own footprint,
// else its species', else one cell, with the header on the cell it names
func TestAMaterializeCoversTheFootprintItsRequestResolves(t *testing.T) {
	big := vmath.Footprint{Width: 9, Height: 5, HeaderX: 4, HeaderY: 2}
	for _, tc := range []struct {
		name    string
		payload *event.MaterializeRequestPayload
		want    vmath.Footprint
	}{
		{"its own", &event.MaterializeRequestPayload{X: 30, Y: 12, Footprint: big, Species: component.SpeciesSwarm}, big},
		{"its species'", &event.MaterializeRequestPayload{X: 30, Y: 12, Species: component.SpeciesKraken}, component.Footprints[component.SpeciesKraken]},
		{"one cell", &event.MaterializeRequestPayload{X: 30, Y: 12, Footprint: vmath.Footprint{Width: 3}}, vmath.CellFootprint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _, _ := testCursorWorld(t)
			newMaterializeWorld(w).deliver(event.GameEvent{Type: event.EventMaterializeRequest, Domain: core.DomainShared, Payload: tc.payload})
			materializes := w.Components.Materialize.Entities()
			if len(materializes) != 1 {
				t.Fatalf("%d materializes, want one", len(materializes))
			}
			if m, _ := w.Components.Materialize.GetComponent(materializes[0]); m.Footprint != tc.want || m.X != 30 || m.Y != 12 {
				t.Fatalf("materialize %+v, want %+v with its header on (30, 12)", m, tc.want)
			}
		})
	}
}

// TestACarrierLandsItsSpeciesInItsOwnDomain: a materialize or spirit naming a
// species asks for it as it ends with its header on the carrier's cell, Shared where
// every instance ran the carrier and stamped player, a crossing, where this instance
// alone did; one naming none asks for nothing
func TestACarrierLandsItsSpeciesInItsOwnDomain(t *testing.T) {
	for _, tc := range []struct {
		name    string
		carrier func(core.Domain, component.SpeciesType) event.GameEvent
	}{
		{"materialize", func(d core.Domain, species component.SpeciesType) event.GameEvent {
			return event.GameEvent{Type: event.EventMaterializeRequest, Domain: d,
				Payload: &event.MaterializeRequestPayload{X: 30, Y: 12, Type: component.SpawnTypeSwarm, Species: species}}
		}},
		{"spirit", func(d core.Domain, species component.SpeciesType) event.GameEvent {
			return event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: d,
				Payload: &event.SpiritSpawnRequestPayload{StartX: 2, StartY: 2, TargetX: 30, TargetY: 12, Char: 'x',
					Species: species, LandX: 30, LandY: 12}}
		}},
	} {
		for _, domain := range []core.Domain{core.DomainShared, core.DomainPlayer} {
			t.Run(tc.name+" "+domain.String(), func(t *testing.T) {
				w, _, _ := testCursorWorld(t)
				spirits := ready(NewSpiritSystem(w))
				b := newMaterializeWorld(w, spirits)
				b.spirits = spirits
				b.deliver(tc.carrier(domain, component.SpeciesQuasar), tc.carrier(domain, component.SpeciesNone))
				for range 2 * materializeTicks() {
					b.tick()
				}
				lands := b.sent(event.EventSpeciesLandRequest, 0)
				if len(lands) != 1 || lands[0].Domain != domain {
					t.Fatalf("land requests = %+v, want one in the %s domain", lands, domain)
				}
				if p, _ := lands[0].Payload.(*event.SpeciesLandRequestPayload); p == nil || *p != (event.SpeciesLandRequestPayload{Species: component.SpeciesQuasar, X: 30, Y: 12}) {
					t.Fatalf("the land request carries %+v, want the quasar headed at (30, 12)", lands[0].Payload)
				}
			})
		}
	}
}
