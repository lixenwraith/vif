package system

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// Navigation re-testing only the cells the wall log names stands where one
// re-reading every cell each tick does: the walls, both grids, the composite
// passability, every field, the phase it captures and how it steers, through
// walls spawned, destroyed and moved, a cursor moving and a cave scrolling
func TestNavigationOnTheWallLogStandsAsAWholeReread(t *testing.T) {
	for seed := range uint64(4) {
		rng := rand.New(rand.NewPCG(seed, 11))
		world := engine.NewWorld()
		engine.NewGameContextWithClock(world, 81, 30, engine.NewManualClock())
		walls := ready(NewWallSystem(world)).(*WallSystem)
		cfg := world.Resources.Config
		if seed%2 == 0 {
			walls.HandleEvent(event.GameEvent{Type: event.EventCaveStart, Payload: &event.CaveStartPayload{
				Seed: seed + 3, Speed: 8, MinGap: 9, MaxGap: 18, MinWall: 2, Wavelength: 24, Swing: 5, Detail: 1, Ramp: 30,
			}})
		}
		world.Resources.Time.DeltaTime = 50 * time.Millisecond
		ready(NewCursorSystem(world)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
			Payload: &event.CursorSpawnRequestPayload{X: 10, Y: cfg.MapHeight / 2, Control: uint8(component.ControlLocal)}})
		world.Resources.Event.Queue.Consume()
		var navs, wallEs []core.Entity
		for i := range 8 {
			x, y := 16+rng.IntN(cfg.MapWidth-24), 3+rng.IntN(cfg.MapHeight-6)
			e := newBody(world, x, y, 4, 2)
			if i%2 == 1 {
				e = world.CreateEntity(core.DomainShared)
				world.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
			}
			world.Components.Navigation.SetComponent(e, component.NavigationComponent{Width: 4 - 3*(i%2), Height: 2 - i%2})
			navs = append(navs, e)
		}
		spawn := func() {
			e := world.CreateEntity(core.DomainShared)
			world.Positions.SetPosition(e, component.PositionComponent{X: rng.IntN(cfg.MapWidth), Y: rng.IntN(cfg.MapHeight)})
			world.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockKinetic})
			wallEs = append(wallEs, e)
		}
		for range 80 {
			spawn()
		}
		logged := ready(NewNavigationSystem(world)).(*NavigationSystem)
		reread := ready(NewNavigationSystem(world)).(*NavigationSystem)
		cursor := world.Resources.Player.Slot(0)
		for tick := range 240 {
			switch r := rng.IntN(10); {
			case r < 3:
				for range 1 + rng.IntN(5) {
					spawn()
				}
			case r < 5 && len(wallEs) > 0:
				for range 1 + rng.IntN(5) {
					i := rng.IntN(len(wallEs))
					world.DestroyEntity(wallEs[i])
					wallEs = slices.Delete(wallEs, i, i+1)
				}
			case r < 7 && len(wallEs) > 0:
				for range 1 + rng.IntN(5) {
					e := wallEs[rng.IntN(len(wallEs))]
					if pos, ok := world.Positions.GetPosition(e); ok {
						world.Positions.Move(e, component.PositionComponent{
							X: min(max(pos.X+rng.IntN(3)-1, 0), cfg.MapWidth-1), Y: min(max(pos.Y+rng.IntN(3)-1, 0), cfg.MapHeight-1)})
					}
				}
			case r == 7:
				if pos, ok := world.Positions.GetPosition(cursor); ok {
					world.Positions.Move(cursor, component.PositionComponent{
						X: min(max(pos.X+rng.IntN(5)-2, 0), cfg.MapWidth-1), Y: min(max(pos.Y+rng.IntN(3)-1, 0), cfg.MapHeight-1)})
				}
			}
			walls.Update()
			world.Resources.Event.Queue.Consume()

			logged.Update()
			var steer []component.NavigationComponent
			for _, e := range navs {
				n, _ := world.Components.Navigation.GetComponent(e)
				steer = append(steer, n)
			}
			reread.wallsSeq = 0 // a lost log: every cell is read again
			reread.Update()
			for i, e := range navs {
				if n, _ := world.Components.Navigation.GetComponent(e); n != steer[i] {
					t.Fatalf("seed %d tick %d: navigator %d steers %+v on the log, %+v on a reread", seed, tick, i, steer[i], n)
				}
			}
			if !slices.Equal(logged.walls, reread.walls) || logged.grid != reread.grid || logged.comp != reread.comp ||
				!slices.Equal(logged.compositePassability.Valid, reread.compositePassability.Valid) {
				t.Fatalf("seed %d tick %d: walls, generations or passability differ", seed, tick)
			}
			for y := range cfg.MapHeight {
				for x := range cfg.MapWidth {
					if logged.pointGrid.Blocked(x, y) != reread.pointGrid.Blocked(x, y) ||
						logged.compositeGrid.Blocked(x, y) != reread.compositeGrid.Blocked(x, y) {
						t.Fatalf("seed %d tick %d: the grids differ at (%d,%d)", seed, tick, x, y)
					}
				}
			}
			a, _ := logged.SaveShared()
			b, _ := reread.SaveShared()
			if !bytes.Equal(a, b) {
				t.Fatalf("seed %d tick %d: captured phases differ\n  log:    %s\n  reread: %s", seed, tick, a, b)
			}
			if tick%8 != 0 {
				continue
			}
			for id, g := range logged.groups {
				for _, composite := range []bool{false, true} {
					f, o := logged.flow(g, composite), reread.flow(reread.groups[id], composite)
					for y := range cfg.MapHeight {
						for x := range cfg.MapWidth {
							if f.GetDirection(x, y) != o.GetDirection(x, y) || f.GetDistance(x, y) != o.GetDistance(x, y) {
								t.Fatalf("seed %d tick %d: group %d field (composite %t) differs at (%d,%d)", seed, tick, id, composite, x, y)
							}
						}
					}
				}
			}
		}
	}
}
