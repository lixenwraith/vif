package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// devourerFixture is a Devourer risen on a 120 by 40 map, its cursors parked in the
// middle the bezel leaves open
func devourerFixture(t *testing.T) (*engine.World, *DevourerSystem, core.Entity) {
	t.Helper()
	w, first, second := testCursorWorld(t)
	w.SetupLevel(120, 40, false, false, false)
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	for _, cursor := range []core.Entity{first, second} {
		w.Positions.SetPosition(cursor, component.PositionComponent{X: 60, Y: 20})
	}
	s := ready(NewDevourerSystem(w)).(*DevourerSystem)
	s.HandleEvent(event.GameEvent{Type: event.EventDevourerSpawnRequest, Payload: &event.DevourerSpawnRequestPayload{}})
	if w.Components.Devourer.CountEntities() != 1 {
		t.Fatal("no Devourer rose")
	}
	w.Resources.Event.Queue.Consume()
	return w, s, w.Components.Devourer.Entities()[0]
}

// stepDevourer runs n ticks of the system alone, returning what they pushed
func stepDevourer(w *engine.World, s *DevourerSystem, n int) (pushed []event.GameEvent) {
	for range n {
		s.Update()
		pushed = append(pushed, w.Resources.Event.Queue.Consume()...)
	}
	return pushed
}

// grown runs a fixture past its creep
func grown(w *engine.World, s *DevourerSystem) {
	stepDevourer(w, s, int(parameter.DevourerCreep/parameter.GameUpdateInterval)+1)
}

// TestADevourerCreepsInFromTheEdgesToABezel: it rises a band along every edge, grows
// from its first frame, and over its creep reaches a bezel its share of the shorter
// side thick, give or take its walls' reach, the middle left open, a member standing
// on each cell it covers
func TestADevourerCreepsInFromTheEdgesToABezel(t *testing.T) {
	w, s, e := devourerFixture(t)
	cfg := w.Resources.Config
	if !s.body.covers(0, cfg.MapHeight/2) || !s.body.covers(cfg.MapWidth/2, 0) || s.body.covers(5, cfg.MapHeight/2) {
		t.Fatal("it did not rise as a band along the edges")
	}
	d, _ := w.Components.Devourer.GetPtr(e)
	for covered, frame := len(s.body.cells), d.Frame(); d.Grown() < 1; {
		if stepDevourer(w, s, 1); d.Frame() == frame {
			continue
		}
		if d.Grown() <= 0.5 && len(s.body.cells) <= covered {
			t.Fatalf("frame %d covers %d cells, the one before %d", d.Frame(), len(s.body.cells), covered)
		}
		covered, frame = len(s.body.cells), d.Frame()
	}
	stepDevourer(w, s, 1)
	bezel := parameter.DevourerBezel * min(float64(cfg.MapWidth), 2*float64(cfg.MapHeight))
	reach := parameter.DevourerSwell + parameter.DevourerBreath
	for _, r := range parameter.DevourerRipples {
		reach += r.Amp
	}
	reach *= bezel
	left, right := int(bezel-reach)-1, int(bezel+reach)+1
	top, bottom := int((bezel-reach)/2)-1, int((bezel+reach)/2)+1
	if !s.body.covers(left, cfg.MapHeight/2) || s.body.covers(right, cfg.MapHeight/2) ||
		!s.body.covers(cfg.MapWidth/2, top) || s.body.covers(cfg.MapWidth/2, bottom) ||
		!s.body.covers(0, 0) || s.body.covers(cfg.MapWidth/2, cfg.MapHeight/2) {
		t.Fatalf("a bezel %.1f thick within %.1f is not what stands", bezel, reach)
	}
	header, _ := w.Components.Header.GetComponent(e)
	standing := 0
	for _, m := range header.MemberEntries {
		if pos, ok := w.Positions.GetPosition(m.Entity); ok {
			standing++
			if !s.body.covers(pos.X, pos.Y) {
				t.Fatalf("a member stands off the body at %d,%d", pos.X, pos.Y)
			}
		}
	}
	if standing != len(s.body.cells) {
		t.Fatalf("%d members stand on %d covered cells", standing, len(s.body.cells))
	}
}

