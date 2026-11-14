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

// TestAReplaceRetypesWhatADeleteReachesAndScoresNothing: r retypes the drawn glyph of
// each cell in its span that a delete would take, the player's and unprotected, and
// it is an edit, as a delete is: no reward, heat or error follows it.
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
		glyph(7, core.DomainShared, 'c'), glyph(9, core.DomainPlayer, 'd')}
	w.Components.Protection.SetComponent(cells[1], component.ProtectionComponent{Mask: component.ProtectFromDelete})

	typing.HandleEvent(event.GameEvent{Type: event.EventGlyphReplaceRequest,
		Payload: &event.GlyphReplaceRequestPayload{StartX: 5, EndX: 8, Y: 5, Char: 'z'}})
	var got []rune
	for _, e := range cells {
		g, _ := w.Components.Glyph.GetComponent(e)
		got = append(got, g.Rune)
	}
	if string(got) != "zbcd" {
		t.Fatalf("glyphs read %q after the replace, want %q", string(got), "zbcd")
	}
	if sent := w.Resources.Event.Queue.Consume(); len(sent) != 0 {
		t.Fatalf("a replace sent %d events, want none", len(sent))
	}
}
