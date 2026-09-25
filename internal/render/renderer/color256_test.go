package renderer

import (
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// TestEmber256WearsTheHeatBarLeadColour: the solid 256-colour ember takes the
// palette of the heat bar's last filled cell, at every heat and screen width.
func TestEmber256WearsTheHeatBarLeadColour(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 1)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorMode256
	pos := component.PositionComponent{X: 40, Y: 10}
	gameCtx.World.Positions.SetPosition(cursors[0], pos)
	gameCtx.World.Components.Shield.SetComponent(cursors[0], component.ShieldComponent{Type: component.ShieldTypePlayer})
	bar, ember := NewHeatRenderer(gameCtx), NewEmberRenderer(gameCtx)

	for _, width := range []int{80, 97, 203} {
		rc := peerContext(gameCtx)
		rc.ScreenWidth = width
		for heat := 1; heat <= 100; heat++ {
			gameCtx.World.Components.Heat.SetComponent(cursors[0], component.HeatComponent{Current: heat, EmberActive: true})
			buf := render.NewRenderBuffer(terminal.ColorMode256, width, 24)
			bar.Render(rc, buf)
			lead := -1
			for x := range width {
				if buf.CellAt(x, 0).Attrs&terminal.AttrBg256 != 0 {
					lead = x
				}
			}
			if lead < 0 {
				continue
			}
			want := buf.CellAt(lead, 0).Bg.R

			buf.Clear()
			ember.Render(rc, buf)
			if got := buf.CellAt(pos.X+1, pos.Y); got.Attrs&terminal.AttrBg256 == 0 || got.Bg.R != want {
				t.Fatalf("width %d heat %d: ember palette (%d, %v), heat bar lead %d", width, heat, got.Bg.R, got.Attrs, want)
			}
		}
	}
}

// TestShield256RimClosesOnEveryAxis: every shield's 256-colour rim crosses both
// axes on both sides of its centre, however few cells tall the shield is.
func TestShield256RimClosesOnEveryAxis(t *testing.T) {
	t.Parallel()
	const w, h, cx, cy = 60, 30, 30, 15
	rc := render.RenderContext{ViewportWidth: w, ViewportHeight: h, MapWidth: w, MapHeight: h}
	p := NewShieldPainter(terminal.ColorMode256)

	for shieldType := range visual.ShieldConfigs {
		cfg := &visual.ShieldConfigs[shieldType]
		buf := render.NewRenderBuffer(terminal.ColorMode256, w, h)
		p.Paint(buf, rc, cx, cy, ShieldStyle{Config: cfg, BlendScale: 1, Palette256: cfg.Palette256, SkipX: -1, SkipY: -1})

		for _, dir := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			crossed := false
			for n := 1; n <= cfg.VisualRadiusXInt && !crossed; n++ {
				crossed = buf.CellAt(cx+dir[0]*n, cy+dir[1]*n).Attrs&terminal.AttrBg256 != 0
			}
			if !crossed {
				t.Errorf("shield type %d: no rim cell towards %v", shieldType, dir)
			}
		}
	}
}

// consoleGlyphs are the non-ASCII symbols in the kernel's CP437 map (Arch's console), in every
// font Ubuntu's setupcon installs for Latin scripts, and in FreeBSD vt's default font
const consoleGlyphs = "·×÷°±•‼←↑→↓≈≡≤≥─│┌┐└┘├┤┬┴┼═║╒╓╔╕╖╗╘╙╚╛╜╝╞╟╠╡╢╣╤╥╦╧╨╩╪╫╬█░▒■▲▶▼◀♦"

// TestConsoleGlyphsAreInEveryConsoleFont: a console draws a glyph its font lacks as '#' or a
// replacement mark, so everything a 256-colour path draws is ASCII or in consoleGlyphs.
func TestConsoleGlyphsAreInEveryConsoleFont(t *testing.T) {
	t.Parallel()
	glyphs := []rune{visual.MissileTrailChar256, visual.OrbFullChar256, visual.HealthBarChar, parameter.OverlayPinMarker256}
	for _, set := range [][]rune{visual.Density256Chars[:], visual.MissileHeadChars256[:], visual.BulletHeadChars256[:],
		visual.BoxDrawSingleLUT[:], visual.BoxDrawDoubleLUT[:]} {
		glyphs = append(glyphs, set...)
	}
	for _, frame := range visual.SwarmPatternChars256 {
		for _, row := range frame {
			glyphs = append(glyphs, row[:]...)
		}
	}
	for _, text := range parameter.AudioText {
		glyphs = append(glyphs, []rune(text)...)
	}
	for _, quadrant := range "▘▝▖▗▀▄▌▐▚▞▛▜▙▟▓" {
		shade, _ := wallShade256(quadrant)
		glyphs = append(glyphs, shade)
	}
	for _, r := range glyphs {
		if (r < ' ' || r > '~') && !strings.ContainsRune(consoleGlyphs, r) {
			t.Errorf("%q is missing from some console font", r)
		}
	}
}
