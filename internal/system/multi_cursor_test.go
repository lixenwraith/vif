package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func testCursorWorld(t *testing.T) (*engine.World, core.Entity, core.Entity) {
	t.Helper()

	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := NewCursorSystem(w).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: 5, Y: 5, Slot: 0, Control: uint8(component.ControlHuman),
		},
	})
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: 15, Y: 5, Slot: 1, Control: uint8(component.ControlBot),
		},
	})

	first := w.Resources.Player.Slot(0)
	second := w.Resources.Player.Slot(1)
	if first == 0 || second == 0 || first == second {
		t.Fatalf("cursor roster = (%d, %d), want two distinct entities", first, second)
	}
	w.Resources.Event.Queue.Consume()
	return w, first, second
}

func TestClosestCursorUsesRoster(t *testing.T) {
	w, first, second := testCursorWorld(t)

	got, _, _, ok := ClosestCursor(w, 14, 5)
	if !ok || got != second {
		t.Fatalf("closest cursor = (%d, %t), want (%d, true)", got, ok, second)
	}

	got, _, _, ok = ClosestCursor(w, 10, 5)
	if !ok || got != first {
		t.Fatalf("tie cursor = (%d, %t), want slot-zero entity %d", got, ok, first)
	}
}

func TestCursorOverlapIncludesEveryTouchingPlayer(t *testing.T) {
	w, first, second := testCursorWorld(t)
	cursors := NewCursorSystem(w).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorMoveRequest,
		Payload: &event.CursorMoveRequestPayload{
			Entity: second,
			X:      5,
			Y:      5,
		},
	})
	w.Resources.Event.Queue.Consume()

	entity := w.CreateEntity()
	w.Positions.SetPosition(entity, component.PositionComponent{X: 5, Y: 5})

	overlaps := CheckCursorOverlaps(w, entity)
	if overlaps.Count != 2 {
		t.Fatalf("overlap count = %d, want 2", overlaps.Count)
	}
	if overlaps.Entries[0].Cursor != first || overlaps.Entries[1].Cursor != second {
		t.Fatalf("overlap cursors = (%d, %d), want (%d, %d)", overlaps.Entries[0].Cursor, overlaps.Entries[1].Cursor, first, second)
	}
	if !overlaps.Entries[0].OnCursor || !overlaps.Entries[1].OnCursor {
		t.Fatalf("overlap flags = (%t, %t), want both true", overlaps.Entries[0].OnCursor, overlaps.Entries[1].OnCursor)
	}
}

func TestCursorMoveRejectsZeroEntity(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	cursors := NewCursorSystem(w).(*CursorSystem)

	cursors.HandleEvent(event.GameEvent{
		Type:    event.EventCursorMoveRequest,
		Payload: &event.CursorMoveRequestPayload{X: 9, Y: 8},
	})

	pos, ok := w.Positions.GetPosition(first)
	if !ok || pos.X != 5 || pos.Y != 5 {
		t.Fatalf("local cursor moved to %#v from a zero-entity command", pos)
	}
}

func TestNavigationGroupZeroPublishesRoster(t *testing.T) {
	w, first, second := testCursorWorld(t)
	navigation := NewNavigationSystem(w).(*NavigationSystem)
	navigation.resolveGroupTargets()

	state := w.Resources.Target.GetGroup(0)
	if !state.Valid || state.Count != 2 {
		t.Fatalf("group zero = %#v, want two valid targets", state)
	}
	if state.Targets[0].Entity != first || state.Targets[1].Entity != second {
		t.Fatalf("group-zero entities = (%d, %d), want (%d, %d)", state.Targets[0].Entity, state.Targets[1].Entity, first, second)
	}

	empty := engine.NewWorld()
	engine.NewGameContextWithClock(empty, 40, 24, engine.NewManualClock())
	emptyNavigation := NewNavigationSystem(empty).(*NavigationSystem)
	emptyNavigation.resolveGroupTargets()
	if state := empty.Resources.Target.GetGroup(0); state.Valid || state.Count != 0 {
		t.Fatalf("empty group zero = %#v, want invalid", state)
	}
}

func TestBulletCollisionAddressesHitCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	bullets := NewBulletSystem(w).(*BulletSystem)
	damage := component.BulletDamage{EnergyDrain: -7, HeatDelta: -11}

	if !bullets.collideCursor(&component.BulletComponent{Damage: damage}, 15, 5) {
		t.Fatal("direct hit did not collide with slot-one cursor")
	}
	events := w.Resources.Event.Queue.Consume()
	if len(events) != 1 || events[0].Type != event.EventHeatAddRequest {
		t.Fatalf("direct-hit events = %#v, want one heat command", events)
	}
	heat, ok := events[0].Payload.(*event.HeatAddRequestPayload)
	if !ok || heat.Entity != second || heat.Delta != damage.HeatDelta {
		t.Fatalf("heat payload = %#v, want entity %d delta %d", events[0].Payload, second, damage.HeatDelta)
	}

	shield, ok := w.Components.Shield.GetComponent(first)
	if !ok {
		t.Fatal("slot-zero cursor has no shield component")
	}
	shield.Active = true
	shield.InvRxSq = 1
	shield.InvRySq = 1
	w.Components.Shield.SetComponent(first, shield)

	if !bullets.collideCursor(&component.BulletComponent{Damage: damage}, 5, 5) {
		t.Fatal("shield hit did not collide with slot-zero cursor")
	}
	events = w.Resources.Event.Queue.Consume()
	if len(events) != 1 || events[0].Type != event.EventShieldDrainRequest {
		t.Fatalf("shield-hit events = %#v, want one shield command", events)
	}
	drain, ok := events[0].Payload.(*event.ShieldDrainRequestPayload)
	if !ok || drain.Entity != first || drain.Value != damage.EnergyDrain {
		t.Fatalf("shield payload = %#v, want entity %d value %d", events[0].Payload, first, damage.EnergyDrain)
	}
}

func TestCombatQueriesExcludeEveryCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	if HasCombatTargetAt(w, 15, 5, 0, first) {
		t.Fatalf("slot-one cursor %d was treated as a combat target", second)
	}

	enemy := w.CreateEntity()
	w.Positions.SetPosition(enemy, component.PositionComponent{X: 16, Y: 5})
	w.Components.Combat.SetComponent(enemy, component.CombatComponent{
		OwnerEntity:      enemy,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        1,
	})
	targets := FindNearestTargets(w, 15, 5, 1, first)
	if len(targets) != 1 || targets[0].Target != enemy {
		t.Fatalf("nearest targets = %#v, want only enemy %d", targets, enemy)
	}
}