// TestADevourersHitboxStepsWithItsFrame: a tick inside a frame moves no member, and
// each frame's footprint is the shape its frame builds, which moves as frames pass
func TestADevourersHitboxStepsWithItsFrame(t *testing.T) {
	w, s, e := devourerFixture(t)
	grown(w, s)
	d, _ := w.Components.Devourer.GetPtr(e)
	cfg := w.Resources.Config
	positions := func() map[core.Entity]component.PositionComponent {
		header, _ := w.Components.Header.GetComponent(e)
		out := make(map[core.Entity]component.PositionComponent, len(header.MemberEntries))
		for _, m := range header.MemberEntries {
			if pos, ok := w.Positions.GetPosition(m.Entity); ok {
				out[m.Entity] = pos
			}
		}
		return out
	}
	moved := false
	var shape component.DevourerShape
	for range 40 {
		frame, before := d.Frame(), positions()
		stepDevourer(w, s, 1)
		after := positions()
		if d.Frame() == frame {
			for m, pos := range before {
				if after[m] != pos {
					t.Fatalf("frame %d: a tick inside it moved a member", frame)
				}
			}
			continue
		}
		shape.Build(d.Seed, d.Frame(), d.Grown(), cfg.MapWidth, cfg.MapHeight)
		for y := range cfg.MapHeight {
			for x := range cfg.MapWidth {
				if s.body.covers(x, y) != shape.Covers(x, y) {
					t.Fatalf("frame %d: cell %d,%d covered %v, its shape %v", d.Frame(), x, y, s.body.covers(x, y), shape.Covers(x, y))
				}
			}
		}
		moved = moved || len(after) != len(before)
		for m, pos := range before {
			moved = moved || after[m] != pos
		}
	}
	if !moved {
		t.Fatal("twenty frames left the walls where they stood")
	}
}

// TestAnOmenFormsAGutAlongItsRayAndFourAtMost: an omen ends in a gut of its own hit
// points hanging between the walls it named, over the open space alone, and no omen
// lights while four guts stand
func TestAnOmenFormsAGutAlongItsRayAndFourAtMost(t *testing.T) {
	w, s, e := devourerFixture(t)
	grown(w, s)
	d, _ := w.Components.Devourer.GetPtr(e)
	omen := int(parameter.DevourerOmen / parameter.GameUpdateInterval)
	for slot := range parameter.DevourerGutCap {
		d.NextOmen = 0
		stepDevourer(w, s, 1)
		lit := d.Omen
		if lit.Remaining <= 0 {
			t.Fatalf("slot %d: no omen lit", slot)
		}
		stepDevourer(w, s, omen)
		gut := d.Guts[slot]
		g, ok := w.Components.DevourerGut.GetComponent(gut)
		combat, _ := w.Components.Combat.GetComponent(gut)
		if !ok || g.From != lit.From || g.To != lit.To || combat.HitPoints != parameter.DevourerGutHP {
			t.Fatalf("slot %d: the omen ended in %+v at %d hit points", slot, g, combat.HitPoints)
		}
		header, _ := w.Components.Header.GetComponent(gut)
		standing := 0
		for _, m := range header.MemberEntries {
			if pos, ok := w.Positions.GetPosition(m.Entity); ok {
				standing++
				if s.body.covers(pos.X, pos.Y) {
					t.Fatalf("slot %d: a gut member stands on the body at %d,%d", slot, pos.X, pos.Y)
				}
			}
		}
		if standing == 0 {
			t.Fatalf("slot %d: the gut stands on no cell", slot)
		}
	}
	d.NextOmen = 0
	stepDevourer(w, s, 1)
	if d.Omen.Remaining > 0 {
		t.Fatal("an omen lit with four guts standing")
	}
}

// formGut runs a grown fixture's omen to its gut
func formGut(t *testing.T, w *engine.World, s *DevourerSystem, d *component.DevourerComponent) core.Entity {
	t.Helper()
	d.NextOmen = 0
	stepDevourer(w, s, 1+int(parameter.DevourerOmen/parameter.GameUpdateInterval))
	if d.Guts[0] == 0 {
		t.Fatal("no gut formed")
	}
	return d.Guts[0]
}

