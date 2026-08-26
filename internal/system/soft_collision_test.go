package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
)

func TestSoftCollisionMatrixExcludesDrain(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := NewSoftCollisionSystem(w).(*SoftCollisionSystem)

	for species := component.SpeciesType(1); species < component.SpeciesCount; species++ {
		if s.matrix[component.SpeciesDrain][species] != nil {
			t.Errorf("drain still pushes %s through soft collision", component.SpeciesNames[species])
		}
		if s.matrix[species][component.SpeciesDrain] != nil {
			t.Errorf("%s still pushes drain through soft collision", component.SpeciesNames[species])
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

			s := NewSoftCollisionSystem(w).(*SoftCollisionSystem)
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

func TestSoftCollisionImpulseStreamFollowsTargetDomain(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := NewSoftCollisionSystem(w).(*SoftCollisionSystem)

	shared := s.impulseStream(w.CreateEntity(core.DomainShared))
	player := s.impulseStream(w.CreateEntity(core.DomainPlayer))
	if shared == player {
		t.Fatal("player impulses draw from the shared stream")
	}
}
