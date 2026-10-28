package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestAMaterializeLastsItsRequestsDuration: a request's duration_ms times its
// materialize, which completes on the tick it ends; one of 0 or less takes its
// style's, a cell's and an area's alike
func TestAMaterializeLastsItsRequestsDuration(t *testing.T) {
	ticks := func(d time.Duration) int { return int(d / parameter.GameUpdateInterval) }
	nugget := component.MaterializeStyles[component.SpawnTypeNugget].Duration()
	swarm := component.MaterializeStyles[component.SpawnTypeSwarm].Duration()
	for _, tc := range []struct {
		name    string
		payload any
		want    int
	}{
		{"a cell at its style's", &event.MaterializeRequestPayload{X: 10, Y: 10, Type: component.SpawnTypeNugget}, ticks(nugget)},
		{"a cell at 150 ms", &event.MaterializeRequestPayload{X: 10, Y: 10, Type: component.SpawnTypeNugget, DurationMs: 150}, 3},
		{"an area at its style's", &event.MaterializeAreaRequestPayload{X: 10, Y: 10, AreaWidth: 4, AreaHeight: 2, Type: component.SpawnTypeSwarm}, ticks(swarm)},
		{"an area at 250 ms", &event.MaterializeAreaRequestPayload{X: 10, Y: 10, AreaWidth: 4, AreaHeight: 2, Type: component.SpawnTypeSwarm, DurationMs: 250}, 5},
		{"an area below zero", &event.MaterializeAreaRequestPayload{X: 10, Y: 10, AreaWidth: 4, AreaHeight: 2, Type: component.SpawnTypeSwarm, DurationMs: -250}, ticks(swarm)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _, _ := testCursorWorld(t)
			b := newMaterializeWorld(w)
			typ := event.EventMaterializeRequest
			if _, ok := tc.payload.(*event.MaterializeAreaRequestPayload); ok {
				typ = event.EventMaterializeAreaRequest
			}
			b.deliver(event.GameEvent{Type: typ, Domain: core.DomainShared, Payload: tc.payload})
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
