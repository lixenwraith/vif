package system

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

func TestTracerSamplesEverySweptCellBeforeCombatImpact(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
	weapon.fireTracers(cursor, 5, 5, gunVolley(&component.WeaponComponent{}, &component.WeaponSpecs[component.WeaponGun], 1), component.PalettePositive)
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
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
	spec := &component.WeaponSpecs[component.WeaponGun]
	for _, charges := range []int{1, spec.MaxCharges} {
		for _, e := range w.Components.Tracer.GetAllEntities() {
			w.DestroyEntity(e)
		}
		weapon.fireTracers(cursor, 5, 5, gunVolley(&component.WeaponComponent{}, spec, charges), component.PalettePositive)
		lanes := tracerLanes(charges)
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

// TestAnAimedGunFiresOnlyAlongItsAim: an aim request narrows the local cursor's
// volley to its cardinals, and a zero aim gives the gun back its own four.
func TestAnAimedGunFiresOnlyAlongItsAim(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	volley := func(aim component.TracerDirection) (right, other int) {
		for _, e := range w.Components.Tracer.GetAllEntities() {
			w.DestroyEntity(e)
		}
		weapon.HandleEvent(event.GameEvent{Type: event.EventGunAimRequest, Payload: &event.GunAimRequestPayload{Directions: aim}})
		weaponComp.Cooldown[component.WeaponGun] = 0
		weapon.fireLoadout(cursor, weaponComp, weapon.orbsOf(cursor))
		for _, e := range w.Components.Tracer.Entities() {
			if k, _ := w.Components.Kinetic.GetComponent(e); k.VelX > 0 {
				right++
			} else {
				other++
			}
		}
		return right, other
	}
	if right, other := volley(component.TracerRight); right != 1 || other != 0 {
		t.Fatalf("aimed right: %d right and %d other tracers, want 1 and 0", right, other)
	}
	if right, other := volley(0); right != 1 || other != 3 {
		t.Fatalf("aim cleared: %d right and %d other tracers, want 1 and 3", right, other)
	}
}

// TestAGunOnAPathFiresTheLevelItsChargesCountTo: a path names each level's lanes
// along each cardinal, and a gun on it fires the level its charges count to, holds
// a charge per level and keeps its charges when the path changes; a request without
// a path gives it back a lane per charge along its aim, up to its own cap.
func TestAGunOnAPathFiresTheLevelItsChargesCountTo(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	gun := component.WeaponGun
	// fired is the lane offsets the cursor's next volley sends along each cardinal
	fired := func() map[component.TracerDirection][]int {
		for _, e := range w.Components.Tracer.GetAllEntities() {
			w.DestroyEntity(e)
		}
		weaponComp.Cooldown[gun] = 0
		weapon.fireLoadout(cursor, weaponComp, weapon.orbsOf(cursor))
		lanes := map[component.TracerDirection][]int{}
		for _, e := range w.Components.Tracer.Entities() {
			pos, _ := w.Positions.GetPosition(e)
			k, _ := w.Components.Kinetic.GetComponent(e)
			switch {
			case k.VelX > 0:
				lanes[component.TracerRight] = append(lanes[component.TracerRight], pos.Y-5)
			case k.VelX < 0:
				lanes[component.TracerLeft] = append(lanes[component.TracerLeft], pos.Y-5)
			case k.VelY > 0:
				lanes[component.TracerDown] = append(lanes[component.TracerDown], pos.X-5)
			default:
				lanes[component.TracerUp] = append(lanes[component.TracerUp], pos.X-5)
			}
		}
		for _, l := range lanes {
			slices.Sort(l)
		}
		return lanes
	}
	want := func(right, left, down, up int) map[component.TracerDirection][]int {
		lanes := map[component.TracerDirection][]int{}
		for d, n := range map[component.TracerDirection]int{
			component.TracerRight: right, component.TracerLeft: left, component.TracerDown: down, component.TracerUp: up,
		} {
			if n > 0 {
				lanes[d] = tracerLanes(n)
			}
		}
		return lanes
	}

	// One level more than the gun's own cap, so the path's is the one that holds
	path := []component.GunLevel{{Right: 1}, {Right: 3, Down: 1, Up: 1}, {Left: 2}, {Right: 2, Left: 1}}
	weapon.HandleEvent(event.GameEvent{Type: event.EventGunAimRequest, Payload: &event.GunAimRequestPayload{Path: path}})
	for level, expect := range []map[component.TracerDirection][]int{
		want(1, 0, 0, 0), want(3, 0, 1, 1), want(0, 2, 0, 0), want(2, 1, 0, 0),
	} {
		weaponComp.Charges[gun] = level + 1
		if got := fired(); !maps.EqualFunc(got, expect, slices.Equal) {
			t.Fatalf("level %d fired %v, want %v", level+1, got, expect)
		}
	}
	weaponComp.Charges[gun] = parameter.WeaponMaxChargeGun
	for range 3 {
		weapon.addWeapon(cursor, gun)
	}
	if weaponComp.Charges[gun] != len(path) {
		t.Fatalf("a gun on a %d-level path climbed to %d charges", len(path), weaponComp.Charges[gun])
	}

	weapon.HandleEvent(event.GameEvent{Type: event.EventGunAimRequest, Payload: &event.GunAimRequestPayload{Path: path[:2]}})
	if got := fired(); weaponComp.Charges[gun] != len(path) || !maps.EqualFunc(got, want(3, 0, 1, 1), slices.Equal) {
		t.Fatalf("on a shorter path %d charges fired %v, want %d charges firing its last level", weaponComp.Charges[gun], got, len(path))
	}

	weapon.HandleEvent(event.GameEvent{Type: event.EventGunAimRequest, Payload: &event.GunAimRequestPayload{Directions: component.TracerRight}})
	weaponComp.Charges[gun] = 2
	if got := fired(); !maps.EqualFunc(got, want(2, 0, 0, 0), slices.Equal) {
		t.Fatalf("off the path two charges aimed right fired %v, want two lanes right", got)
	}
	for range 3 {
		weapon.addWeapon(cursor, gun)
	}
	if weaponComp.Charges[gun] != parameter.WeaponMaxChargeGun || weaponComp.GunLevels != 0 || weaponComp.GunPath != [parameter.GunPathMax]component.GunLevel{} {
		t.Fatalf("off the path: %d charges, %d levels, want the gun's own cap and no path", weaponComp.Charges[gun], weaponComp.GunLevels)
	}
}

// TestGunOutlivesAnEnergyZeroCrossingAndLeavesOnRequest: a fresh loadout holds
// the gun; a zero crossing strips the loot and keeps it; a remove request takes
// it, and main fire then launches nothing.
func TestGunOutlivesAnEnergyZeroCrossingAndLeavesOnRequest(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
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

// The kinetic window covers every attacker and weapon: a hit on a closed window
// opens it with its impulse, one on an open window lands only when stronger than
// the impulse that opened it, resetting the window, and a window a species opened
// without an impulse refuses every hit.
func TestStrongerKnockbackResetsTheKineticWindow(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 10, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntitySwarm, HitPoints: 100,
	})
	w.Components.Kinetic.SetComponent(target, component.KineticComponent{})
	c, _ := w.Components.Combat.GetPtr(target)
	k, _ := w.Components.Kinetic.GetPtr(target)
	hit := func() (landed bool) {
		before, immune := k.VelX, combat.statKineticImmune.Load()
		combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
			AttackType: component.CombatAttackTracer, OwnerEntity: cursor, OriginEntity: cursor,
			TargetEntity: target, HitEntity: target, HasVelocity: true, OriginVelX: 1,
		})
		c.RemainingDamageImmunity = 0 // damage is not under test
		if combat.statKineticImmune.Load() != immune {
			return false
		}
		if k.VelX == before {
			t.Fatal("an accepted knockback left the velocity alone")
		}
		return true
	}

	if !hit() || c.RemainingKineticImmunity != parameter.CombatKineticImmunityDuration || c.KineticStrength <= 0 {
		t.Fatalf("first hit: window=%v strength=%v, want opened with the impulse", c.RemainingKineticImmunity, c.KineticStrength)
	}
	c.RemainingKineticImmunity, c.KineticStrength = time.Millisecond, 1e9
	if hit() || c.KineticStrength != 1e9 {
		t.Fatal("a weaker hit landed on the open window")
	}
	c.KineticStrength = 1e-9
	if !hit() || c.RemainingKineticImmunity != parameter.CombatKineticImmunityDuration || c.KineticStrength <= 1e-9 {
		t.Fatalf("stronger hit: window=%v strength=%v, want reset with the new impulse", c.RemainingKineticImmunity, c.KineticStrength)
	}
	c.KineticStrength = 0
	if hit() {
		t.Fatal("a hit landed on a window a species opened")
	}
}