func TestPlayerCommandsMutateOnlyAddressedCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	energy := NewEnergySystem(w).(*EnergySystem)
	heat := NewHeatSystem(w).(*HeatSystem)
	shield := NewShieldSystem(w).(*ShieldSystem)
	boost := NewBoostSystem(w).(*BoostSystem)
	weapon := NewWeaponSystem(w).(*WeaponSystem)

	// Zero is invalid rather than an alias for the local cursor.
	energy.HandleEvent(event.GameEvent{Type: event.EventEnergySetRequest, Payload: &event.EnergySetPayload{Value: 99}})
	heat.HandleEvent(event.GameEvent{Type: event.EventHeatSetRequest, Payload: &event.HeatSetRequestPayload{Value: 88}})
	shield.HandleEvent(event.GameEvent{Type: event.EventShieldActivate, Payload: &event.ShieldActivatePayload{}})
	boost.HandleEvent(event.GameEvent{Type: event.EventBoostActivate, Payload: &event.BoostActivatePayload{Duration: 2 * time.Second}})
	weapon.HandleEvent(event.GameEvent{Type: event.EventWeaponAddRequest, Payload: &event.WeaponAddRequestPayload{Weapon: component.WeaponRod}})

	energy.HandleEvent(event.GameEvent{Type: event.EventEnergySetRequest, Payload: &event.EnergySetPayload{Entity: second, Value: 42}})
	heat.HandleEvent(event.GameEvent{Type: event.EventHeatSetRequest, Payload: &event.HeatSetRequestPayload{Entity: second, Value: 37}})
	shield.HandleEvent(event.GameEvent{Type: event.EventShieldActivate, Payload: &event.ShieldActivatePayload{Entity: second}})
	boost.HandleEvent(event.GameEvent{Type: event.EventBoostActivate, Payload: &event.BoostActivatePayload{Entity: second, Duration: time.Second}})
	weapon.HandleEvent(event.GameEvent{Type: event.EventWeaponAddRequest, Payload: &event.WeaponAddRequestPayload{Entity: second, Weapon: component.WeaponRod}})

	firstEnergy, _ := w.Components.Energy.GetComponent(first)
	secondEnergy, _ := w.Components.Energy.GetComponent(second)
	if firstEnergy.Current != 0 || secondEnergy.Current != 42 {
		t.Fatalf("energy = (%d, %d), want (0, 42)", firstEnergy.Current, secondEnergy.Current)
	}

	firstHeat, _ := w.Components.Heat.GetComponent(first)
	secondHeat, _ := w.Components.Heat.GetComponent(second)
	if firstHeat.Current != 0 || secondHeat.Current != 37 {
		t.Fatalf("heat = (%d, %d), want (0, 37)", firstHeat.Current, secondHeat.Current)
	}

	firstShield, _ := w.Components.Shield.GetComponent(first)
	secondShield, _ := w.Components.Shield.GetComponent(second)
	if firstShield.Active || !secondShield.Active {
		t.Fatalf("shield active = (%t, %t), want (false, true)", firstShield.Active, secondShield.Active)
	}

	firstBoost, _ := w.Components.Boost.GetComponent(first)
	secondBoost, _ := w.Components.Boost.GetComponent(second)
	if firstBoost.Active || !secondBoost.Active || secondBoost.Remaining != time.Second {
		t.Fatalf("boost = (%#v, %#v), want only slot one active", firstBoost, secondBoost)
	}

	firstWeapon, _ := w.Components.Weapon.GetComponent(first)
	secondWeapon, _ := w.Components.Weapon.GetComponent(second)
	if firstWeapon.Charges[component.WeaponRod] != 0 || secondWeapon.Charges[component.WeaponRod] != 1 {
		t.Fatalf("rod charges = (%d, %d), want (0, 1)", firstWeapon.Charges[component.WeaponRod], secondWeapon.Charges[component.WeaponRod])
	}
}

func TestEnemyKillBoostRewardsOnlyCreditedCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	boost := NewBoostSystem(w).(*BoostSystem)

	boost.HandleEvent(event.GameEvent{
		Type: event.EventEnemyKilled,
		Payload: &event.EnemyKilledPayload{
			KillerEntity: second,
			Species:      component.SpeciesDrain,
		},
	})

	firstBoost, _ := w.Components.Boost.GetComponent(first)
	secondBoost, _ := w.Components.Boost.GetComponent(second)
	if firstBoost.Active || !secondBoost.Active || secondBoost.Remaining != parameter.BoostBaseDuration {
		t.Fatalf("boost after first kill = (%#v, %#v), want only credited cursor activated", firstBoost, secondBoost)
	}

	boost.HandleEvent(event.GameEvent{
		Type: event.EventEnemyKilled,
		Payload: &event.EnemyKilledPayload{
			KillerEntity: second,
			Species:      component.SpeciesSwarm,
		},
	})
	boost.HandleEvent(event.GameEvent{
		Type:    event.EventEnemyKilled,
		Payload: &event.EnemyKilledPayload{Species: component.SpeciesEye},
	})

	secondBoost, _ = w.Components.Boost.GetComponent(second)
	wantRemaining := parameter.BoostBaseDuration + parameter.BoostExtensionDuration
	if secondBoost.Remaining != wantRemaining || secondBoost.TotalDuration != wantRemaining {
		t.Fatalf("boost after second kill = %#v, want remaining and total %v", secondBoost, wantRemaining)
	}
}

