package system

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func TestDrainOnlyReceivesKrakenSoftCollisions(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := ready(NewSoftCollisionSystem(w)).(*SoftCollisionSystem)

	for species := component.SpeciesType(1); species < component.SpeciesCount; species++ {
		if s.matrix[component.SpeciesDrain][species] != nil {
			t.Errorf("drain still pushes %s through soft collision", component.SpeciesNames[species])
		}
		if species != component.SpeciesKraken && s.matrix[species][component.SpeciesDrain] != nil {
			t.Errorf("%s still pushes drain through soft collision", component.SpeciesNames[species])
		}
	}
}

// A data species with mass pushes and flocks as the swarm does, one without takes
// no row, and every code species pair keeps the rule it had with no programs
func TestADataSpeciesTakesSoftCollisionRowsOnlyWithMass(t *testing.T) {
	plain := engine.NewWorld()
	engine.NewGameContextWithClock(plain, 40, 24, engine.NewManualClock())
	before := NewSoftCollisionSystem(plain).(*SoftCollisionSystem)

	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	heavy, weightless := component.SpeciesDataFirst, component.SpeciesDataFirst+1
	w.Resources.Behaviors = []component.BehaviorPrototype{
		{Name: "warden", Species: heavy, Footprint: vmath.Footprint{Width: 4, Height: 2}, Mass: 2},
		{Name: "wisp", Species: weightless, Footprint: vmath.CellFootprint},
	}
	s := NewSoftCollisionSystem(w).(*SoftCollisionSystem)

	for _, pair := range [][2]component.SpeciesType{
		{heavy, heavy}, {heavy, component.SpeciesSwarm}, {component.SpeciesSwarm, heavy},
		{heavy, component.SpeciesQuasar}, {component.SpeciesQuasar, heavy},
		{component.SpeciesStorm, heavy}, {component.SpeciesPylon, heavy}, {component.SpeciesKraken, heavy},
	} {
		if s.matrix[pair[0]][pair[1]] == nil {
			t.Errorf("species %d does not push species %d", pair[0], pair[1])
		}
	}
	if s.flockMatrix[heavy][heavy] == nil || s.flockMatrix[component.SpeciesSwarm][heavy] == nil || s.flockMatrix[heavy][component.SpeciesDrain] == nil {
		t.Error("a data species with mass is outside the shared flock")
	}
	for species := range component.SpeciesMax {
		if s.matrix[weightless][species] != nil || s.matrix[species][weightless] != nil ||
			s.flockMatrix[weightless][species] != nil || s.flockMatrix[species][weightless] != nil {
			t.Errorf("a data species without mass has a row against species %d", species)
		}
	}
	for a := range component.SpeciesCount {
		for b := range component.SpeciesCount {
			if !reflect.DeepEqual(s.matrix[a][b], before.matrix[a][b]) || !reflect.DeepEqual(s.flockMatrix[a][b], before.flockMatrix[a][b]) {
				t.Errorf("species %d against %d changed with a program declared", a, b)
			}
		}
	}
}

