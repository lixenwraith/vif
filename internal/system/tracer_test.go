package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
)

func TestTracerSamplesEverySweptCellBeforeCombatImpact(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	weapon.fireTracers(cursor, 5, 5, &component.WeaponSpecs[component.WeaponGun], 1, component.PalettePositive)
	w.Resources.Event.Queue.Consume() // Spawn sound.

	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 10, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        1,
	})

	w.Resources.Time.DeltaTime = 100 * time.Millisecond
	weapon.advanceTracers()

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

	var impacted *component.TracerComponent
	for _, tracerEntity := range w.Components.Tracer.Entities() {
		tracer, ok := w.Components.Tracer.GetPtr(tracerEntity)
		if !ok || !tracer.Blocked {
			continue
		}
		impacted = tracer
		pos, _ := w.Positions.GetPosition(tracerEntity)
		if pos.X != 10 || pos.Y != 5 {
			t.Fatalf("impacted tracer position = %#v, want (10,5)", pos)
		}
		break
	}
	if impacted == nil {
		t.Fatal("right-moving tracer did not stop on the crossed combatant cell")
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

// TestGunLanesFollowItsCharges: one lane per direction at one charge, every lane
// of the table at the cap, each starting on its own row or column beside the cursor.
func TestGunLanesFollowItsCharges(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	spec := &component.WeaponSpecs[component.WeaponGun]
	for _, charges := range []int{1, spec.MaxCharges} {
		for _, e := range w.Components.Tracer.GetAllEntities() {
			w.DestroyEntity(e)
		}
		weapon.fireTracers(cursor, 5, 5, spec, charges, component.PalettePositive)
		lanes := component.TracerLanes(charges)
		rows, cols := map[int]bool{}, map[int]bool{}
		for _, e := range w.Components.Tracer.Entities() {
			pos, _ := w.Positions.GetPosition(e)
			if k, _ := w.Components.Kinetic.GetComponent(e); k.VelX != 0 {
				rows[pos.Y-5] = true
			} else {
				cols[pos.X-5] = true
			}
		}
		if n := w.Components.Tracer.CountEntities(); n != 4*len(lanes) || len(rows) != len(lanes) || len(cols) != len(lanes) {
			t.Fatalf("%d charges: %d tracers on rows %v and columns %v, want lanes %v each way", charges, n, rows, cols, lanes)
		}
		for _, lane := range lanes {
			if !rows[lane] || !cols[lane] {
				t.Fatalf("%d charges: lane %d missing from rows %v or columns %v", charges, lane, rows, cols)
			}
		}
	}
}

// TestGunOutlivesAnEnergyZeroCrossingAndLeavesOnRequest: a fresh loadout holds
// the gun; a zero crossing strips the loot and keeps it; a remove request takes
// it, and main fire then launches nothing.
func TestGunOutlivesAnEnergyZeroCrossingAndLeavesOnRequest(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	if weaponComp.Charges[component.WeaponGun] != 1 {
		t.Fatalf("fresh loadout = %v, want one gun charge", weaponComp.Charges)
	}
	weaponComp.Charges[component.WeaponRod] = 2
	weapon.removeAllWeapons(cursor)
	if weaponComp.Charges[component.WeaponRod] != 0 || weaponComp.Charges[component.WeaponGun] != 1 {
		t.Fatalf("loadout after a zero crossing = %v, want the rod gone and the gun kept", weaponComp.Charges)
	}

	weapon.HandleEvent(event.GameEvent{Type: event.EventWeaponRemoveRequest,
		Payload: &event.WeaponRemoveRequestPayload{Entity: cursor, Weapon: component.WeaponGun}})
	if weaponComp.Charges[component.WeaponGun] != 0 {
		t.Fatal("a remove request left the gun")
	}
	w.Resources.Event.Queue.Consume()
	weapon.fireLoadout(cursor, weaponComp, weapon.orbsOf(cursor))
	if n := w.Components.Tracer.CountEntities(); n != 0 {
		t.Fatalf("%d tracers fired without the gun", n)
	}
}
