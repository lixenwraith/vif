package renderer

import (
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// sightFrame draws a frame of a map full of x under the sight renderer, the cursor
// at (x, 5) at the moment at
func sightFrame(game *engine.GameContext, r *SightRenderer, x int, at time.Duration) *render.RenderBuffer {
	config := game.World.Resources.Config
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
	for y := range config.MapHeight {
		for x := range config.MapWidth {
			buf.SetFgOnly(x, y, 'x', color.RGB{R: 200}, terminal.AttrNone)
		}
	}
	r.Render(render.RenderContext{GameTime: engine.SimEpoch.Add(at), CursorX: x, CursorY: 5, CursorValid: true,
		MapWidth: config.MapWidth, MapHeight: config.MapHeight, ViewportWidth: config.MapWidth,
		ViewportHeight: config.MapHeight, CellAspect: 0.5}, buf)
	return buf
}

// TestADarkMapShowsWhatAStepsLightFellOn: on a dark map a cell shows once the light
// of a step falls on it, ahead of the cursor and not behind it, a wall in the way
// lit and the cell past it not, and it goes back into the dark as the light fades.
func TestADarkMapShowsWhatAStepsLightFellOn(t *testing.T) {
	w, game := glyphWorld()
	w.Resources.View.Sight.Dark = true
	wall := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(wall, component.PositionComponent{X: 14, Y: 5})
	w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockAll})
	r := NewSightRenderer(game)
	shows := func(buf *render.RenderBuffer, x, y int) bool { return buf.CellAt(x, y).Rune == 'x' }

	if buf := sightFrame(game, r, 10, 0); shows(buf, 13, 5) {
		t.Fatal("a cursor that has not stepped lit the dark")
	}
	buf := sightFrame(game, r, 11, 30*time.Millisecond)
	for _, c := range []struct {
		x, y int
		lit  bool
	}{{13, 5, true}, {13, 4, true}, {14, 5, true}, {16, 5, false}, {9, 5, false}, {13, 9, false}} {
		if shows(buf, c.x, c.y) != c.lit {
			t.Errorf("after a step right from (10, 5), (%d, %d) shows %t, want %t", c.x, c.y, !c.lit, c.lit)
		}
	}
	if buf := sightFrame(game, r, 11, 30*time.Millisecond+visual.SightFade); shows(buf, 13, 5) {
		t.Error("a cell the light left stayed lit past the fade")
	}
}

// TestATrailDrawsTheWalkFromWhereItBegan: a trail draws the cells the cursor
// stepped through, its first marked as the start, and a jump begins a new one.
func TestATrailDrawsTheWalkFromWhereItBegan(t *testing.T) {
	w, game := glyphWorld()
	w.Resources.View.Sight.Trail = true
	r := NewSightRenderer(game)
	var buf *render.RenderBuffer
	for i, x := range []int{10, 11, 14} {
		buf = sightFrame(game, r, x, time.Duration(i)*50*time.Millisecond)
	}
	for x, want := range map[int]rune{10: visual.SightStartRune, 12: visual.SightTrailRune, 13: visual.SightTrailRune} {
		if got := buf.CellAt(x, 5).Rune; got != want {
			t.Errorf("the walk from 10 to 14 drew %q at (%d, 5), want %q", got, x, want)
		}
	}
	buf = sightFrame(game, r, 30, time.Second)
	if buf.CellAt(10, 5).Rune != 'x' || buf.CellAt(30, 5).Rune != visual.SightStartRune {
		t.Errorf("a jump to (30, 5) left %q at the old start and drew %q at its own",
			buf.CellAt(10, 5).Rune, buf.CellAt(30, 5).Rune)
	}
}
