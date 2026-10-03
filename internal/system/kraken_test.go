package system

import (
	"math"
	"testing"
	"time"

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
	w.SetupLevel(180, 90, false, false, false)
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

func TestKrakenWaitsThenAttacksOrSpinsBeforeAimedCharge(t *testing.T) {
	w, s, e, _ := krakenFixture(t)
	k, _ := w.Components.Kraken.GetPtr(e)
	motion, _ := w.Components.Kinetic.GetPtr(e)
	first, second := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	w.Positions.SetPosition(first, component.PositionComponent{X: 0, Y: 0})
	w.Positions.SetPosition(second, component.PositionComponent{X: 179, Y: 89})
	attacks, charges, targets := 0, 0, map[vmath.Point]bool{}
	previous, repeats, previousLegs := component.KrakenIdle, 0, -1
	for i := range 64 {
		s.rng.Reseed(uint64(i + 1))
		s.wait(k)
		if k.StateRemaining < parameter.KrakenWaitMin || k.StateRemaining > parameter.KrakenWaitMax {
			t.Fatalf("idle duration %v outside wait bounds", k.StateRemaining)
		}
		s.animate(k, motion, 0.05)
		if motion.PreciseX != 90.5 || motion.PreciseY != 45.5 {
			t.Fatal("idle moved the body")
		}
		s.chooseState(k, motion.PreciseX, motion.PreciseY)
		if k.State == previous {
			repeats++
		} else {
			previous, repeats = k.State, 1
		}
		if repeats > 2 {
			t.Fatal("Kraken repeated the same action more than twice")
		}
		switch k.State {
		case component.KrakenAttack:
			attacks++
			if k.AttackLegs == previousLegs {
				t.Fatal("consecutive leg attacks reused the same four legs")
			}
			previousLegs = k.AttackLegs
		case component.KrakenSpin:
			charges++
			if k.StateRemaining != parameter.KrakenSpinDuration || !s.bodyFits(k.TargetX, k.TargetY) {
				t.Fatalf("invalid spin or padded target: %+v", k)
			}
			targets[vmath.PointAtF(k.TargetX, k.TargetY)] = true
			x, y := k.TargetX, k.TargetY
			s.chooseState(k, motion.PreciseX, motion.PreciseY)
			if k.State != component.KrakenMove || k.TargetX != x || k.TargetY != y {
				t.Fatal("spin did not charge at its locked target")
			}
		default:
			t.Fatalf("idle entered state %v", k.State)
		}
		s.chooseState(k, motion.PreciseX, motion.PreciseY)
		if k.State != component.KrakenIdle {
			t.Fatal("action did not return to the common wait")
		}
	}
	if attacks == 0 || charges == 0 || len(targets) != 2 {
		t.Fatalf("attacks=%d charges=%d cursor targets=%v", attacks, charges, targets)
	}
	for y := range 90 {
		spawnWall(w, 105, y)
	}
	w.Positions.RemoveEntity(first)
	s.aimCharge(k, 90.5, 45.5)
	if k.TargetX <= 105 || !s.bodyFits(k.TargetX, k.TargetY) {
		t.Fatalf("wall prevented a bounds-padded charge: (%f,%f)", k.TargetX, k.TargetY)
	}
}

func TestKrakenWindupKeepsSpinningUntilItsLockedCharge(t *testing.T) {
	w, s, e, _ := krakenFixture(t)
	k, _ := w.Components.Kraken.GetPtr(e)
	motion, _ := w.Components.Kinetic.GetPtr(e)
	k.LastAction, k.ActionStreak, k.StateRemaining = component.KrakenAttack, 2, 0
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	s.Update()
	if k.State != component.KrakenSpin {
		t.Fatal("charge did not start with a spin")
	}
	x, y, tx, ty := motion.PreciseX, motion.PreciseY, k.TargetX, k.TargetY
	for slot := range 2 {
		w.Positions.SetPosition(w.Resources.Player.Slot(uint8(slot)), component.PositionComponent{X: 170, Y: 80})
	}
	rotation := 0.0
	for elapsed := parameter.GameUpdateInterval; elapsed < parameter.KrakenSpinDuration; elapsed += parameter.GameUpdateInterval {
		angle := k.Angle
		w.Resources.Game.State.IncrementGameTicks()
		s.Update()
		rotation += math.Abs(math.Remainder(k.Angle-angle, 2*math.Pi))
		if k.State != component.KrakenSpin || motion.PreciseX != x || motion.PreciseY != y {
			t.Fatal("spin ended or moved before its wind-up completed")
		}
		if elapsed >= time.Second && math.Abs(k.RotSpeed) < parameter.KrakenSpinRotSpeed*0.9 {
			t.Fatal("spin lost its rotation before the charge")
		}
	}
	if rotation < 2*math.Pi {
		t.Fatal("wind-up did not visibly rotate at least a full turn")
	}
	maxBend, leg := 0.0, -1
	k.TentacleSamples(x, y, func(lx, ly, _, step float64, _ bool) {
		if step == 0 {
			leg++
		}
		if step < 0.95 {
			return
		}
		base := k.Angle + float64(leg)*math.Pi/4
		bend := math.Abs(math.Remainder(math.Atan2((ly-y)*2, lx-x)-base, 2*math.Pi))
		maxBend = max(maxBend, bend)
	})
	if maxBend > math.Pi/4 {
		t.Fatal("spinning tentacles curved behind the adjacent leg")
	}
	direction, speed := k.TurnDir, math.Abs(k.RotSpeed)
	s.Update()
	if k.State != component.KrakenMove || (motion.PreciseX == x && motion.PreciseY == y) || k.TargetX != tx || k.TargetY != ty {
		t.Fatal("spin failed to move immediately toward the locked cursor position")
	}
	for k.State == component.KrakenMove {
		if k.TurnDir != direction || k.RotSpeed*direction <= 0 || math.Abs(k.RotSpeed) < speed*0.99 {
			t.Fatal("charge interrupted or slowed the wind-up's rotation")
		}
		s.Update()
	}
}

func TestKrakenChargeTargetsThroughWallsAndSkipsCoveredCursors(t *testing.T) {
	w, s, e, _ := krakenFixture(t)
	k, _ := w.Components.Kraken.GetPtr(e)
	motion, _ := w.Components.Kinetic.GetPtr(e)
	first, second := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	for y := range 90 {
		spawnWall(w, 100, y)
	}
	w.Positions.SetPosition(first, component.PositionComponent{X: 90, Y: 45})
	w.Positions.SetPosition(second, component.PositionComponent{X: 120, Y: 70})
	for seed := range 8 {
		s.rng.Reseed(uint64(seed + 1))
		if !s.aimCharge(k, motion.PreciseX, motion.PreciseY) || k.TargetX != 120.5 || k.TargetY != 70.5 {
			t.Fatal("covered cursor prevented selecting a useful charge through walls")
		}
	}
	k.State, k.StateRemaining = component.KrakenMove, parameter.KrakenMoveDuration
	for range 20 {
		s.animate(k, motion, parameter.GameUpdateInterval.Seconds())
	}
	if motion.PreciseX != k.TargetX || motion.PreciseY != k.TargetY {
		t.Fatal("charge stopped before reaching the cursor across the wall")
	}
	w.Positions.RemoveEntity(first)
	k.State, k.LastAction, k.ActionStreak = component.KrakenIdle, component.KrakenAttack, 2
	s.chooseState(k, motion.PreciseX, motion.PreciseY)
	if k.State != component.KrakenAttack {
		t.Fatal("covered cursor should receive a leg attack, not an empty spin/wait")
	}
}

func TestKrakenDestroysWallsAtSpawnAndAcrossItsFootprint(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	w.SetupLevel(180, 90, false, false, false)
	s := NewKrakenSystem(w).(*KrakenSystem)
	walls, death := NewWallSystem(w).(*WallSystem), NewDeathSystem(w).(*DeathSystem)
	var leg vmath.Point
	shape := component.KrakenComponent{}
	shape.TentacleSamples(90.5, 45.5, func(x, y, _, step float64, _ bool) {
		if leg == (vmath.Point{}) && step >= 0.5 {
			leg = vmath.PointAtF(x, y)
		}
	})
	for _, cell := range []vmath.Point{{X: 90, Y: 45}, leg, {X: 150, Y: 80}} {
		spawnWall(w, cell.X, cell.Y)
	}
	decorative := w.CreateEntity(core.DomainShared)
	w.Components.Wall.SetComponent(decorative, component.WallComponent{})
	w.Positions.SetPosition(decorative, component.PositionComponent{X: 90, Y: 46})
	s.spawn(&event.KrakenSpawnRequestPayload{X: 90, Y: 45})
	if w.Components.Kraken.CountEntities() != 1 {
		t.Fatal("walls prevented Kraken from spawning")
	}
	e := w.Components.Kraken.Entities()[0]
	walls.pushEntitiesAtPosition(90, 45)
	pos, _ := w.Positions.GetPosition(e)
	if pos.X != 90 || pos.Y != 45 {
		t.Fatal("walls displaced Kraken from its requested spawn center")
	}
	settle := func() {
		for range 12 {
			evs := w.Resources.Event.Queue.Consume()
			if len(evs) == 0 {
				return
			}
			for _, ev := range evs {
				walls.HandleEvent(ev)
				death.HandleEvent(ev)
			}
		}
		t.Fatal("wall destruction did not settle")
	}
	settle()
	for _, cell := range []vmath.Point{{X: 90, Y: 45}, leg, {X: 90, Y: 46}} {
		if w.Positions.HasBlockingWallAt(cell.X, cell.Y, 0) {
			t.Fatalf("spawn footprint left wall at %+v", cell)
		}
	}
	spawnWall(w, 125, 45)
	k, _ := w.Components.Kraken.GetPtr(e)
	k.State, k.StateRemaining, k.TargetX, k.TargetY = component.KrakenMove, parameter.KrakenMoveDuration, 140.5, 45.5
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	for range 20 {
		s.Update()
		settle()
	}
	motion, _ := w.Components.Kinetic.GetComponent(e)
	if motion.PreciseX < 125 || w.Positions.HasBlockingWallAt(125, 45, 0) || !w.Positions.HasBlockingWallAt(150, 80, 0) {
		t.Fatal("charge failed to demolish its path or destroyed a wall outside its footprint")
	}
}

func TestKrakenConvertsGlyphsAndDestroysWholeGoldAndNuggets(t *testing.T) {
	w, s, e, leg := krakenFixture(t)
	gold := NewGoldSystem(w).(*GoldSystem)
	nuggets := NewNuggetSystem(w).(*NuggetSystem)
	death := NewDeathSystem(w).(*DeathSystem)
	composite := NewCompositeSystem(w).(*CompositeSystem)
	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: leg.X, Y: leg.Y})
	var members []component.MemberEntry
	for i, ch := range "AZ" {
		member := w.CreateEntity(core.DomainShared)
		w.Components.Glyph.SetComponent(member, component.GlyphComponent{Rune: ch, Type: component.GlyphGold})
		w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
		w.Positions.SetPosition(member, component.PositionComponent{X: leg.X + i*20, Y: leg.Y})
		members = append(members, component.MemberEntry{Entity: member, OffsetX: i * 20})
	}
	w.Components.Header.SetComponent(header, component.HeaderComponent{Type: component.CompositeTypeContainer, Behavior: component.BehaviorGold, MemberEntries: members})
	gold.headerEntity, gold.active = header, true
	nugget := w.CreateEntity(core.DomainPlayer)
	w.Components.Nugget.SetComponent(nugget, component.NuggetComponent{})
	w.Positions.SetPosition(nugget, component.PositionComponent{X: leg.X, Y: leg.Y})
	nuggets.activeNuggetEntity = nugget
	for _, typ := range []component.GlyphType{component.GlyphGreen, component.GlyphRed, component.GlyphBlue, component.GlyphWhite} {
		glyph := w.CreateEntity(core.DomainPlayer)
		w.Components.Glyph.SetComponent(glyph, component.GlyphComponent{Rune: 'a' + rune(typ), Type: typ})
		w.Positions.SetPosition(glyph, component.PositionComponent{X: leg.X, Y: leg.Y})
	}
	s.interact(e)
	decays, goldDeaths := 0, 0
	for range 12 {
		evs := w.Resources.Event.Queue.Consume()
		if len(evs) == 0 {
			break
		}
		for _, ev := range evs {
			if ev.Type == event.EventParticleSpawnBatch {
				p := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry])
				for _, entry := range p.Entries {
					if entry.Behavior != component.ParticleDecay || !entry.SkipStartCell {
						t.Fatalf("glyph did not become decay: %+v", entry)
					}
					decays++
				}
			}
			if ev.Type == event.EventGoldDestroyed {
				goldDeaths++
			}
			gold.HandleEvent(ev)
			nuggets.HandleEvent(ev)
			composite.HandleEvent(ev)
			death.HandleEvent(ev)
		}
		composite.Update()
	}
	if decays != 4 || goldDeaths != 1 || w.Components.Glyph.CountEntities() != 0 || w.Components.Nugget.HasEntity(nugget) || nuggets.activeNuggetEntity != 0 {
		t.Fatalf("decays=%d gold deaths=%d glyphs=%d nugget=%d", decays, goldDeaths, w.Components.Glyph.CountEntities(), nuggets.activeNuggetEntity)
	}
}

