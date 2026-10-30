package renderer

import (
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/cave"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// stoneField lays a field across the map whose ceiling and floor step from column to
// column, so rock cells open below, above and to either side, origin its first profile
// column
func stoneField(w *engine.World, look engine.RockLook, origin int64) {
	cols := make([]engine.RockColumn, w.Resources.Config.MapWidth)
	for x := range cols {
		cols[x] = stoneProfile(w, origin+int64(x))
	}
	w.Positions.SetRock(component.WallBlockAll, look, origin, cols)
}

// stoneProfile is profile column i: a ceiling of two to eight rows, a floor of three to
// seven, each stepping by several rows at once
func stoneProfile(w *engine.World, i int64) engine.RockColumn {
	return engine.RockColumn{Ceil: 2 + int(i*5%7), Floor: w.Resources.Config.MapHeight - 3 - int(i*3%5)}
}

func drawRock(game *engine.GameContext, r *WallRenderer) *render.RenderBuffer {
	config := game.World.Resources.Config
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	r.Render(render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
		ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)
	return buf
}

// TestSceneryPassesBehindRock: the glyph renderer draws scenery in the open and none
// where rock stands, since rock draws first and a glyph would print on it
func TestSceneryPassesBehindRock(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	config := w.Resources.Config
	cols := make([]engine.RockColumn, config.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Ceil: 3, Floor: config.MapHeight}
	}
	w.Positions.SetRock(component.WallBlockAll, engine.RockLook{}, 0, cols)
	for _, at := range []component.PositionComponent{{X: 5, Y: 1}, {X: 5, Y: 6}} {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, at)
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 's', Type: component.GlyphBlue, Level: component.GlyphNormal})
		w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectScenery})
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	NewGlyphRenderer(game).Render(render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
		ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)
	if hidden, shown := buf.CellAt(5, 1).Rune, buf.CellAt(5, 6).Rune; hidden != 0 || shown != 's' {
		t.Fatalf("drew %q in rock and %q in the open, want nothing and 's'", hidden, shown)
	}
}

// TestAParticlePassesHiddenThroughRock: the sigil renderer draws a particle in the
// open and none in rock that lets it pass, since rock draws first
func TestAParticlePassesHiddenThroughRock(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	config := w.Resources.Config
	cols := make([]engine.RockColumn, config.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Ceil: 3, Floor: config.MapHeight}
	}
	w.Positions.SetRock(component.WallBlockAll&^component.WallBlockParticle, engine.RockLook{}, 0, cols)
	for _, at := range []component.PositionComponent{{X: 5, Y: 1}, {X: 5, Y: 6}} {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, at)
		w.Components.Particle.SetComponent(e, component.ParticleComponent{Behavior: component.ParticleDecay})
		w.Components.Sigil.SetComponent(e, component.SigilComponent{Rune: 'd'})
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	NewSigilRenderer(game).Render(render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
		ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)
	if hidden, shown := buf.CellAt(5, 1).Rune, buf.CellAt(5, 6).Rune; hidden != 0 || shown != 'd' {
		t.Fatalf("drew %q in rock and %q in the open, want nothing and 'd'", hidden, shown)
	}
}

// TestRockDrawsWhereTheScrollStandsAtTheFrame: rock and shared walls ride the cave's
// scroll, so a frame between ticks draws them carried on, the columns ahead entering
// at the right edge, and scenery hides behind the rock as drawn
func TestRockDrawsWhereTheScrollStandsAtTheFrame(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	config := w.Resources.Config
	cols := make([]engine.RockColumn, config.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Floor: config.MapHeight}
	}
	cols[10].Ceil = 5
	w.Positions.SetRock(component.WallBlockAll, engine.RockLook{Visual: component.WallVisualConfig{Char: '#', RenderFg: true}}, 0, cols)
	w.Positions.SetRockAhead([]engine.RockColumn{{Ceil: 5, Floor: config.MapHeight}})
	wall := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(wall, component.PositionComponent{X: 20, Y: 8})
	w.Components.Wall.SetComponent(wall, component.WallComponent{Rune: 'W', RenderFg: true})
	for _, x := range []int{9, 10} {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 2})
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 's', Type: component.GlyphBlue, Level: component.GlyphNormal})
		w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectScenery})
	}
	// Half a column carried and 20 columns a second for 30 ms: 1.1 columns on
	tick := time.Unix(1000, 0)
	w.Resources.Time.PaceTime, w.Resources.Time.DeltaTime = tick, 50*time.Millisecond
	w.Resources.Cave.Phase, w.Resources.Cave.Rate = vmath.CellSpan/2, 20*vmath.MicroRate
	ctx := render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
		ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight, GameTime: tick.Add(30 * time.Millisecond)}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	NewWallRenderer(game).Render(ctx, buf)
	if left, stood, entered, moved := buf.CellAt(9, 2).Rune, buf.CellAt(10, 2).Rune, buf.CellAt(config.MapWidth-1, 2).Rune, buf.CellAt(19, 8).Rune; left != '#' || stood != 0 || entered != '#' || moved != 'W' {
		t.Fatalf("drew %q a column left of the rock, %q where it stood, %q at the edge, the wall %q, want '#', nothing, '#', 'W'", left, stood, entered, moved)
	}
	buf = render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	NewGlyphRenderer(game).Render(ctx, buf)
	if hidden, shown := buf.CellAt(9, 2).Rune, buf.CellAt(10, 2).Rune; hidden != 0 || shown != 's' {
		t.Fatalf("drew %q behind the rock and %q where it left, want nothing and 's'", hidden, shown)
	}
}

