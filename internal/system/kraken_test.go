package system

import (
	"math"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func krakenFixture(t *testing.T) (*engine.World, *KrakenSystem, core.Entity, vmath.Point) {
	t.Helper()
	w, _, _ := testCursorWorld(t)
	w.SetupLevel(180, 90, false, false)
	s := NewKrakenSystem(w).(*KrakenSystem)
	s.spawn(&event.KrakenSpawnRequestPayload{X: 90, Y: 45})
	if w.Components.Kraken.CountEntities() != 1 {
		t.Fatal("Kraken did not spawn")
	}
	e := w.Components.Kraken.Entities()[0]
	k, _ := w.Components.Kraken.GetPtr(e)
	k.RotSpeed = 0
	s.syncMembers(e, k, 90.5, 45.5)
	leg := vmath.Point{}
	for _, cell := range s.cells {
		if cell.Y == 45 && cell.X > leg.X {
			leg = cell
		}
	}
	if leg.X <= 98 {
		t.Fatal("fixture needs a tentacle outside the body")
	}
	w.Resources.Event.Queue.Consume()
	return w, s, e, leg
}

func TestKrakenLegInterceptsMissilesAndSharesUnstunnableHealth(t *testing.T) {
	w, _, e, leg := krakenFixture(t)
	cursor := w.Resources.Player.Slot(0)
	target, hit, ok := CombatTargetAt(w, leg.X, leg.Y, engine.ScopeBoth, 0, cursor)
	if !ok || target != e || hit == e {
		t.Fatalf("leg resolved to (%d, %d, %v)", target, hit, ok)
	}
	missiles := NewMissileSystem(w).(*MissileSystem)
	x, y, impact := missiles.traverseForImpact(135.5, 45.5, 90.5, 45.5, &component.MissileComponent{
		Owner: cursor, TargetEntity: e, HitEntity: e,
	})
	if impact != impactCombatant || x <= 98 || y != 45 {
		t.Fatalf("missile passed the leg: impact=(%d,%d,%d)", x, y, impact)
	}
	combat := NewCombatSystem(w).(*CombatSystem)
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		AttackType: component.CombatAttackBullet, OriginEntity: cursor, OwnerEntity: cursor, TargetEntity: e, HitEntity: hit,
	})
	hp, _ := w.Components.Combat.GetPtr(e)
	if hp.HitPoints != parameter.KrakenInitialHP-parameter.CombatDamageBullet || !w.Components.Member.HasEntity(hit) {
		t.Fatalf("leg hit did not damage the intact composite: %+v", hp)
	}
	hp.RemainingDamageImmunity = 0
	before := hp.HitPoints
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		AttackType: component.CombatAttackPulse, OriginEntity: cursor, OwnerEntity: cursor, TargetEntity: e, HitEntities: []core.Entity{hit},
	})
	if hp.HitPoints != before-parameter.CombatDamagePulse || hp.StunnedRemaining != 0 {
		t.Fatalf("pulse must damage without stunning: %+v", hp)
	}
	for _, attack := range []component.CombatAttackType{component.CombatAttackShield, component.CombatAttackExplosion} {
		combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
			AttackType: attack, OriginEntity: cursor, OwnerEntity: cursor, TargetEntity: e, HitEntities: []core.Entity{hit},
		})
	}
	motion, _ := w.Components.Kinetic.GetPtr(e)
	if motion.VelX != 0 || motion.VelY != 0 {
		t.Fatal("Kraken received combat knockback")
	}
}

func TestKrakenOnlyBodyStopsAtWallsAndMapEdges(t *testing.T) {
	w, s, e, leg := krakenFixture(t)
	_, member, _ := CombatTargetAt(w, leg.X, leg.Y, engine.ScopeShared, 0, 0)
	spawnWall(w, leg.X, leg.Y)
	walls := NewWallSystem(w).(*WallSystem)
	walls.pushEntitiesAtPosition(leg.X, leg.Y)
	if pos, ok := w.Positions.GetPosition(member); !ok || pos.X != leg.X || pos.Y != leg.Y {
		t.Fatal("wall displaced a Kraken leg")
	}
	for y := range 90 {
		spawnWall(w, 105, y)
	}
	motion, _ := w.Components.Kinetic.GetPtr(e)
	s.moveBody(motion, 140.5, 45.5)
	if !s.bodyFits(motion.PreciseX, motion.PreciseY) || motion.PreciseX >= 105 {
		t.Fatal("body crossed a wall")
	}
	s.moveBody(motion, -40, -40)
	if !s.bodyFits(motion.PreciseX, motion.PreciseY) {
		t.Fatal("body crossed the map edge")
	}
	k, _ := w.Components.Kraken.GetPtr(e)
	k.State, k.AttackT = component.KrakenAttack, 1
	s.syncMembers(e, k, motion.PreciseX, motion.PreciseY)
	outside := false
	k.TentacleSamples(motion.PreciseX, motion.PreciseY, func(x, y, _, _ float64, _ bool) {
		outside = outside || x < 0 || y < 0
	})
	if !outside {
		t.Fatal("expected unclamped tentacles beyond the map")
	}
	for _, cell := range s.cells {
		if w.Positions.IsOutOfBounds(cell.X, cell.Y) {
			t.Fatal("off-map tentacle got a spatial hitbox")
		}
	}
}

