package system

import (
	"reflect"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
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
		{Name: "warden", Species: heavy, Width: 4, Height: 2, Mass: 2},
		{Name: "wisp", Species: weightless, Width: 1, Height: 1},
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