func TestCombatRecordsCursorDamageCreditOnUnitAndAblativeHeader(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)

	unit := w.CreateEntity()
	w.Positions.SetPosition(unit, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(unit, component.CombatComponent{
		OwnerEntity:      unit,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageCleaner,
	})
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: unit, HitEntity: unit,
		AttackType: component.CombatAttackProjectile,
	})
	unitCombat, _ := w.Components.Combat.GetComponent(unit)
	if unitCombat.HitPoints != 0 || unitCombat.LastDamagedBy != cursor {
		t.Fatalf("unit combat = %#v, want fatal credit for cursor %d", unitCombat, cursor)
	}

	header := w.CreateEntity()
	member := w.CreateEntity()
	w.Positions.SetPosition(header, component.PositionComponent{X: 10, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 10, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type:          component.CompositeTypeAblative,
		MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: component.CombatEntityPylon,
	})
	w.Components.Combat.SetComponent(member, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: component.CombatEntityPylon,
		HitPoints:        parameter.CombatDamageCleaner,
	})
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: header, HitEntity: member,
		AttackType: component.CombatAttackProjectile,
	})

	headerCombat, _ := w.Components.Combat.GetComponent(header)
	memberCombat, _ := w.Components.Combat.GetComponent(member)
	if memberCombat.HitPoints != 0 || memberCombat.LastDamagedBy != cursor || headerCombat.LastDamagedBy != cursor {
		t.Fatalf("ablative combat = header %#v member %#v, want fatal credit for cursor %d", headerCombat, memberCombat, cursor)
	}

	areaTarget := w.CreateEntity()
	w.Positions.SetPosition(areaTarget, component.PositionComponent{X: 12, Y: 5})
	w.Components.Combat.SetComponent(areaTarget, component.CombatComponent{
		OwnerEntity:      areaTarget,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageExplosion,
	})
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: areaTarget, HitEntities: []core.Entity{areaTarget},
		AttackType: component.CombatAttackExplosion,
	})
	areaCombat, _ := w.Components.Combat.GetComponent(areaTarget)
	if areaCombat.HitPoints != 0 || areaCombat.LastDamagedBy != cursor {
		t.Fatalf("area combat = %#v, want fatal credit for cursor %d", areaCombat, cursor)
	}
}

func TestCombatTelemetryAttributesDamageAbsorptionAndChains(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)
	target := w.CreateEntity()
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageCleaner * 3,
	})
	payload := &event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackProjectile,
	}
	combat.applyHitDirect(payload)

	targetCombat, _ := w.Components.Combat.GetPtr(target)
	targetCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
	combat.applyHitDirect(payload)

	reg := w.Resources.Status
	for key, want := range map[string]int64{
		"combat.damage_dealt":             parameter.CombatDamageCleaner,
		"combat.damage_attacker_cursor":   parameter.CombatDamageCleaner,
		"combat.damage_defender_drain":    parameter.CombatDamageCleaner,
		"combat.absorbed_attacker_cursor": parameter.CombatDamageCleaner,
		"combat.absorbed_defender_drain":  parameter.CombatDamageCleaner,
		"combat.chain_followups":          2,
		"combat.chain_depth_total":        2,
		"combat.chain_depth_max":          1,
		"combat.hits_direct":              2,
	} {
		if got := reg.Ints.Get(key).Load(); got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
}