// TestStoneRockDrawsEachCellsArtAndKeepsItAcrossAScroll: a rock field in stone draws
// pattern.Art in every rock cell, placed by its profile column, pattern.Art256 on the 256
// tier, and nothing in the open; a renderer that drew the field before it scrolled draws
// what a new one draws after, the stone moving with its rock, and a new seed redraws it
func TestStoneRockDrawsEachCellsArtAndKeepsItAcrossAScroll(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorMode256
	indexed := NewWallRenderer(game)
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	look := engine.RockLook{Stone: true, Art: uint8(pattern.ArtStrata), Seed: 7}
	stoneField(w, look, 100)
	kept := NewWallRenderer(game)

	width, height := w.Resources.Config.MapWidth, w.Resources.Config.MapHeight
	before, before256 := drawRock(game, kept), drawRock(game, indexed)
	cols := w.Positions.Rock().Columns()
	for x := range width {
		var around stoneNeighbourhood
		for i := range around {
			if nx := x + i - pattern.FaceReach; nx >= 0 && nx < width {
				around[i] = cols[nx]
			} else {
				around[i] = stoneSealed
			}
		}
		for y := range height {
			cell, cell256 := before.CellAt(x, y), before256.CellAt(x, y)
			if !cols[x].Rock(y) {
				if cell.Rune != 0 || cell256.Rune != 0 {
					t.Fatalf("open cell (%d,%d) drew %q, %q in 256 colours", x, y, cell.Rune, cell256.Rune)
				}
				continue
			}
			depth, face := stoneDepth(&around, y)
			r, fg, bg := pattern.Art(pattern.ArtStrata, 7, pattern.ArtCell{X: 100 + int64(x), Y: int64(y), Depth: depth, Face: face, W: stoneGroupWidth, H: height})
			if cell.Rune != r || cell.Fg != fg || cell.Bg != bg {
				t.Fatalf("rock (%d,%d) drew %q %v on %v, want %q %v on %v", x, y, cell.Rune, cell.Fg, cell.Bg, r, fg, bg)
			}
			_, fg256, bg256 := pattern.Art256(pattern.ArtStrata, 7, pattern.ArtCell{X: 100 + int64(x), Y: int64(y), Depth: depth, Face: face, W: stoneGroupWidth, H: height})
			if cell256.Rune != r || cell256.Fg.R != fg256 || cell256.Bg.R != bg256 || cell256.Attrs != terminal.AttrFg256|terminal.AttrBg256 {
				t.Fatalf("rock (%d,%d) drew %q %v on %v (%v) in 256 colours, want %q %d on %d", x, y, cell256.Rune, cell256.Fg, cell256.Bg, cell256.Attrs, r, fg256, bg256)
			}
		}
	}

	w.Positions.ShiftRock(stoneProfile(w, 100+int64(width)))
	after := drawRock(game, kept)
	fresh := drawRock(game, NewWallRenderer(game))
	for x := range width {
		for y := range height {
			if got, want := after.CellAt(x, y), fresh.CellAt(x, y); got != want {
				t.Fatalf("after a scroll (%d,%d) drew %+v, a new renderer %+v", x, y, got, want)
			}
			// Away from the edges a column keeps its neighbours, so its stone moves whole
			if x >= pattern.FaceReach && x+1 < width-pattern.FaceReach {
				if got, was := after.CellAt(x, y), before.CellAt(x+1, y); got != was {
					t.Fatalf("(%d,%d) drew %+v after a scroll, want %+v from the column it took", x, y, got, was)
				}
			}
		}
	}

	look.Seed = 8
	stoneField(w, look, 101)
	redrawn, fresh := drawRock(game, kept), drawRock(game, NewWallRenderer(game))
	changed := false
	for x := range width {
		for y := range height {
			if redrawn.CellAt(x, y) != fresh.CellAt(x, y) {
				t.Fatalf("under a new seed (%d,%d) drew %+v, a new renderer %+v", x, y, redrawn.CellAt(x, y), fresh.CellAt(x, y))
			}
			changed = changed || redrawn.CellAt(x, y) != after.CellAt(x, y)
		}
	}
	if !changed {
		t.Fatal("a new seed drew the same stone")
	}
}

