package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// TestExplosionCombatDoesNotDependOnVisualMergeState deliberately gives one
// instance the nearby center that used to absorb the request. Presentation is
// player-domain state; it must not decide whether shared combat is emitted.
func TestExplosionCombatDoesNotDependOnVisualMergeState(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	explosion := NewExplosionSystem(w).(*ExplosionSystem)

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 7, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 7, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type: component.CompositeTypeUnit, MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity: header, CombatEntityType: component.CombatEntityStorm, HitPoints: 10,
	})

	// This is a legitimate difference between participants once remote explosion
	// visuals are local. In the old coupled implementation it suppresses combat.
	w.Resources.Transient.ExplosionBacking[0] = engine.ExplosionCenter{
		X: 7, Y: 5, Radius: 4, Intensity: 1, DurNano: 1_000_000_000,
		Type: event.ExplosionTypeMissile,
	}
	w.Resources.Transient.ExplosionCount = 1

	explosion.HandleEvent(event.GameEvent{
		Type: event.EventExplosionRequest,
		Payload: &event.ExplosionRequestPayload{
			Entity: cursor, X: 7, Y: 5, Radius: 4,
			Attack: component.CombatAttackMissile,
		},
	})

	events := w.Resources.Event.Queue.Consume()
	if len(events) != 1 || events[0].Type != event.EventCombatAttackAreaRequest {
		t.Fatalf("derived events = %#v, want one shared area attack despite the local visual center", events)
	}
	p, ok := events[0].Payload.(*event.CombatAttackAreaRequestPayload)
	if !ok || p.TargetEntity != header || len(p.HitEntities) != 1 || p.HitEntities[0] != member {
		t.Fatalf("area attack = %#v, want header %d member %d", events[0].Payload, header, member)
	}
}

// TestExplosionHitsCarryTheArtifactIdentity is D-3 one step down the derivation.
// The geometry crosses and its per-target hits are re-derived (D-5), but not at one
// tick: the producer resolves them at once and every receiver a playout lead later.
// Without the artifact's identity on them, their knockback would come from the
// shared stream the two instances stand at different positions in (D-8).
func TestExplosionHitsCarryTheArtifactIdentity(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	explosion := NewExplosionSystem(w).(*ExplosionSystem)
	id := event.CrossingID{CrossingSource: 2, CrossingSeq: 9}

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 7, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 7, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type: component.CompositeTypeUnit, MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity: header, CombatEntityType: component.CombatEntityStorm, HitPoints: 10,
	})

	explosion.HandleEvent(event.GameEvent{
		Type: event.EventExplosionRequest,
		Payload: &event.ExplosionRequestPayload{
			CrossingID: id, Entity: cursor, X: 7, Y: 5, Radius: 4,
			Attack: component.CombatAttackExplosion,
		},
	})

	events := w.Resources.Event.Queue.Consume()
	if len(events) != 1 {
		t.Fatalf("derived events = %#v, want one shared area attack", events)
	}
	p, ok := events[0].Payload.(*event.CombatAttackAreaRequestPayload)
	if !ok || p.CrossingID != id {
		t.Fatalf("derived hit identity = %#v, want the explosion's %#v", events[0].Payload, id)
	}
}