func TestDrainFlockingObservesSharedSpeciesOneWay(t *testing.T) {
	for _, tc := range []struct {
		name       string
		setSpecies func(*engine.World, core.Entity)
	}{
		{
			name: "swarm",
			setSpecies: func(w *engine.World, entity core.Entity) {
				w.Components.Swarm.SetComponent(entity, component.SwarmComponent{})
			},
		},
		{
			name: "quasar",
			setSpecies: func(w *engine.World, entity core.Entity) {
				w.Components.Quasar.SetComponent(entity, component.QuasarComponent{})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())

			shared := w.CreateEntity(core.DomainShared)
			drain := w.CreateEntity(core.DomainPlayer)
			tc.setSpecies(w, shared)
			w.Components.Drain.SetComponent(drain, component.DrainComponent{})
			w.Components.Combat.SetComponent(shared, component.CombatComponent{HitPoints: 1})
			w.Components.Combat.SetComponent(drain, component.CombatComponent{HitPoints: 1})
			w.Components.Kinetic.SetComponent(shared, component.KineticComponent{})
			w.Components.Kinetic.SetComponent(drain, component.KineticComponent{})
			w.Positions.SetPosition(shared, component.PositionComponent{X: 10, Y: 10})
			w.Positions.SetPosition(drain, component.PositionComponent{X: 11, Y: 10})

			s := ready(NewSoftCollisionSystem(w)).(*SoftCollisionSystem)
			s.rebuildCaches()
			s.processAllFlocking(1)

			drainKinetic, _ := w.Components.Kinetic.GetComponent(drain)
			if drainKinetic.VelX <= 0 || drainKinetic.VelY != 0 {
				t.Fatalf("drain velocity = (%v, %v), want repulsion away from %s", drainKinetic.VelX, drainKinetic.VelY, tc.name)
			}
			sharedKinetic, _ := w.Components.Kinetic.GetComponent(shared)
			if sharedKinetic.VelX != 0 || sharedKinetic.VelY != 0 {
				t.Fatalf("shared %s velocity = (%v, %v), want no drain influence", tc.name, sharedKinetic.VelX, sharedKinetic.VelY)
			}
		})
	}
}

// TestSoftCollisionBlocksHoldEveryBodyInReach: the blocks a species' reach crosses
// about a cell hold every body within it, on the map or off it, and the reach
// spans every cell an ellipse of its rules holds
func TestSoftCollisionBlocksHoldEveryBodyInReach(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	swarm := component.SpeciesSwarm
	for range 40 {
		w := engine.NewWorld()
		mw, mh := 20+rng.IntN(120), 12+rng.IntN(40)
		engine.NewGameContextWithClock(w, mw, mh, engine.NewManualClock())
		for range softBlocksMin + rng.IntN(200) {
			e := w.CreateEntity(core.DomainShared)
			w.Components.Swarm.SetComponent(e, component.SwarmComponent{})
			w.Positions.SetPosition(e, component.PositionComponent{X: rng.IntN(mw+16) - 8, Y: rng.IntN(mh+8) - 4})
		}
		s := ready(NewSoftCollisionSystem(w)).(*SoftCollisionSystem)
		s.rebuildCaches()
		bodies := s.caches[swarm]
		for _, reach := range [][2]int{s.pushReach[swarm], s.flockReach[swarm]} {
			for i := range bodies {
				src, found := &bodies[i], map[core.Entity]bool{}
				first, last, more := s.blocksNear(src.x, src.y, reach)
				for b := first; b <= last; b += s.blockCols {
					for _, e := range s.near[s.start[b]:s.start[b+more+1]] {
						found[e.entity] = true
					}
				}
				for _, tgt := range bodies {
					dx, dy := tgt.x-src.x, tgt.y-src.y
					if max(dx, -dx) <= reach[0] && max(dy, -dy) <= reach[1] && !found[tgt.entity] {
						t.Fatalf("on a %dx%d map the blocks %v cells about (%d,%d) cross leave out the body at (%d,%d)",
							mw, mh, reach, src.x, src.y, tgt.x, tgt.y)
					}
				}
			}
		}
		push, flock := s.matrix[swarm][swarm], s.flockMatrix[swarm][swarm]
		for dx := -20; dx <= 20; dx++ {
			for dy := -20; dy <= 20; dy++ {
				for _, c := range []struct {
					reach        [2]int
					invRx, invRy float64
				}{{s.pushReach[swarm], push.SourceInvRx, push.SourceInvRy}, {s.flockReach[swarm], flock.InvRxSq, flock.InvRySq}} {
					if vmath.EllipseContainsPoint(dx, dy, 0, 0, c.invRx, c.invRy) && (max(dx, -dx) > c.reach[0] || max(dy, -dy) > c.reach[1]) {
						t.Fatalf("a reach of %v cells leaves out (%d,%d), which its ellipse holds", c.reach, dx, dy)
					}
				}
			}
		}
	}
}