// TestARockCellsFaceIsItsNearestOpening: below a ceiling, above a floor, along the row
// where a column within reach opens nearer, the left one on a tie, and its own column's
// opening on a tie with a side's
func TestARockCellsFaceIsItsNearestOpening(t *testing.T) {
	var around stoneNeighbourhood
	for i := range around {
		around[i] = engine.RockColumn{Ceil: 10, Floor: 20}
	}
	for _, c := range []struct {
		name  string
		left  int // the ceiling of the column d to the left, 0 to leave it
		right int
		d     int
		y     int
		depth int
		face  pattern.Face
	}{
		{"the ceiling's face", 0, 0, 1, 9, 0, pattern.FaceDown},
		{"deep in the ceiling", 0, 0, 1, 2, 7, pattern.FaceDown},
		{"the floor's face", 0, 0, 1, 20, 0, pattern.FaceUp},
		{"a step down to the right", 0, 6, 1, 7, 0, pattern.FaceRight},
		{"a step two columns left", 6, 0, 2, 6, 1, pattern.FaceLeft},
		{"both sides alike", 6, 6, 1, 7, 0, pattern.FaceLeft},
		{"a side no nearer than below", 0, 6, 3, 7, 2, pattern.FaceDown},
		{"a side past reach", 0, 0, pattern.FaceReach + 1, 0, 9, pattern.FaceDown},
	} {
		a := around
		if c.d <= pattern.FaceReach {
			if c.left > 0 {
				a[pattern.FaceReach-c.d].Ceil = c.left
			}
			if c.right > 0 {
				a[pattern.FaceReach+c.d].Ceil = c.right
			}
		}
		if depth, face := stoneDepth(&a, c.y); depth != c.depth || face != c.face {
			t.Errorf("%s: row %d at depth %d facing %d, want %d facing %d", c.name, c.y, depth, face, c.depth, c.face)
		}
	}
}

// BenchmarkRockRender draws vanguard's cave scrolling a column a frame across a wide
// terminal, a large one and the largest map, in a flat visual and in stone; "redraw"
// changes the stone's seed every frame, so every column draws again, as on a cave's
// first frame
func BenchmarkRockRender(b *testing.B) {
	for _, size := range []struct{ w, h int }{{200, 60}, {400, 120}, {500, 254}} {
		for _, c := range []struct {
			name   string
			look   engine.RockLook
			redraw bool
		}{
			{"visual", engine.RockLook{Visual: component.WallVisualConfig{Char: '▒', RenderFg: true, RenderBg: true}}, false},
			{"stone", engine.RockLook{Stone: true, Art: uint8(pattern.ArtCracked), Seed: 7}, false},
			{"redraw", engine.RockLook{Stone: true, Art: uint8(pattern.ArtCracked), Seed: 7}, true},
		} {
			b.Run(fmt.Sprintf("%dx%d/%s", size.w, size.h, c.name), func(b *testing.B) {
				w := engine.NewWorld()
				game := engine.NewGameContextWithClock(w, size.w, size.h, engine.NewManualClock())
				w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
				config := w.Resources.Config
				profile := cave.Config{Seed: 7, Height: config.MapHeight, MinGap: 7, MaxGap: 15, MinWall: 2, Wavelength: 44, Swing: 9, Detail: 1, Ramp: 96}
				column := func(i int64) engine.RockColumn {
					col := profile.At(i)
					return engine.RockColumn{Ceil: col.Top, Floor: config.MapHeight - col.Bottom}
				}
				cols := make([]engine.RockColumn, config.MapWidth)
				for x := range cols {
					cols[x] = column(int64(x))
				}
				w.Positions.SetRock(component.WallBlockAll, c.look, 0, cols)
				r := NewWallRenderer(game)
				buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
				ctx := render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
					ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}
				r.Render(ctx, buf)
				next := int64(len(cols))
				b.ReportAllocs()
				for b.Loop() {
					if c.redraw {
						look := c.look
						look.Seed = uint64(next)
						w.Positions.SetRock(component.WallBlockAll, look, 0, w.Positions.Rock().Columns())
					}
					w.Positions.ShiftRock(column(next))
					next++
					r.Render(ctx, buf)
				}
				rock := 0
				for _, col := range w.Positions.Rock().Columns() {
					rock += min(col.Ceil, config.MapHeight) + config.MapHeight - min(col.Floor, config.MapHeight)
				}
				b.ReportMetric(float64(rock), "rock-cells")
			})
		}
	}
}
