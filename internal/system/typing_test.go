package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// TestTypingActsOnTheDrawnGlyph: where drift stacks layers in one cell, a keystroke
// matches the nearest layer, the one drawn on top, whatever order they arrived in.
func TestTypingActsOnTheDrawnGlyph(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	typing := ready(NewTypingSystem(w)).(*TypingSystem)
	far := addDriftGlyph(w, 5, 5, component.GlyphDark)
	near := addDriftGlyph(w, 5, 5, component.GlyphBright)
	w.Components.Glyph.SetComponent(far, component.GlyphComponent{Rune: 'a', Type: component.GlyphBlue, Level: component.GlyphDark})
	w.Components.Glyph.SetComponent(near, component.GlyphComponent{Rune: 'b', Type: component.GlyphBlue, Level: component.GlyphBright})

	consumed := func(typed rune) []component.GlyphLevel {
		typing.handleTyping(cursor, 5, 5, typed)
		var levels []component.GlyphLevel
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.EnergyGlyphConsumedPayload); ok {
				levels = append(levels, p.Level)
			}
		}
		return levels
	}
	if got := consumed('a'); len(got) != 0 {
		t.Fatalf("typing the hidden glyph consumed %v, want a miss", got)
	}
	if got := consumed('b'); len(got) != 1 || got[0] != component.GlyphBright {
		t.Fatalf("typing the drawn glyph consumed %v, want [bright]", got)
	}
}

// TestAReplaceRetypesWhatADeleteReachesAndScoresNothing: a replace retypes the drawn
// glyph of each cell its text names that a delete would take, the player's and
// unprotected, a space leaving its cell, and it is an edit, as a delete is: no
// reward, heat or error follows it.
func TestAReplaceRetypesWhatADeleteReachesAndScoresNothing(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	typing := ready(NewTypingSystem(w)).(*TypingSystem)
	glyph := func(x int, domain core.Domain, ch rune) core.Entity {
		e := w.CreateEntity(domain)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 5})
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: ch})
		return e
	}
	cells := []core.Entity{glyph(5, core.DomainPlayer, 'a'), glyph(6, core.DomainPlayer, 'b'),
		glyph(7, core.DomainShared, 'c'), glyph(8, core.DomainPlayer, 'd'), glyph(9, core.DomainPlayer, 'e')}
	w.Components.Protection.SetComponent(cells[1], component.ProtectionComponent{Mask: component.ProtectFromDelete})

	typing.HandleEvent(event.GameEvent{Type: event.EventGlyphReplaceRequest,
		Payload: &event.GlyphReplaceRequestPayload{X: 5, Y: 5, Text: "zzz y"}})
	var got []rune
	for _, e := range cells {
		g, _ := w.Components.Glyph.GetComponent(e)
		got = append(got, g.Rune)
	}
	if string(got) != "zbcdy" {
		t.Fatalf("glyphs read %q after the replace, want %q", string(got), "zbcdy")
	}
	if sent := w.Resources.Event.Queue.Consume(); len(sent) != 0 {
		t.Fatalf("a replace sent %d events, want none", len(sent))
	}
}

// TestADeleteTakesItsRangeByKind: over three rows of five glyphs, a char range takes
// from its start to its end as text runs, a line range its rows whole, and a block
// the columns between its ends on every row, whichever end comes first.
func TestADeleteTakesItsRangeByKind(t *testing.T) {
	for _, c := range []struct {
		p    event.DeleteRequestPayload
		want string
	}{
		{event.DeleteRequestPayload{RangeType: event.DeleteRangeChar, StartX: 3, StartY: 0, EndX: 1, EndY: 2}, "abc.. ..... ..mno"},
		{event.DeleteRequestPayload{RangeType: event.DeleteRangeChar, StartX: 1, StartY: 2, EndX: 3, EndY: 0}, "abc.. ..... ..mno"},
		{event.DeleteRequestPayload{RangeType: event.DeleteRangeLine, StartX: 3, StartY: 1, EndX: 1, EndY: 2}, "abcde ..... ....."},
		{event.DeleteRequestPayload{RangeType: event.DeleteRangeBlock, StartX: 3, StartY: 0, EndX: 1, EndY: 1}, "a...e f...j klmno"},
	} {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		typing := ready(NewTypingSystem(w)).(*TypingSystem)
		for i, ch := range "abcdefghijklmno" {
			e := w.CreateEntity(core.DomainPlayer)
			w.Positions.SetPosition(e, component.PositionComponent{X: i % 5, Y: i / 5})
			w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: ch})
		}
		typing.HandleEvent(event.GameEvent{Type: event.EventDeleteRequest, Payload: &c.p})
		dying := map[core.Entity]bool{}
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.DeathRequestPayload); ok {
				for _, e := range p.Entities {
					dying[e] = true
				}
			}
		}
		got := []rune("..... ..... .....")
		w.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
			if pos, _ := w.Positions.GetPosition(e); !dying[e] {
				got[pos.Y*6+pos.X] = g.Rune
			}
			return true
		})
		if string(got) != c.want {
			t.Errorf("%+v left %q, want %q", c.p, string(got), c.want)
		}
	}
}