func TestCombatClearsStaleCursorCreditWhenEnemyDealsFatalDamage(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)

	target := w.CreateEntity()
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageCleaner + parameter.CombatDamageEyeSelfDestruct,
	})
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackProjectile,
	})

	targetCombat, _ := w.Components.Combat.GetComponent(target)
	targetCombat.RemainingDamageImmunity = 0
	w.Components.Combat.SetComponent(target, targetCombat)

	eye := w.CreateEntity()
	w.Components.Combat.SetComponent(eye, component.CombatComponent{
		OwnerEntity:      eye,
		CombatEntityType: component.CombatEntityEye,
	})
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		OwnerEntity: eye, OriginEntity: eye,
		TargetEntity: target, HitEntities: []core.Entity{target},
		AttackType: component.CombatAttackSelfDestruct,
	})

	targetCombat, _ = w.Components.Combat.GetComponent(target)
	if targetCombat.HitPoints != 0 || targetCombat.LastDamagedBy != 0 {
		t.Fatalf("target combat = %#v, want fatal non-cursor damage with no player credit", targetCombat)
	}
}

func TestTowerDeathEmitsKillOnlyForCursorCredit(t *testing.T) {
	w, _, killer := testCursorWorld(t)
	towers := NewTowerSystem(w).(*TowerSystem)

	header := w.CreateEntity()
	w.Components.Tower.SetComponent(header, component.TowerComponent{
		SpawnX: 7,
		SpawnY: 9,
		Type:   component.TowerCyan,
	})
	w.Components.Combat.SetComponent(header, component.CombatComponent{LastDamagedBy: killer})
	towers.handleTowerDeath(header)

	kills := 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type != event.EventEnemyKilled {
			continue
		}
		payload, ok := ev.Payload.(*event.EnemyKilledPayload)
		if !ok || payload.Entity != header || payload.KillerEntity != killer || payload.Species != component.SpeciesTower {
			t.Fatalf("tower kill payload = %#v", ev.Payload)
		}
		kills++
	}
	if kills != 1 {
		t.Fatalf("tower kill events = %d, want 1", kills)
	}

	uncredited := w.CreateEntity()
	w.Components.Tower.SetComponent(uncredited, component.TowerComponent{SpawnX: 3, SpawnY: 4})
	w.Components.Combat.SetComponent(uncredited, component.CombatComponent{})
	towers.handleTowerDeath(uncredited)
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type == event.EventEnemyKilled {
			t.Fatalf("uncredited tower death emitted enemy kill: %#v", ev.Payload)
		}
	}
}

func TestBoostRewardSurvivesStaleTypingDecision(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	boost := NewBoostSystem(w).(*BoostSystem)
	typing := NewTypingSystem(w).(*TypingSystem)

	// Pass one: the keystroke is dispatched ahead of the kills it shares a batch with.
	typing.applyUniversalRewards(cursor)
	for range 3 {
		boost.HandleEvent(event.GameEvent{
			Type:    event.EventEnemyKilled,
			Payload: &event.EnemyKilledPayload{KillerEntity: cursor, Species: component.SpeciesDrain},
		})
	}

	want := parameter.BoostBaseDuration + 2*parameter.BoostExtensionDuration
	got, _ := w.Components.Boost.GetComponent(cursor)
	if got.Remaining != want {
		t.Fatalf("boost after three kills = %v, want %v", got.Remaining, want)
	}

	// Pass two: the typing reward must extend the live boost, never truncate it.
	for _, ev := range w.Resources.Event.Queue.Consume() {
		boost.HandleEvent(ev)
	}
	want += parameter.BoostExtensionDuration
	got, _ = w.Components.Boost.GetComponent(cursor)
	if got.Remaining != want {
		t.Fatalf("boost after typing reward = %v, want %v", got.Remaining, want)
	}
}

func TestDirectDamageAppliesExactlyOnce(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)

	target := w.CreateEntity()
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatInitialHPDrain,
	})

	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackProjectile,
	})

	got, _ := w.Components.Combat.GetComponent(target)
	want := parameter.CombatInitialHPDrain - parameter.CombatDamageCleaner
	if got.HitPoints != want {
		t.Fatalf("hit points = %d, want %d (one application of %d)",
			got.HitPoints, want, parameter.CombatDamageCleaner)
	}
	if n := combat.statDamage.Load(); n != int64(parameter.CombatDamageCleaner) {
		t.Fatalf("combat.damage_dealt = %d, want %d", n, parameter.CombatDamageCleaner)
	}
}
