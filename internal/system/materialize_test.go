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
			for _, e := range w.Components.Materialize.Entities() {
				if m, _ := w.Components.Materialize.GetComponent(e); m.Footprint != tc.want || m.X != 30 || m.Y != 12 {
					t.Fatalf("materialize %+v, want %+v with its header on (30, 12)", m, tc.want)
				}
				return
			}
			t.Fatal("no materialize")
		})
	}
}
