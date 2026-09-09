package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// newStormBreachFixture spawns a storm and returns the systems the incident ran
// through: CompositeSystem detects the member loss, StormSystem owns the circle.
func newStormBreachFixture(t *testing.T) (*engine.World, *StormSystem, *CompositeSystem) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 160, 60, engine.NewManualClock())

	storm := NewStormSystem(w).(*StormSystem)
	composite := NewCompositeSystem(w).(*CompositeSystem)

	storm.HandleEvent(event.GameEvent{Type: event.EventStormSpawnRequest})
	if storm.rootEntity == 0 {
		t.Fatal("storm did not spawn")
	}
	return w, storm, composite
}

// settleBreach runs CompositeSystem's detection pass and routes what it derives
// until the queue drains. Both systems see every event; each ignores the rest.
func settleBreach(w *engine.World, storm *StormSystem, composite *CompositeSystem) {
	for range 8 {
		composite.Update()
		events := w.Resources.Event.Queue.Consume()
		if len(events) == 0 {
			return
		}
		for _, ev := range events {
			storm.HandleEvent(ev)
			composite.HandleEvent(ev)
		}
	}
}

// livingMembers counts a circle header's members that still hold a Combat component.
func livingMembers(w *engine.World, header core.Entity) int {
	headerComp, ok := w.Components.Header.GetComponent(header)
	if !ok {
		return 0
	}
	count := 0
	for _, m := range headerComp.MemberEntries {
		if m.Entity != 0 && w.Components.Combat.HasEntity(m.Entity) {
			count++
		}
	}
	return count
}

// TestStormCircleRetiresOnlyWhenEmptied is the regression for the phantom target a
// map crop produced. A partial member loss reads as an integrity breach; retiring
// the circle on one cleared CirclesAlive without destroying anything, and render,
// physics and member reaping all key off that flag — the survivors went unseen and
// unreaped while staying combat-live, then outlived the storm entirely.
func TestStormCircleRetiresOnlyWhenEmptied(t *testing.T) {
	w, storm, composite := newStormBreachFixture(t)

	stormComp, _ := w.Components.Storm.GetComponent(storm.rootEntity)
	circle := stormComp.Circles[0]
	headerComp, _ := w.Components.Header.GetComponent(circle)
	total := len(headerComp.MemberEntries)
	if total < 4 {
		t.Fatalf("circle members = %d, want a populated ellipse", total)
	}

	// A crop destroys members directly; nothing tells the composite first.
	for i := range 3 {
		w.DestroyEntity(headerComp.MemberEntries[i].Entity)
	}
	settleBreach(w, storm, composite)

	stormComp, _ = w.Components.Storm.GetComponent(storm.rootEntity)
	if !stormComp.CirclesAlive[0] {
		t.Fatalf("circle retired after losing 3 of %d members, want it alive", total)
	}
	if got := livingMembers(w, circle); got != total-3 {
		t.Fatalf("living members = %d, want %d", got, total-3)
	}

	// Losing the rest is the circle's death: nothing combat-live may outlive it.
	headerComp, _ = w.Components.Header.GetComponent(circle)
	survivors := []core.Entity{circle}
	for _, m := range headerComp.MemberEntries {
		if m.Entity != 0 {
			survivors = append(survivors, m.Entity)
			w.DestroyEntity(m.Entity)
		}
	}
	settleBreach(w, storm, composite)

	if w.Components.Header.HasEntity(circle) {
		t.Fatal("emptied circle header survived, invisible and still targetable")
	}
	for _, e := range survivors {
		if w.Components.Combat.HasEntity(e) {
			t.Fatalf("entity %d still carries combat state after the circle died", e)
		}
	}
}

// TestOffMapEntityIsNotATarget pins the other half of the same incident. A crop
// leaves entities it may not destroy outside the new bounds, where the grid holds
// no cell and the renderer clips them. The finders read component stores rather
// than the grid, so without this they answer with something nothing can account for.
func TestOffMapEntityIsNotATarget(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	config := w.Resources.Config

	e := w.CreateEntity(core.DomainShared)
	w.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityDrain, HitPoints: 10,
	})

	w.Positions.SetPosition(e, component.PositionComponent{X: config.MapWidth + 3, Y: 2})
	if got := FindNearestTargets(w, 1, 1, 1, engine.ScopeBoth, 0); len(got) != 0 {
		t.Fatalf("targets = %v, want none while the entity sits off the map", got)
	}

	w.Positions.SetPosition(e, component.PositionComponent{X: config.MapWidth - 1, Y: 2})
	if got := FindNearestTargets(w, 1, 1, 1, engine.ScopeBoth, 0); len(got) != 1 || got[0].Target != e {
		t.Fatalf("targets = %v, want the same entity once it is back in bounds", got)
	}
}