// TestACutGutFadesAndFreesItsSlotUncounted: a gut shot through fades where it hung
// and leaves its slot to the next omen, and is no kill
func TestACutGutFadesAndFreesItsSlotUncounted(t *testing.T) {
	w, s, e := devourerFixture(t)
	grown(w, s)
	d, _ := w.Components.Devourer.GetPtr(e)
	gut := formGut(t, w, s, d)
	combat, _ := w.Components.Combat.GetPtr(gut)
	combat.HitPoints = 0
	faded := false
	for _, ev := range stepDevourer(w, s, 1) {
		if p, ok := ev.Payload.(*event.CompositeDestroyRequestPayload); ok && p.HeaderEntity == gut {
			faded = p.Effect == component.DeathFadeout
		}
		if ev.Type == event.EventSpeciesKilled {
			t.Fatal("a cut gut counted as a kill")
		}
	}
	if !faded || d.Guts[0] != 0 {
		t.Fatalf("a cut gut faded %v, its slot holds %d", faded, d.Guts[0])
	}
}

// TestAKilledDevourerDropsFromTheOpenSpaceAndRecedes: its kill names the map's
// middle, where its drops burst from, its body draws back from how far it had grown,
// and its guts fade with it
func TestAKilledDevourerDropsFromTheOpenSpaceAndRecedes(t *testing.T) {
	w, s, e := devourerFixture(t)
	grown(w, s)
	d, _ := w.Components.Devourer.GetPtr(e)
	gut := formGut(t, w, s, d)
	combat, _ := w.Components.Combat.GetPtr(e)
	combat.HitPoints = 0
	cfg := w.Resources.Config
	var killed, receded, body, guts bool
	for _, ev := range stepDevourer(w, s, 1) {
		switch p := ev.Payload.(type) {
		case *event.SpeciesKilledPayload:
			killed = p.Species == component.SpeciesDevourer && p.X == cfg.MapWidth/2 && p.Y == cfg.MapHeight/2
		case *component.DevourerRecede:
			receded = p.Entity == e && p.Grown == 1 && p.Width == cfg.MapWidth
		case *event.CompositeDestroyRequestPayload:
			body = body || p.HeaderEntity == e
			guts = guts || p.HeaderEntity == gut && p.Effect == component.DeathFadeout
		}
	}
	if !killed || !receded || !body || !guts {
		t.Fatalf("killed %v, receded %v, body ended %v, gut faded %v", killed, receded, body, guts)
	}
}

// TestADevourerStrikesWhatItTouchesOnceATick: a bare cursor on its body pays its heat
// and a shield reaching its body and a gut pays its drain, once a tick each
func TestADevourerStrikesWhatItTouchesOnceATick(t *testing.T) {
	w, s, e := devourerFixture(t)
	grown(w, s)
	d, _ := w.Components.Devourer.GetPtr(e)
	gut := formGut(t, w, s, d)
	first, second := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	w.Positions.SetPosition(first, component.PositionComponent{X: 0, Y: 0})
	// The second stands on the gut's cell nearest a wall, its shield reaching both
	header, _ := w.Components.Header.GetComponent(gut)
	near, best := component.PositionComponent{}, 1e9
	for _, m := range header.MemberEntries {
		pos, ok := w.Positions.GetPosition(m.Entity)
		if !ok {
			continue
		}
		if depth := -s.shape.Depth(float64(pos.X)+0.5, 2*float64(pos.Y)+1); depth < best {
			near, best = pos, depth
		}
	}
	w.Positions.SetPosition(second, near)
	w.Components.Shield.SetComponent(second, component.ShieldComponent{Active: true, Radius: 6})
	heat, drain := 0, 0
	for _, ev := range stepDevourer(w, s, 1) {
		switch p := ev.Payload.(type) {
		case *event.HeatAddRequestPayload:
			if p.Entity == first && p.Delta == -parameter.DevourerDamageHeat {
				heat++
			}
		case *event.ShieldDrainRequestPayload:
			if p.Entity == second && p.Value == parameter.DevourerShieldDrain {
				drain++
			}
		}
	}
	if heat != 1 || drain != 1 {
		t.Fatalf("a tick struck the bare cursor %d times and the shield %d", heat, drain)
	}
}