func TestKrakenContactsDamageEachOwnerAndPushOtherSpecies(t *testing.T) {
	w, s, e, leg := krakenFixture(t)
	first, second := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	remote := spawnRemoteCursor(t, w, 2, leg.X, leg.Y, 77)
	for _, cursor := range []core.Entity{first, second, remote} {
		w.Positions.SetPosition(cursor, component.PositionComponent{X: leg.X, Y: leg.Y})
	}
	w.Components.Shield.SetComponent(first, component.ShieldComponent{Active: true, InvRxSq: 1.0 / 16, InvRySq: 1.0 / 4})
	s.interact(e)
	shieldHits, heatHits := 0, 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch p := ev.Payload.(type) {
		case *event.ShieldDrainRequestPayload:
			if p.Entity != first || p.Value != parameter.KrakenShieldDrain {
				t.Fatalf("unexpected shield contact: %+v", p)
			}
			shieldHits++
		case *event.HeatAddRequestPayload:
			if p.Entity != second || p.Delta != -parameter.KrakenDamageHeat {
				t.Fatalf("unexpected heat contact: %+v", p)
			}
			heatHits++
		}
	}
	if shieldHits != 1 || heatHits != 1 {
		t.Fatalf("contacts = (%d,%d), want one of each", shieldHits, heatHits)
	}
	other := w.CreateEntity(core.DomainShared)
	w.Components.Swarm.SetComponent(other, component.SwarmComponent{})
	w.Components.Kinetic.SetComponent(other, component.KineticComponent{})
	w.Components.Combat.SetComponent(other, component.CombatComponent{HitPoints: 100})
	w.Positions.SetPosition(other, component.PositionComponent{X: leg.X, Y: leg.Y})
	soft := NewSoftCollisionSystem(w).(*SoftCollisionSystem)
	soft.Update()
	motion, _ := w.Components.Kinetic.GetPtr(other)
	if math.Hypot(motion.VelX, motion.VelY) == 0 {
		t.Fatal("Kraken tentacle did not push the swarm")
	}
	motion, _ = w.Components.Kinetic.GetPtr(e)
	if motion.VelX != 0 || motion.VelY != 0 {
		t.Fatal("Kraken received a reciprocal impulse")
	}
}

func TestKrakenDeathRetiresWholeMemberPool(t *testing.T) {
	w, s, e, _ := krakenFixture(t)
	k, _ := w.Components.Kraken.GetPtr(e)
	k.State, k.AttackT = component.KrakenAttack, 1
	s.syncMembers(e, k, 90.5, 45.5)
	k.State, k.AttackT = component.KrakenIdle, 0
	s.syncMembers(e, k, 90.5, 45.5)
	header, _ := w.Components.Header.GetComponent(e)
	if len(header.MemberEntries) <= len(s.cells) {
		t.Fatal("fixture needs spare hitbox identities after contraction")
	}
	for _, member := range header.MemberEntries[len(s.cells):] {
		if _, ok := w.Positions.GetPosition(member.Entity); ok {
			t.Fatal("retracted hitbox still occupies a cell")
		}
	}
	hp, _ := w.Components.Combat.GetPtr(e)
	hp.HitPoints, hp.LastDamagedBy = 0, w.Resources.Player.Slot(0)
	s.Update()
	composite, death := NewCompositeSystem(w), NewDeathSystem(w)
	kills := 0
	for round := 0; round < 8; round++ {
		events := w.Resources.Event.Queue.Consume()
		if len(events) == 0 {
			break
		}
		for _, ev := range events {
			if ev.Type == event.EventSpeciesKilled {
				p := ev.Payload.(*event.SpeciesKilledPayload)
				if p.Entity != e || p.Species != component.SpeciesKraken || p.KillerEntity != w.Resources.Player.Slot(0) {
					t.Fatalf("unexpected kill credit: %+v", p)
				}
				kills++
			}
			composite.(*CompositeSystem).HandleEvent(ev)
			death.(*DeathSystem).HandleEvent(ev)
		}
	}
	if kills != 1 || w.Components.Kraken.HasEntity(e) {
		t.Fatal("Kraken death did not retire the root exactly once")
	}
	for _, member := range header.MemberEntries {
		_, positioned := w.Positions.GetPosition(member.Entity)
		if w.Components.Member.HasEntity(member.Entity) || positioned {
			t.Fatal("Kraken death left a live hitbox")
		}
	}
}