// softTarget stands a swarm at rest with its header at (x, y), open to every push
func softTarget(w *engine.World, x, y int) core.Entity {
	e := w.CreateEntity(core.DomainShared)
	w.Components.Swarm.SetComponent(e, component.SwarmComponent{})
	w.Components.Combat.SetComponent(e, component.CombatComponent{HitPoints: 1})
	w.Components.Kinetic.SetComponent(e, component.KineticComponent{})
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	return e
}

// TestASoftPushRunsNoneIntoASideItsTargetStandsAgainst: a pylon pushing a body up
// and right pushes it up alone against a wall at its right, and not at all from
// the map's corner it stands in, where the body takes no immunity and steers on
func TestASoftPushRunsNoneIntoASideItsTargetStandsAgainst(t *testing.T) {
	foot := component.Footprints[component.SpeciesSwarm]
	for _, tc := range []struct {
		name       string
		wall       bool
		corner     bool
		wantX      bool
		wantPushed bool
	}{
		{name: "open", wantX: true, wantPushed: true},
		{name: "wall", wall: true, wantPushed: true},
		{name: "corner", corner: true},
	} {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		cfg := w.Resources.Config
		x, y := 10, 10
		if tc.corner {
			x, y = cfg.MapWidth-foot.Width+foot.HeaderX, foot.HeaderY
		}
		body := softTarget(w, x, y)
		pylon := w.CreateEntity(core.DomainShared)
		w.Components.Pylon.SetComponent(pylon, component.PylonComponent{SpawnX: x - 1, SpawnY: y + 1})
		if tc.wall {
			wall := w.CreateEntity(core.DomainShared)
			w.Positions.SetPosition(wall, component.PositionComponent{X: x - foot.HeaderX + foot.Width, Y: y})
			w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockKinetic})
		}
		s := ready(NewSoftCollisionSystem(w)).(*SoftCollisionSystem)
		s.rebuildCaches()
		s.processAllCollisions()

		k, _ := w.Components.Kinetic.GetComponent(body)
		c, _ := w.Components.Combat.GetComponent(body)
		if pushed := c.RemainingKineticImmunity > 0; pushed != tc.wantPushed || tc.wantPushed && k.VelY >= 0 {
			t.Errorf("%s: velocity (%.2f, %.2f), immune %t; want pushed up %t", tc.name, k.VelX, k.VelY, pushed, tc.wantPushed)
		}
		if right := k.VelX > -k.VelY/2; right != tc.wantX {
			t.Errorf("%s: velocity (%.2f, %.2f) runs right %t, want %t", tc.name, k.VelX, k.VelY, right, tc.wantX)
		}
	}
}

// TestABodyOnItsSourcesCellPartsEveryWay: a body standing on its source's cell is
// pushed a way drawn anew each tick, to either side, not always the same one
func TestABodyOnItsSourcesCellPartsEveryWay(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	body := softTarget(w, 20, 10)
	pylon := w.CreateEntity(core.DomainShared)
	w.Components.Pylon.SetComponent(pylon, component.PylonComponent{SpawnX: 20, SpawnY: 10})
	s := ready(NewSoftCollisionSystem(w)).(*SoftCollisionSystem)
	var left, right int
	for range 16 {
		w.Resources.Game.State.IncrementGameTicks()
		w.Components.Kinetic.SetComponent(body, component.KineticComponent{})
		w.Components.Combat.SetComponent(body, component.CombatComponent{HitPoints: 1})
		s.rebuildCaches()
		s.processAllCollisions()
		if k, _ := w.Components.Kinetic.GetComponent(body); k.VelX < 0 {
			left++
		} else if k.VelX > 0 {
			right++
		}
	}
	if left == 0 || right == 0 {
		t.Fatalf("a body on its source's cell parted left %d and right %d times in 16 ticks, want both", left, right)
	}
}
