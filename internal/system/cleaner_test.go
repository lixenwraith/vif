package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

func TestCleanerSamplesEverySweptCellBeforeCombatImpact(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	cleaners := NewCleanerSystem(w).(*CleanerSystem)
	cleaners.spawnDirectionalCleaners(cursor, 5, 5, component.CleanerColorPositive)
	w.Resources.Event.Queue.Consume() // Spawn sound.

	enemy := w.CreateEntity()
	w.Positions.SetPosition(enemy, component.PositionComponent{X: 10, Y: 5})
	w.Components.Combat.SetComponent(enemy, component.CombatComponent{
		OwnerEntity:      enemy,
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
		if !ok || payload.OwnerEntity != cursor || payload.TargetEntity != enemy {
			t.Fatalf("combat payload = %#v, want cursor %d targeting enemy %d", ev.Payload, cursor, enemy)
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
		t.Fatal("right-moving cleaner did not stop on the crossed enemy cell")
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

func TestSweepingCleanerUsesFullMapBoundsWhenViewportIsSmaller(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	w.SetupLevel(120, 30, false, false)

	glyph := w.CreateEntity()
	w.Positions.SetPosition(glyph, component.PositionComponent{X: 100, Y: 5})
	w.Components.Glyph.SetComponent(glyph, component.GlyphComponent{Rune: 'x', Type: component.GlyphRed})

	cleaners := NewCleanerSystem(w).(*CleanerSystem)
	cleaners.spawnSweepingCleaners(cursor)
	entities := w.Components.Cleaner.Entities()
	if len(entities) != 1 {
		t.Fatalf("sweeping cleaner count = %d, want 1", len(entities))
	}
	cleaner, _ := w.Components.Cleaner.GetComponent(entities[0])
	minX, maxX := cleanerFlightBounds(w.Resources.Config.MapWidth)
	if cleaner.TargetX != maxX || cleaner.TrailRing[0].X != int(minX-0.5) {
		t.Fatalf("sweep bounds = start cell %d target %v, want map-wide start %v target %v",
			cleaner.TrailRing[0].X, cleaner.TargetX, minX, maxX)
	}
}
