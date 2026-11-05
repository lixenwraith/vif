package system

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// TestASpeciesAskedForAtAMapCornerStandsWholeInsideIt: a spawn request names the
// header's cell blind to the footprint around it, so a species asked for at either
// corner stands every cell of its body inside the map, none past an edge
func TestASpeciesAskedForAtAMapCornerStandsWholeInsideIt(t *testing.T) {
	const width, height = 80, 40
	for _, kind := range []struct {
		name    string
		system  func(*engine.World) engine.System
		request func(x, y int) event.GameEvent
	}{
		{"swarm", NewSwarmSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventSwarmSpawnRequest, Payload: &event.SwarmSpawnRequestPayload{X: x, Y: y}}
		}},
		{"quasar", NewQuasarSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventQuasarSpawnRequest, Payload: &event.QuasarSpawnRequestPayload{X: x, Y: y}}
		}},
		{"eye", NewEyeSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventEyeSpawnRequest, Payload: &event.EyeSpawnRequestPayload{X: x, Y: y, RouteID: -1}}
		}},
		{"snake", NewSnakeSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventSnakeSpawnRequest, Payload: &event.SnakeSpawnRequestPayload{X: x, Y: y}}
		}},
	} {
		for _, corner := range [][2]int{{0, 0}, {width - 1, height - 1}} {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, width, height, engine.NewManualClock())
			ready(kind.system(w)).(event.Handler).HandleEvent(kind.request(corner[0], corner[1]))
			if w.Positions.CountEntities() == 0 {
				t.Fatalf("%s asked for at %v: nothing stood", kind.name, corner)
			}
			for _, e := range w.Positions.Entities() {
				if pos, _ := w.Positions.GetPosition(e); w.Positions.IsOutOfBounds(pos.X, pos.Y) {
					t.Fatalf("%s asked for at %v: a cell stands at (%d, %d), past the %dx%d map", kind.name, corner, pos.X, pos.Y, width, height)
				}
			}
		}
	}
}

// TestALandRequestStandsTheCodeSpeciesItNames: every Shared code species' own system
// answers a land request naming it as its spawn request at that request's defaults,
// so a fusion or a program lands any of them; one naming another species stands
// nothing
func TestALandRequestStandsTheCodeSpeciesItNames(t *testing.T) {
	systems := map[component.SpeciesType]func(*engine.World) engine.System{
		component.SpeciesSwarm: NewSwarmSystem, component.SpeciesQuasar: NewQuasarSystem,
		component.SpeciesStorm: NewStormSystem, component.SpeciesPylon: NewPylonSystem,
		component.SpeciesSnake: NewSnakeSystem, component.SpeciesEye: NewEyeSystem,
		component.SpeciesTower: NewTowerSystem, component.SpeciesKraken: NewKrakenSystem,
	}
	for species := component.SpeciesNone + 1; species < component.SpeciesCount; species++ {
		if component.SpeciesDomains[species] != core.DomainShared {
			continue
		}
		build, ok := systems[species]
		if !ok {
			t.Fatalf("%s is a Shared species no case stands", component.SpeciesNames[species])
		}
		t.Run(component.SpeciesNames[species], func(t *testing.T) {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 120, 60, engine.NewManualClock())
			s := ready(build(w)).(event.Handler)
			land := func(named component.SpeciesType) (created []component.SpeciesType) {
				// Delivered as the router does, to a system that takes it
				if slices.Contains(s.EventTypes(), event.EventSpeciesLandRequest) {
					s.HandleEvent(event.GameEvent{Type: event.EventSpeciesLandRequest,
						Payload: &event.SpeciesLandRequestPayload{Species: named, X: 60, Y: 30}})
				}
				for _, ev := range w.Resources.Event.Queue.Consume() {
					if p, ok := ev.Payload.(*event.SpeciesCreatedPayload); ok {
						created = append(created, p.Species)
					}
				}
				return created
			}
			if created := land(component.SpeciesDrain); len(created) != 0 {
				t.Fatalf("a land request naming the drain stood %v", created)
			}
			if created := land(species); len(created) != 1 || created[0] != species {
				t.Fatalf("a land request naming it stood %v", created)
			}
		})
	}
}
