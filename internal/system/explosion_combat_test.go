package system

import (
	"slices"
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

// Every glyph below sits inside the resulting blast, so the pass over the field
// and the pass over the blast are told apart by what each one admits.
func TestSpecialAttackConvertsDarkGlyphsThenChainsThroughTheBlast(t *testing.T) {
	for _, tc := range []struct {
		name   string
		energy int64
		heat   int
	}{
		{"positive overheat", 50, 156},
		{"negative", -50, 100},
		{"zero energy", 0, 1},
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

			var excluded, chained, flashed []core.Entity
			wantCenters := make(map[event.ExplosionCenterEntry]bool)
			wantChainCells := make(map[event.ExplosionCenterEntry]bool)
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
				cell := event.ExplosionCenterEntry{X: 8 + i, Y: 5}
				switch {
				case g.shared || g.member || g.dying:
					excluded = append(excluded, e)
				case g.level != component.GlyphDark:
					chained = append(chained, e)
					wantChainCells[cell] = true
				case g.typ == component.GlyphGreen || g.typ == polarity:
					wantCenters[cell] = true
				default:
					flashed = append(flashed, e)
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

			dust.HandleEvent(event.GameEvent{Type: event.EventFireSpecialRequest,
				Payload: &event.FireSpecialRequestPayload{Entity: cursor}})
			spends, blasts, deaths := 0, 0, 0
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
				case event.EventDeathBatch:
					deaths++
					p := ev.Payload.(*event.DeathRequestPayload)
					if p.EffectEvent != event.EventFlashSpawnOneRequest || !slices.Equal(p.Entities, flashed) {
						t.Fatalf("flashes = %+v, want the caught dark glyphs %v", p, flashed)
					}
					event.ReleaseDeathRequest(p)
				case event.EventDustSpawnBatchRequest, event.EventDustSpawnOneRequest:
					t.Fatal("special attack deferred a conversion")
				}
			}
			if spends != 1 || blasts != 1 || deaths != 1 {
				t.Fatalf("spends=%d blasts=%d deaths=%d", spends, blasts, deaths)
			}

			// Glyphs the blast caught are the dust the next special attack detonates
			for _, e := range w.Components.Dust.Entities() {
				pos, _ := w.Positions.GetPosition(e)
				cell := event.ExplosionCenterEntry{X: pos.X, Y: pos.Y}
				if !wantChainCells[cell] {
					t.Fatalf("chained dust at %+v, want a converted glyph cell", cell)
				}
				delete(wantChainCells, cell)
			}
			if len(wantChainCells) != 0 {
				t.Fatalf("blast left %d caught glyph cells without dust", len(wantChainCells))
			}
			for _, e := range chained {
				if w.Components.Glyph.HasEntity(e) {
					t.Fatalf("caught glyph %d was not converted", e)
				}
			}
			for _, e := range slices.Concat(excluded, flashed) {
				if !w.Components.Glyph.HasEntity(e) {
					t.Fatalf("glyph %d was destroyed instead of skipped or flashed", e)
				}
			}
			h, _ := w.Components.Heat.GetComponent(cursor)
			if total := h.Current + h.Overheat; total != tc.heat-1 {
				t.Fatalf("heat = %+v, want total %d", h, tc.heat-1)
			}
		})
	}
}

// A special attack that cannot be paid for, or has nothing to detonate, is inert.
func TestSpecialAttackWithoutHeatOrDustChangesNothing(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	heat := NewHeatSystem(w).(*HeatSystem)
	dust := NewDustSystem(w).(*DustSystem)
	w.Components.Energy.SetComponent(cursor, component.EnergyComponent{Current: 50})

	glyph := w.CreateEntity(core.DomainPlayer)
	w.Components.Glyph.SetComponent(glyph, component.GlyphComponent{Rune: 'x', Type: component.GlyphBlue})
	w.Positions.SetPosition(glyph, component.PositionComponent{X: 8, Y: 5})
	fire := event.GameEvent{Type: event.EventFireSpecialRequest,
		Payload: &event.FireSpecialRequestPayload{Entity: cursor}}

	dust.HandleEvent(fire)
	if evs := w.Resources.Event.Queue.Consume(); len(evs) != 0 || !w.Components.Glyph.HasEntity(glyph) {
		t.Fatalf("heat 0 emitted %d events, eligible glyph alive = %v", len(evs), w.Components.Glyph.HasEntity(glyph))
	}

	heat.setHeat(cursor, 10)
	w.DestroyEntity(glyph)
	dust.HandleEvent(fire)
	if evs := w.Resources.Event.Queue.Consume(); len(evs) != 0 {
		t.Fatalf("dustless special attack emitted %d events", len(evs))
	}
	if h, _ := w.Components.Heat.GetComponent(cursor); h.Current != 10 {
		t.Fatalf("heat = %+v, want 10 unspent", h)
	}
}
