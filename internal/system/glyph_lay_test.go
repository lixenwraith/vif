package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// oneBlock hands the spawner one block, then nothing
type oneBlock struct{ lines []string }

func (b *oneBlock) NextBlock() (core.CodeBlock, bool) {
	if b.lines == nil {
		return core.CodeBlock{}, false
	}
	lines := b.lines
	b.lines = nil
	return core.CodeBlock{Lines: lines}, true
}

// Where the scenario asks for blocks, a corpus block is laid whole: its lines on
// consecutive rows from one column, so a bracket pair or a paragraph reads across them.
func TestACorpusBlockIsLaidOnConsecutiveRows(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	w.Resources.Config.ContentBlocks = true
	glyphs := ready(NewGlyphSystem(w)).(*GlyphSystem)
	w.Resources.Content = &engine.ContentResource{Provider: &oneBlock{lines: []string{"func main() {", "    run()", "}"}}}
	glyphs.spawnGlyphs()

	rows := map[int]int{} // row to the leftmost column laid on it
	w.Components.Glyph.Each(func(e core.Entity, _ *component.GlyphComponent) bool {
		pos, _ := w.Positions.GetPosition(e)
		if left, ok := rows[pos.Y]; !ok || pos.X < left {
			rows[pos.Y] = pos.X
		}
		return true
	})
	if len(rows) != 3 {
		t.Fatalf("the block's three lines lie on %d rows", len(rows))
	}
	top, left := -1, -1
	for y, x := range rows {
		if top < 0 || y < top {
			top, left = y, x
		}
	}
	for i, want := range []int{left, left + 4, left} {
		if x, ok := rows[top+i]; !ok || x != want {
			t.Fatalf("row %d of the block starts at column %d (laid %v), want %d: consecutive rows from one column", i, x, ok, want)
		}
	}
}

// A glyph clear removes the player glyphs in its rectangle at once, every one for
// an empty rectangle; a gold member, a shared composite's, stays.
func TestAGlyphClearTakesARectangleOrTheMap(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	glyphs := ready(NewGlyphSystem(w)).(*GlyphSystem)
	for i, text := range []string{"first line", "second line"} {
		glyphs.placeLineAt(&event.GlyphLinePayload{Text: text, X: 2, Y: 10 + i})
	}
	member := w.CreateEntity(core.DomainShared)
	w.Components.Glyph.SetComponent(member, component.GlyphComponent{Rune: 'G', Type: component.GlyphGold})
	w.Positions.SetPosition(member, component.PositionComponent{X: 2, Y: 12})

	glyphs.clear(&event.GlyphClearPayload{X: 0, Y: 11, Width: 40, Height: 1})
	if n := w.Components.Glyph.CountEntities(); n != 9+1 {
		t.Fatalf("after clearing row 11, %d glyphs stand, want the first line's 9 and the gold member", n)
	}
	glyphs.clear(&event.GlyphClearPayload{})
	if n := w.Components.Glyph.CountEntities(); n != 1 || !w.Components.Glyph.HasEntity(member) {
		t.Fatalf("after clearing the map, %d glyphs stand, want the gold member alone", n)
	}
}

// A put lays each cell's glyph with the rune, colour and level it names, and passes
// over a cell a glyph holds, an earlier cell of the same put included, a cell off
// the map and a rune no glyph holds.
func TestAPutLaysEachGlyphAsYankedAndPassesOverAHeldCell(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	glyphs := ready(NewGlyphSystem(w)).(*GlyphSystem)
	glyphs.placeLineAt(&event.GlyphLinePayload{Text: "x", X: 3, Y: 10})
	glyphs.HandleEvent(event.GameEvent{Type: event.EventGlyphPutRequest, Payload: &event.GlyphPutPayload{Cells: []event.GlyphCell{
		{X: 2, Y: 10, Rune: 'a', Type: component.GlyphBlue, Level: component.GlyphBright},
		{X: 3, Y: 10, Rune: 'b', Type: component.GlyphBlue, Level: component.GlyphBright},
		{X: 4, Y: 10, Rune: 'c', Type: component.GlyphRed, Level: component.GlyphDark},
		{X: 4, Y: 10, Rune: 'd', Type: component.GlyphRed, Level: component.GlyphDark},
		{X: 40, Y: 10, Rune: 'e'},
		{X: 5, Y: 10, Rune: ' '},
	}}})
	at := func(x int) component.GlyphComponent {
		e, _ := w.Positions.TopGlyphAt(x, 10)
		g, _ := w.Components.Glyph.GetComponent(e)
		return g
	}
	if got, want := at(2), (component.GlyphComponent{Rune: 'a', Type: component.GlyphBlue, Level: component.GlyphBright}); got != want {
		t.Errorf("the free cell holds %+v, want %+v", got, want)
	}
	if got, want := at(4), (component.GlyphComponent{Rune: 'c', Type: component.GlyphRed, Level: component.GlyphDark}); got != want {
		t.Errorf("the cell named twice holds %+v, want the first, %+v", got, want)
	}
	if got := at(3).Rune; got != 'x' {
		t.Errorf("the held cell reads %q, want the glyph that held it", got)
	}
	if n := w.Components.Glyph.CountEntities(); n != 3 {
		t.Errorf("%d glyphs stand, want the one laid before and the put's two", n)
	}
}

// A viewer's world is its session's Shared world alone: where a player's lays a
// corpus line, takes the line a scenario lays and spawns a nugget, it holds none.
func TestAViewersWorldLaysNoFieldOfItsOwn(t *testing.T) {
	for _, watches := range []bool{false, true} {
		w, _, _ := testCursorWorld(t)
		if watches {
			w.Watch()
		}
		w.Resources.Content = &engine.ContentResource{Provider: &oneBlock{lines: []string{"a corpus line"}}}
		w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
		w.Resources.Time.GameTime = engine.SimEpoch.Add(2 * parameter.NuggetSpawnInterval)
		glyphs, nuggets := ready(NewGlyphSystem(w)).(*GlyphSystem), ready(NewNuggetSystem(w)).(*NuggetSystem)

		glyphs.HandleEvent(event.GameEvent{Type: event.EventGlyphLineRequest,
			Payload: &event.GlyphLinePayload{Text: "a lesson", X: 2, Y: 10}})
		scenario := w.Components.Glyph.CountEntities()
		glyphs.Update()
		nuggets.Update()
		corpus, nugget := w.Components.Glyph.CountEntities()-scenario, w.Components.Nugget.CountEntities()
		if watches && scenario+corpus+nugget != 0 || !watches && (scenario == 0 || corpus == 0 || nugget == 0) {
			t.Fatalf("watching %t: the world holds %d glyphs of a scenario's line, %d of the corpus and %d nuggets",
				watches, scenario, corpus, nugget)
		}
	}
}
