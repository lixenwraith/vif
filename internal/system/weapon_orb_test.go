package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// armWeapon grants one charge of one weapon to one cursor and settles the grant.
func armWeapon(w *engine.World, weapon *WeaponSystem, cursor core.Entity, wt component.WeaponType) {
	weapon.HandleEvent(event.GameEvent{
		Type:    event.EventWeaponAddRequest,
		Payload: &event.WeaponAddRequestPayload{Entity: cursor, Weapon: wt},
	})
}

// orbsOwnedBy counts one cursor's orbs per weapon type straight from the store.
func orbsOwnedBy(w *engine.World, cursor core.Entity) [component.WeaponCount]int {
	var out [component.WeaponCount]int
	for _, e := range w.Components.Orb.Entities() {
		orb, ok := w.Components.Orb.GetComponent(e)
		if !ok || orb.OwnerEntity != cursor {
			continue
		}
		if orb.WeaponType >= 0 && orb.WeaponType < component.WeaponCount {
			out[orb.WeaponType]++
		}
	}
	return out
}

// settleDeaths runs the death system over whatever the weapon system emitted, so a
// reaped orb is gone from the store rather than merely requested.
func settleDeaths(w *engine.World, deaths *DeathSystem) {
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type == event.EventDeathBatch {
			deaths.HandleEvent(ev)
		}
	}
}

// TestOrbsAreRecoveredFromTheStoreRatherThanDuplicated is the orb index's whole
// claim: the Orb store is the index, so an orb is found rather than replaced, and
// anything the store holds that no loadout justifies leaves.
//
// The three injections are the three ways the old cached index could be wrong and
// could not say so: a second orb for a pair that already has one (what a lost
// reference produced, once per correction), an orb whose owner is not a cursor this
// instance simulates (D-2), and an orb for a weapon whose charges are gone.
func TestOrbsAreRecoveredFromTheStoreRatherThanDuplicated(t *testing.T) {
	w, cursor, other := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	deaths := NewDeathSystem(w).(*DeathSystem)

	armWeapon(w, weapon, cursor, component.WeaponRod)
	armWeapon(w, weapon, cursor, component.WeaponLauncher)
	w.Resources.Event.Queue.Consume()

	weapon.Update()
	settleDeaths(w, deaths)
	if got := orbsOwnedBy(w, cursor); got != [component.WeaponCount]int{1, 1, 0} {
		t.Fatalf("orbs after arming rod and launcher = %v, want one each", got)
	}

	// A second orb for a pair that already has one, the shape a lost reference used
	// to leave behind. The survivor is the older entity, not whichever the dense
	// store happens to hold first.
	kept := orbSlotsOf(w, cursor)[component.WeaponRod]
	duplicate := weapon.spawnOrbEntity(cursor, component.WeaponRod)
	if duplicate == 0 || duplicate == kept {
		t.Fatalf("duplicate orb = %d, want a second entity beside %d", duplicate, kept)
	}
	// An orb owned by a cursor this instance does not simulate (D-2), and one for a
	// weapon that was never charged.
	remote := spawnRemoteCursor(t, w, 2, 30, 5, 9)
	orphan := weapon.spawnOrbEntity(remote, component.WeaponRod)
	stale := weapon.spawnOrbEntity(cursor, component.WeaponDisruptor)
	if orphan == 0 || stale == 0 {
		t.Fatal("the injected orbs were not created; the reap proves nothing")
	}
	w.Resources.Event.Queue.Consume()

	weapon.Update()
	settleDeaths(w, deaths)

	if got := orbsOwnedBy(w, cursor); got != [component.WeaponCount]int{1, 1, 0} {
		t.Fatalf("orbs after the reap = %v, want one rod and one launcher", got)
	}
	if !w.Components.Orb.HasEntity(kept) {
		t.Fatalf("the reap dropped the surviving orb %d rather than its duplicate", kept)
	}
	for name, e := range map[string]core.Entity{"duplicate": duplicate, "remote-owned": orphan, "uncharged": stale} {
		if w.Components.Orb.HasEntity(e) {
			t.Errorf("%s orb %d survived the reap", name, e)
		}
		if w.Positions.HasPosition(e) {
			t.Errorf("%s orb %d still holds a placement", name, e)
		}
	}
	if orbsOwnedBy(w, other) != [component.WeaponCount]int{} {
		t.Error("the reap gave the unarmed cursor orbs")
	}
}

// orbSlotsOf reads one cursor's orb index through the system's own accessor.
func orbSlotsOf(w *engine.World, cursor core.Entity) orbSlots {
	return NewWeaponSystem(w).(*WeaponSystem).orbsOf(cursor)
}
