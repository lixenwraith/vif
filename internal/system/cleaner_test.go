package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
)

func TestCleanerSamplesEverySweptCellBeforeCombatImpact(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	cleaners := NewCleanerSystem(w).(*CleanerSystem)
	cleaners.spawnDirectionalCleaners(core.DomainShared, cursor, 5, 5, component.CleanerColorPositive)
	w.Resources.Event.Queue.Consume() // Spawn sound.

	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 10, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        1,
	})

	w.Resources.Time.DeltaTime = 100 * time.Millisecond
	cleaners.Update()

	combatRequests := 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type != event.EventCombatAttackDirectRequest {
			continue
		}
		payload, ok := ev.Payload.(*event.CombatAttackDirectRequestPayload)
		if !ok || payload.OwnerEntity != cursor || payload.TargetEntity != target {
			t.Fatalf("combat payload = %#v, want cursor %d targeting combatant %d", ev.Payload, cursor, target)
		}
		combatRequests++
	}
	if combatRequests != 1 {
		t.Fatalf("combat requests = %d, want 1", combatRequests)
	}

	var impacted *component.CleanerComponent
	for _, cleanerEntity := range w.Components.Cleaner.Entities() {
		cleaner, ok := w.Components.Cleaner.GetPtr(cleanerEntity)
		if !ok || !cleaner.Blocked {
			continue
		}
		impacted = cleaner
		pos, _ := w.Positions.GetPosition(cleanerEntity)
		if pos.X != 10 || pos.Y != 5 {
			t.Fatalf("impacted cleaner position = %#v, want (10,5)", pos)
		}
		break
	}
	if impacted == nil {
		t.Fatal("right-moving cleaner did not stop on the crossed combatant cell")
	}
	if impacted.TrailLen != 6 {
		t.Fatalf("trail length = %d, want origin plus five crossed cells", impacted.TrailLen)
	}
	for i, wantX := range []int{10, 9, 8, 7, 6, 5} {
		idx := (impacted.TrailHead - i + len(impacted.TrailRing)) % len(impacted.TrailRing)
		if got := impacted.TrailRing[idx]; got.X != wantX || got.Y != 5 {
			t.Fatalf("trail[%d] = %#v, want (%d,5)", i, got, wantX)
		}
	}
}
