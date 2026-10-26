package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
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
