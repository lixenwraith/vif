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
	weapon.fireAllWeapons(cursor, weaponComp, weapon.orbsOf(cursor))
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
