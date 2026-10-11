package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// capTarget places one shared single combatant
func capTarget(w *engine.World, x, y int) core.Entity {
	e := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityDrain, HitPoints: 5,
	})
	return e
}

// TestCappedRodArcsToTheNearestOtherTargetInRange: a volley's hit arcs once, from
// the struck cell, to the nearest target within range that the volley did not
// already strike; a struck or distant target gets no arc, and a distant composite
// does not hide a near single behind the rod's composites-first priority.
func TestCappedRodArcsToTheNearestOtherTargetInRange(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	near, next, far := capTarget(w, 8, 5), capTarget(w, 10, 5), capTarget(w, 8+2*int(parameter.LightningArcRange), 5)
	header, member := w.CreateEntity(core.DomainShared), w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 30, Y: 20})
	w.Positions.SetPosition(member, component.PositionComponent{X: 30, Y: 20})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type: component.CompositeTypeUnit, MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, HitPoints: 5})
	hit := func(e core.Entity) TargetAssignment { return TargetAssignment{Target: e, Hit: e} }

	requests := func(assignments ...TargetAssignment) map[core.Entity][2]int {
		w.Resources.Event.Queue.Consume()
		weapon.fireLightning(cursor, 5, 5, component.CombatAttackLightning, assignments, parameter.LightningArcRange)
		origins := map[core.Entity][2]int{}
		for _, ev := range w.Resources.Event.Queue.Consume() {
			p := ev.Payload.(*event.CombatAttackDirectRequestPayload)
			if _, dup := origins[p.TargetEntity]; dup {
				t.Fatalf("%d struck twice in one volley", p.TargetEntity)
			}
			origins[p.TargetEntity] = [2]int{p.OriginX, p.OriginY}
		}
		return origins
	}

	got := requests(hit(near))
	if len(got) != 2 || got[near] != [2]int{5, 5} || got[next] != [2]int{8, 5} {
		t.Fatalf("requests = %v, want %d from the cursor and %d arced from %d's cell", got, near, next, near)
	}
	if got := requests(hit(near), hit(next)); len(got) != 2 {
		t.Fatalf("requests = %v, want no arc: %d is already struck and %d is out of range", got, next, far)
	}
}

// TestCappedTurretStreamsBetweenWavesAndStopsWithTheCooldown: a wave arms the
// stream; each interval while the wave cooldown runs fires TurretStream bullets
// without touching the cooldown; a cooldown that has ended fires none.
func TestCappedTurretStreamsBetweenWavesAndStopsWithTheCooldown(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	capTarget(w, 8, 5)
	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	weaponComp.Charges[component.WeaponTurret] = component.WeaponSpecs[component.WeaponTurret].MaxCharges
	bullets := func() int {
		n := 0
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type == event.EventBulletSpawnRequest {
				n++
			}
		}
		return n
	}

	weapon.Update()
	w.Resources.Event.Queue.Consume()
	weapon.fireLoadout(cursor, weaponComp, weapon.orbsOf(cursor))
	if got := bullets(); got != weaponComp.Charges[component.WeaponTurret] {
		t.Fatalf("wave = %d bullets, want one per charge", got)
	}
	cooldown := weaponComp.Cooldown[component.WeaponTurret]

	w.Resources.Time.DeltaTime = parameter.WeaponStreamInterval
	weapon.Update()
	if got := bullets(); got != parameter.TurretStream || weaponComp.Cooldown[component.WeaponTurret] != cooldown-parameter.WeaponStreamInterval {
		t.Fatalf("stream = %d bullets, cooldown %v; want %d and the wave cooldown merely aged",
			got, weaponComp.Cooldown[component.WeaponTurret], parameter.TurretStream)
	}

	weaponComp.Cooldown[component.WeaponTurret] = 0
	weapon.Update()
	if got := bullets(); got != 0 {
		t.Fatalf("stream = %d bullets after the cooldown ended, want none", got)
	}
}

// TestCappedEmitterSustainsItsRayWhileMainFireIsHeld: the last charge converts a
// burning timed ray in place, held at full strength past any duration while fire
// requests keep coming, taken by the main cooldown or not; main fire at the cap resets
// no cooldown and flashes no orb; once requests stop the ray burns out as a timed one,
// nothing relights it without a request, and one request does.
func TestCappedEmitterSustainsItsRayWhileMainFireIsHeld(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	capTarget(w, 8, 5)
	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	weaponComp.Charges[component.WeaponGun] = 0
	spec := &component.WeaponSpecs[component.WeaponEmitter]
	weaponComp.Charges[component.WeaponEmitter] = spec.MaxCharges - 1
	weapon.Update()
	orb := weapon.orbsOf(cursor)[component.WeaponEmitter]
	weapon.fire(cursor)
	timed, ok := w.Components.Ray.GetPtr(orb)
	if !ok {
		t.Fatal("no ray fired below the cap")
	}

	weaponComp.Charges[component.WeaponEmitter] = spec.MaxCharges
	weaponComp.Cooldown[component.WeaponEmitter] = 0 // the timed shot's; main fire at the cap must leave it so
	w.Resources.Time.DeltaTime = parameter.WeaponCooldownMain
	for range int(2 * timed.Duration / parameter.WeaponCooldownMain) {
		weapon.fire(cursor)
		weapon.Update()
	}
	ray, ok := w.Components.Ray.GetPtr(orb)
	if !ok || ray != timed || ray.Scale != spec.MaxCharges || ray.Remaining != ray.Duration {
		t.Fatalf("held ray at the cap = %+v, want the same ray outliving its duration at scale %d and full strength", ray, spec.MaxCharges)
	}
	orbComp, _ := w.Components.Orb.GetPtr(orb)
	if weaponComp.Cooldown[component.WeaponEmitter] != 0 || orbComp.FlashRemaining != 0 {
		t.Fatalf("cooldown %v, flash %v after main fire at the cap, want neither touched",
			weaponComp.Cooldown[component.WeaponEmitter], orbComp.FlashRemaining)
	}

	w.Resources.Time.DeltaTime = parameter.RayHold
	weapon.Update()
	released, ok := w.Components.Ray.GetPtr(orb)
	if !ok || released.Remaining >= released.Duration {
		t.Fatalf("ray after the hold lapsed = %+v, want it burning down as a timed ray", released)
	}
	w.Resources.Time.DeltaTime = released.Duration
	weapon.Update()
	if _, ok := w.Components.Ray.GetPtr(orb); ok {
		t.Fatal("released ray outlived its duration")
	}

	w.Resources.Time.DeltaTime = parameter.WeaponCooldownMain
	weapon.Update()
	if _, ok := w.Components.Ray.GetPtr(orb); ok {
		t.Fatal("ray lit at the cap without a fire request")
	}
	weapon.fire(cursor)
	weapon.Update()
	if _, ok := w.Components.Ray.GetPtr(orb); !ok {
		t.Fatal("no ray lit at the cap by a fire request")
	}
}