func TestKrakenCleanerAndGlyphExplosionsSpendSeparatePlayerWeaponBudgets(t *testing.T) {
	w, _, kraken, leg := krakenFixture(t)
	cleaner := NewCleanerSystem(w).(*CleanerSystem)
	combat := NewCombatSystem(w).(*CombatSystem)
	explosion := NewExplosionSystem(w).(*ExplosionSystem)
	dust := NewDustSystem(w).(*DustSystem)
	heat := NewHeatSystem(w).(*HeatSystem)
	settle := func() {
		for range 10 {
			evs := w.Resources.Event.Queue.Consume()
			if len(evs) == 0 {
				break
			}
			for _, ev := range evs {
				explosion.HandleEvent(ev)
				combat.HandleEvent(ev)
				heat.HandleEvent(ev)
			}
		}
	}
	hp, _ := w.Components.Combat.GetPtr(kraken)
	before := hp.HitPoints
	for slot := range 2 {
		cursor := w.Resources.Player.Slot(uint8(slot))
		w.Components.Energy.SetComponent(cursor, component.EnergyComponent{Current: 50})
		heat.setHeat(cursor, 10)
		cleaner.checkCollisions(leg.X, leg.Y, 0, cursor, 1, 0, component.CleanerColorPositive)
		settle()
		if hp.HitPoints != before-parameter.CombatDamageCleaner {
			t.Fatalf("player %d cleaner damage=%d, want %d", slot, before-hp.HitPoints, parameter.CombatDamageCleaner)
		}
		before = hp.HitPoints
		combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
			OwnerEntity: cursor, OriginEntity: cursor, TargetEntity: kraken, HitEntity: kraken, AttackType: component.CombatAttackLightning,
		})
		if hp.HitPoints != before-parameter.CombatDamageRod {
			t.Fatal("cleaner follow-up consumed the rod's damage allowance")
		}
		before = hp.HitPoints
		glyph := w.CreateEntity(core.DomainPlayer)
		w.Components.Glyph.SetComponent(glyph, component.GlyphComponent{Rune: 'x', Type: component.GlyphGreen})
		w.Positions.SetPosition(glyph, component.PositionComponent{X: leg.X - 2, Y: leg.Y})
		dust.HandleEvent(event.GameEvent{Type: event.EventFireSpecialRequest, Payload: &event.FireSpecialRequestPayload{Entity: cursor}})
		settle()
		if hp.HitPoints >= before || w.Components.Glyph.HasEntity(glyph) {
			t.Fatalf("player %d glyph explosion lost its damage allowance", slot)
		}
		before = hp.HitPoints
		cleaner.checkCollisions(leg.X, leg.Y, 0, cursor, 1, 0, component.CleanerColorPositive)
		explosion.HandleEvent(event.GameEvent{Type: event.EventExplosionRequest, Payload: &event.ExplosionRequestPayload{
			Entity: cursor, X: leg.X, Y: leg.Y, Radius: 4, Attack: component.CombatAttackExplosion,
		}})
		settle()
		if hp.HitPoints != before {
			t.Fatal("repeat cleaner or explosion bypassed its own allowance")
		}
	}
	hp.SealDamageImmunity(parameter.CombatDamageImmunityDuration)
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: w.Resources.Player.Slot(0), TargetEntity: kraken, HitEntity: kraken, AttackType: component.CombatAttackBullet,
	})
	if hp.HitPoints != before {
		t.Fatal("weapon separation bypassed species-authored invulnerability")
	}
	w.Resources.Time.DeltaTime = parameter.CombatDamageImmunityDuration
	combat.Update()
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: w.Resources.Player.Slot(0), TargetEntity: kraken, HitEntity: kraken, AttackType: component.CombatAttackBullet,
	})
	if hp.HitPoints != before-parameter.CombatDamageBullet {
		t.Fatal("weapon allowance did not reopen after immunity expired")
	}
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

func TestKrakenOnlyBodyStopsAtMapEdges(t *testing.T) {
	w, s, e, leg := krakenFixture(t)
	_, member, _ := CombatTargetAt(w, leg.X, leg.Y, engine.ScopeShared, 0, 0)
	spawnWall(w, leg.X, leg.Y)
	walls := NewWallSystem(w).(*WallSystem)
	walls.pushEntitiesAtPosition(leg.X, leg.Y)
	if pos, ok := w.Positions.GetPosition(member); !ok || pos.X != leg.X || pos.Y != leg.Y {
		t.Fatal("wall displaced a Kraken leg")
	}
	motion, _ := w.Components.Kinetic.GetPtr(e)
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
	motion.PreciseX, motion.PreciseY = 170.5, 80.5
	w.SetupLevel(80, 30, false, false, false)
	s.Update()
	if !s.bodyFits(motion.PreciseX, motion.PreciseY) {
		t.Fatal("map shrink left Kraken's body out of bounds")
	}
	for _, cell := range s.cells {
		if w.Positions.IsOutOfBounds(cell.X, cell.Y) {
			t.Fatal("map shrink left an off-map interaction cell")
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
