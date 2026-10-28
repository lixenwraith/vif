package renderer

import (
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/cave"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// glyphWorld is a 40×24 truecolour map with no rock, ready for glyphs
func glyphWorld() (*engine.World, *engine.GameContext) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	return w, game
}

// addGlyph places a glyph at (x, y), scenery unless typeable
func addGlyph(w *engine.World, x, y int, r rune, level component.GlyphLevel, scenery bool) {
	e := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: r, Type: component.GlyphBlue, Level: level})
	if scenery {
		w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectScenery})
	}
}

// drawGlyphs renders the glyph layer of a frame standing at gameTime
func drawGlyphs(game *engine.GameContext, gameTime time.Time) *render.RenderBuffer {
	config := game.World.Resources.Config
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	NewGlyphRenderer(game).Render(render.RenderContext{GameTime: gameTime, MapWidth: config.MapWidth,
		MapHeight: config.MapHeight, ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)
	return buf
}

// TestSceneryIsDrawnWhereItsLayerStandsBetweenTicks: a frame draws scenery carried on
// from its cell by its layer's part-cell and velocity over the time since the tick,
// a tick at most, none for a frame before the tick or before any; typeable text
// stays in its cell
func TestSceneryIsDrawnWhereItsLayerStandsBetweenTicks(t *testing.T) {
	w, game := glyphWorld()
	addGlyph(w, 20, 5, 'b', component.GlyphBright, true)
	addGlyph(w, 20, 7, 'd', component.GlyphDark, true)
	addGlyph(w, 20, 9, 't', component.GlyphBright, false)
	drift := w.Resources.Drift
	drift.Rate[component.GlyphBright][0] = -80 * vmath.MicroRate
	drift.Rate[component.GlyphDark][0] = -20 * vmath.MicroRate
	drift.Phase[component.GlyphDark][0] = -vmath.CellSpan / 2
	tick := engine.SimEpoch.Add(time.Second)

	for _, tc := range []struct {
		name       string
		pace       time.Time
		since      time.Duration
		bright, dk int
	}{
		{"at the tick", tick, 0, 20, 20},
		{"half a tick on", tick, 25 * time.Millisecond, 18, 19},
		{"past the next tick", tick, 80 * time.Millisecond, 16, 19},
		{"before the tick", tick, -10 * time.Millisecond, 20, 20},
		{"before any tick", time.Time{}, 25 * time.Millisecond, 20, 20},
	} {
		w.Resources.Time.PaceTime = tc.pace
		buf := drawGlyphs(game, tick.Add(tc.since))
		for _, want := range []struct {
			x, y int
			r    rune
		}{{tc.bright, 5, 'b'}, {tc.dk, 7, 'd'}, {20, 9, 't'}} {
			if got := buf.CellAt(want.x, want.y).Rune; got != want.r {
				t.Fatalf("%s: (%d,%d) drew %q, want %q", tc.name, want.x, want.y, got, want.r)
			}
		}
	}
}

// TestTheNearestGlyphDrawsOverTheOnesBehind: where glyphs are drawn in one cell, the
// nearer scenery shows over the farther and typeable text over any scenery, whatever
// order they were made in, and a glyph hides nothing beside it
func TestTheNearestGlyphDrawsOverTheOnesBehind(t *testing.T) {
	w, game := glyphWorld()
	addGlyph(w, 10, 5, 'n', component.GlyphBright, true)
	addGlyph(w, 10, 5, 'f', component.GlyphDark, true)
	addGlyph(w, 11, 5, 'f', component.GlyphDark, true)
	addGlyph(w, 12, 5, 't', component.GlyphDark, false)
	addGlyph(w, 12, 5, 'n', component.GlyphBright, true)

	buf := drawGlyphs(game, time.Time{})
	for i, want := range "nft" {
		if got := buf.CellAt(10+i, 5).Rune; got != want {
			t.Fatalf("(%d,5) drew %q, want %q", 10+i, got, want)
		}
	}
}

// TestSceneryTakesItsColourFromItsDepth: each scenery layer mixes its type's bright
// colour into the background by its depth, while typeable text keeps its level's colour
func TestSceneryTakesItsColourFromItsDepth(t *testing.T) {
	w, game := glyphWorld()
	levels := []component.GlyphLevel{component.GlyphDark, component.GlyphNormal, component.GlyphBright}
	for i, level := range levels {
		addGlyph(w, 5+5*i, 3, 's', level, true)
		addGlyph(w, 5+5*i, 6, 't', level, false)
	}
	buf := drawGlyphs(game, time.Time{})
	bright := visual.GlyphColorLUT[component.GlyphBlue][component.GlyphBright]
	for i, level := range levels {
		if got, want := buf.CellAt(5+5*i, 3).Fg, color.Lerp(visual.RgbBackground, bright, visual.SceneryDepthMix[level]); got != want {
			t.Fatalf("scenery of level %d drew %v, want %v", level, got, want)
		}
		if got, want := buf.CellAt(5+5*i, 6).Fg, visual.GlyphColorLUT[component.GlyphBlue][level]; got != want {
			t.Fatalf("typeable text of level %d drew %v, want %v", level, got, want)
		}
	}
}

// TestAnEnteringLineIsDrawnAStepPastWhereTheSpawnerLeftIt: the runes of a line still
// entering are drawn from where the spawner left its next one, on by the tick's step
// and, as scenery, its layer's carry, up to the edge and under a glyph drawn there
func TestAnEnteringLineIsDrawnAStepPastWhereTheSpawnerLeftIt(t *testing.T) {
	w, game := glyphWorld()
	width := w.Resources.Config.MapWidth
	drift := w.Resources.Drift
	drift.Rate[component.GlyphBright][0] = -80 * vmath.MicroRate
	drift.Step[component.GlyphBright] = [2]int{-4, 0}
	drift.Step[component.GlyphNormal] = [2]int{-2, 0}
	w.Resources.Entry.Lines = []engine.EntryLine{
		{X: width + 1, Y: 5, Dir: 1, Text: "abcdefg", Type: component.GlyphBlue, Level: component.GlyphBright, Scenery: true},
		{X: width, Y: 7, Dir: 1, Text: "pq", Type: component.GlyphBlue, Level: component.GlyphNormal},
	}
	addGlyph(w, width-1, 5, 'R', component.GlyphBright, true)
	addGlyph(w, width-1, 7, 'T', component.GlyphNormal, false)
	tick := engine.SimEpoch.Add(time.Second)
	w.Resources.Time.PaceTime = tick

	buf := drawGlyphs(game, tick.Add(25*time.Millisecond))
	for y, want := range map[int]string{5: "abRde", 7: "...pT"} {
		for i, r := range want {
			if got := buf.CellAt(width-5+i, y).Rune; r != '.' && got != r {
				t.Fatalf("(%d,%d) drew %q, want %q", width-5+i, y, got, r)
			}
		}
	}
}

// BenchmarkGlyphRender draws a frame half a tick past its tick, of scenery drifting at
// vanguard's three layer speeds over vanguard's cave, on three terminals, each with
// as many glyphs as vanguard keeps there, placed at random, in the rock too
func BenchmarkGlyphRender(b *testing.B) {
	for _, size := range []struct{ w, h, glyphs int }{{120, 40, 870}, {200, 60, 1460}, {400, 120, 2560}} {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			w := engine.NewWorld()
			game := engine.NewGameContextWithClock(w, size.w, size.h, engine.NewManualClock())
			w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
			config := w.Resources.Config
			profile := cave.Config{Seed: 7, Height: config.MapHeight, MinGap: 9, MaxGap: 36, MinWall: 2, Wavelength: 64, Swing: 9, Ramp: 96, Octaves: 4}
			cols := make([]engine.RockColumn, config.MapWidth)
			for x := range cols {
				col := profile.At(int64(x))
				cols[x] = engine.RockColumn{Ceil: col.Top, Floor: config.MapHeight - col.Bottom}
			}
			w.Positions.SetRock(component.WallBlockAll, engine.RockLook{}, 0, cols)
			rng := vmath.NewFastRand(3)
			for range size.glyphs {
				addGlyph(w, rng.Intn(size.w), rng.Intn(size.h), 'a'+rune(rng.Intn(26)), component.GlyphLevel(rng.Intn(3)), true)
			}
			for level, speed := range []int64{-20, -40, -80} {
				w.Resources.Drift.Rate[level][0] = speed * vmath.MicroRate
			}
			tick := engine.SimEpoch.Add(time.Second)
			w.Resources.Time.PaceTime, w.Resources.Time.DeltaTime = tick, 50*time.Millisecond
			r := NewGlyphRenderer(game)
			buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
			ctx := render.RenderContext{GameTime: tick.Add(25 * time.Millisecond), MapWidth: config.MapWidth,
				MapHeight: config.MapHeight, ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}
			b.ReportAllocs()
			for b.Loop() {
				r.Render(ctx, buf)
			}
		})
	}
}
