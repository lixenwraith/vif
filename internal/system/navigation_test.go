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
	"github.com/lixenwraith/vif/pkg/navigation"
)

// Navigation re-testing only the cells the wall log names stands where one
// re-reading every cell each tick does: the walls, every tier's passability and
// grid, every field, the phase it captures and how it steers, through walls
// spawned, destroyed and moved, a cursor moving and a cave scrolling
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
			if !slices.Equal(logged.walls.Words, reread.walls.Words) || logged.gen != reread.gen || len(logged.tiers) != len(reread.tiers) {
				t.Fatalf("seed %d tick %d: walls, generations or tiers differ", seed, tick)
			}
			for i, lt := range logged.tiers {
				rt := reread.tiers[i]
				if lt.foot != rt.foot || lt.gen != rt.gen || lt.pass != nil && !slices.Equal(lt.pass.Bits().Words, rt.pass.Bits().Words) {
					t.Fatalf("seed %d tick %d: tier %d differs", seed, tick, i)
				}
				for y := range cfg.MapHeight {
					for x := range cfg.MapWidth {
						if lt.grid.Blocked(x, y) != rt.grid.Blocked(x, y) {
							t.Fatalf("seed %d tick %d: tier %d's grid differs at (%d,%d)", seed, tick, i, x, y)
						}
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
				for tier := range logged.tiers {
					f, o := logged.flow(g, tier), reread.flow(reread.groups[id], tier)
					for y := range cfg.MapHeight {
						for x := range cfg.MapWidth {
							if f.GetDirection(x, y) != o.GetDirection(x, y) || f.GetDistance(x, y) != o.GetDistance(x, y) {
								t.Fatalf("seed %d tick %d: group %d field on tier %d differs at (%d,%d)", seed, tick, id, tier, x, y)
							}
						}
					}
				}
			}
		}
	}
}

// A body the eye's footprint does not hold steers by a tier of its own, which its
// field and a route graph requested for it both search, so neither leads through a
// gap only the eye fits; an install derives the tier from the capture naming it
func TestABodyTheEyeDoesNotHoldRoutesByATierOfItsOwn(t *testing.T) {
	world := engine.NewWorld()
	engine.NewGameContextWithClock(world, 60, 30, engine.NewManualClock())
	ready(NewCursorSystem(world)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{X: 50, Y: 7, Control: uint8(component.ControlLocal)}})
	world.Resources.Event.Queue.Consume()
	narrow, wide := func(y int) bool { return y >= 5 && y <= 8 }, func(y int) bool { return y >= 18 && y <= 26 }
	for y := range 30 {
		if !narrow(y) && !wide(y) {
			wall := world.CreateEntity(core.DomainShared)
			world.Positions.SetPosition(wall, component.PositionComponent{X: 30, Y: y})
			world.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockKinetic})
		}
	}
	big := navigation.Footprint{Width: 7, Height: 5, HeaderX: 3, HeaderY: 2}
	body := world.CreateEntity(core.DomainShared)
	world.Positions.SetPosition(body, component.PositionComponent{X: 12, Y: 7})
	world.Components.Navigation.SetComponent(body, component.NavigationComponent{
		Width: big.Width, Height: big.Height, HeaderX: big.HeaderX, HeaderY: big.HeaderY})
	nav := ready(NewNavigationSystem(world)).(*NavigationSystem)
	nav.Update()

	tier := nav.tierOf(big)
	if tier < 2 {
		t.Fatalf("the 7x5 body rides tier %d, want one of its own", tier)
	}
	own, eye := nav.flow(nav.groups[0], tier), nav.flow(nav.groups[0], 1)
	if own.GetDistance(30, 7) >= 0 || own.GetDistance(30, 22) < 0 || eye.GetDistance(30, 7) < 0 {
		t.Fatal("the body's field leads through the gap only the eye fits, or not through the wide one")
	}
	nav.handleRouteGraphRequest(&event.RouteGraphRequestPayload{RouteGraphID: 9, SourceX: 12, SourceY: 7,
		Width: big.Width, Height: big.Height, HeaderX: big.HeaderX, HeaderY: big.HeaderY})
	rg := world.Resources.RouteGraph.Get(9)
	if rg == nil || rg.Footprint != big || !nav.routeGraphFresh(rg, 0) {
		t.Fatal("no fresh route graph for the body the request named")
	}
	for _, r := range rg.Routes {
		for y := range 30 {
			if narrow(y) && r.Field.GetDistance(30, y) >= 0 {
				t.Fatalf("route %d leads through the gap only the eye fits", r.ID)
			}
		}
	}

	phase, _ := nav.SaveShared()
	joiner := ready(NewNavigationSystem(world)).(*NavigationSystem)
	if err := joiner.LoadShared(phase); err != nil {
		t.Fatal(err)
	}
	again, _ := joiner.SaveShared()
	if !bytes.Equal(phase, again) || joiner.tierOf(big) != tier || !slices.Equal(joiner.tiers[tier].pass.Bits().Words, nav.tiers[tier].pass.Bits().Words) {
		t.Fatal("an install did not derive the tier its capture names")
	}
}