func TestSpecialAttackConvertsBeforeDetonationAndSpendsOnlyForABlast(t *testing.T) {
	for _, tc := range []struct {
		name   string
		energy int64
		heat   int
	}{
		{"positive overheat", 50, 156},
		{"negative", -50, 100},
		{"zero energy", 0, 1},
		{"zero heat", 50, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, cursor, _ := testCursorWorld(t)
			heat := NewHeatSystem(w).(*HeatSystem)
			heat.setHeat(cursor, tc.heat)
			w.Components.Energy.SetComponent(cursor, component.EnergyComponent{Current: tc.energy})
			dust := NewDustSystem(w).(*DustSystem)
			polarity := component.GlyphBlue
			if tc.energy < 0 {
				polarity = component.GlyphRed
			}

			var retained []core.Entity
			wantCenters := make(map[event.ExplosionCenterEntry]bool)
			for i, g := range []struct {
				typ    component.GlyphType
				level  component.GlyphLevel
				shared bool
				member bool
				dying  bool
			}{
				{typ: component.GlyphGreen}, {typ: component.GlyphBlue}, {typ: component.GlyphRed},
				{typ: component.GlyphWhite}, {typ: component.GlyphGold},
				{typ: polarity, level: component.GlyphNormal},
				{typ: polarity, level: component.GlyphBright},
				{typ: polarity, shared: true}, {typ: polarity, member: true}, {typ: polarity, dying: true},
			} {
				domain := core.DomainPlayer
				if g.shared {
					domain = core.DomainShared
				}
				e := w.CreateEntity(domain)
				w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'x', Type: g.typ, Level: g.level})
				w.Positions.SetPosition(e, component.PositionComponent{X: 8 + i, Y: 5})
				if g.member {
					w.Components.Member.SetComponent(e, component.MemberComponent{HeaderEntity: cursor})
				}
				if g.dying {
					w.Components.Death.SetComponent(e, component.DeathComponent{})
				}
				if !g.shared && !g.member && !g.dying && g.level == component.GlyphDark &&
					(g.typ == component.GlyphGreen || g.typ == polarity) {
					wantCenters[event.ExplosionCenterEntry{X: 8 + i, Y: 5}] = true
				} else {
					retained = append(retained, e)
				}
			}
			// Co-located old dust is consumed once, alongside newly converted dust.
			oldDust := 0
			if tc.heat == 156 {
				dust.spawnDust(30, 15, 'a', component.GlyphNormal, 5, 5)
				dust.spawnDust(30, 15, 'b', component.GlyphBright, 5, 5)
				wantCenters[event.ExplosionCenterEntry{X: 30, Y: 15}] = true
				oldDust = 2
			}
			remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
			dust.HandleEvent(event.GameEvent{Type: event.EventFireSpecialRequest,
				Payload: &event.FireSpecialRequestPayload{Entity: remote}})
			if w.Components.Dust.CountEntities() != oldDust || len(w.Resources.Event.Queue.Consume()) != 0 {
				t.Fatal("remote cursor detonated local dust")
			}

			fire := event.GameEvent{Type: event.EventFireSpecialRequest,
				Payload: &event.FireSpecialRequestPayload{Entity: cursor}}
			dust.HandleEvent(fire)
			spends, blasts := 0, 0
			for _, ev := range w.Resources.Event.Queue.Consume() {
				switch ev.Type {
				case event.EventHeatSpendRequest:
					spends++
					heat.HandleEvent(ev)
				case event.EventExplosionBatchRequest:
					blasts++
					p := ev.Payload.(*event.ExplosionBatchRequestPayload)
					if p.Entity != cursor || len(p.Centers) != len(wantCenters) {
						t.Fatalf("blast = %+v, want owner %d and %d centers", p, cursor, len(wantCenters))
					}
					for _, center := range p.Centers {
						if !wantCenters[center] {
							t.Fatalf("unexpected/duplicate center %+v", center)
						}
						delete(wantCenters, center)
					}
					event.ReleaseExplosionBatchRequest(p)
				case event.EventDustSpawnBatchRequest, event.EventDustSpawnOneRequest, event.EventDeathBatch:
					t.Fatal("special attack deferred a conversion or destroyed an ineligible glyph")
				}
			}
			if spends != 1 || blasts != 1 || w.Components.Dust.CountEntities() != 0 {
				t.Fatalf("spends=%d blasts=%d dust=%d", spends, blasts, w.Components.Dust.CountEntities())
			}
			h, _ := w.Components.Heat.GetComponent(cursor)
			if total := h.Current + h.Overheat; total != max(0, tc.heat-1) {
				t.Fatalf("heat = %+v, want total %d", h, max(0, tc.heat-1))
			}
			if w.Components.Glyph.CountEntities() != len(retained) {
				t.Fatal("converted glyphs survived")
			}
			for _, e := range retained {
				if !w.Components.Glyph.HasEntity(e) {
					t.Fatalf("ineligible glyph %d was destroyed by nearby blast", e)
				}
			}
			w.Resources.Event.Queue.Consume()
			dust.HandleEvent(fire)
			if evs := w.Resources.Event.Queue.Consume(); len(evs) != 0 {
				t.Fatalf("empty special attack emitted %d events", len(evs))
			}
		})
	}
}
