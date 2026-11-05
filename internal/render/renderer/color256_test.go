package renderer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
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

// TestShield256RimClosesOnEveryAxis: at any radius and cell aspect, a shield's
// 256-colour rim crosses each axis its TrueColor field reaches along, on both sides
// of its centre, and no axis the field leaves bare; and once the field reaches a row
// above its centre, the rim closes round it, so no step between edge-sharing cells
// leads from the centre past the field without crossing it
func TestShield256RimClosesOnEveryAxis(t *testing.T) {
	t.Parallel()
	const w, h, cx, cy = 90, 90, 45, 45
	untouched := color.RGB{R: 1, G: 2, B: 3}
	rc := render.RenderContext{ViewportWidth: w, ViewportHeight: h, MapWidth: w, MapHeight: h}
	look := &visual.ShieldLooks[component.ShieldTypePlayer]
	style := ShieldStyle{Look: look, BlendScale: 1, Color: look.Color, Palette256: look.Palette256}
	radii := []float64{parameter.PlayerShieldRadius, parameter.QuasarShieldRadius, parameter.LootShieldRadius}
	for r := 0.5; r <= 30; r += 0.25 {
		radii = append(radii, r)
	}

	for _, aspect := range []vmath.Aspect{vmath.RefAspect, 0.45, 0.6, 1} {
		field, rim := NewShieldPainter(terminal.ColorModeTrueColor, aspect), NewShieldPainter(terminal.ColorMode256, aspect)
		for _, radius := range radii {
			lit := render.NewRenderBuffer(terminal.ColorModeTrueColor, w, h)
			ring := render.NewRenderBuffer(terminal.ColorMode256, w, h)
			field.Paint(lit, rc, cx, cy, radius, style)
			rim.Paint(ring, rc, cx, cy, radius, style)
			inField := func(x, y int) bool { return lit.BackgroundAt(x, y, untouched) != untouched }
			onRim := func(x, y int) bool { return ring.CellAt(x, y).Attrs&terminal.AttrBg256 != 0 }

			for _, dir := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				reached, crossed := false, false
				for n := 1; n < cx; n++ {
					x, y := cx+dir[0]*n, cy+dir[1]*n
					reached = reached || inField(x, y)
					crossed = crossed || onRim(x, y)
				}
				if reached != crossed {
					t.Errorf("aspect %v radius %v towards %v: field reaches %v, rim crosses %v", aspect, radius, dir, reached, crossed)
				}
			}

			if !inField(cx, cy-1) {
				continue
			}
			seen := map[[2]int]bool{{cx, cy}: true}
			for queue := [][2]int{{cx, cy}}; len(queue) > 0; queue = queue[1:] {
				c := queue[0]
				if c != [2]int{cx, cy} && !inField(c[0], c[1]) {
					t.Errorf("aspect %v radius %v: cell %v past the field is reached from the centre inside the rim", aspect, radius, c)
					break
				}
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					n := [2]int{c[0] + d[0], c[1] + d[1]}
					if !seen[n] && !onRim(n[0], n[1]) {
						seen[n] = true
						queue = append(queue, n)
					}
				}
			}
		}
	}
}

// consoleGlyphs are the non-ASCII symbols in the kernel's CP437 map (Arch's console), in every
// font Ubuntu's setupcon installs for Latin scripts, and in FreeBSD vt's default font
const consoleGlyphs = "·×÷°±•‼←↑→↓≈≡≤≥─│┌┐└┘├┤┬┴┼═║╒╓╔╕╖╗╘╙╚╛╜╝╞╟╠╡╢╣╤╥╦╧╨╩╪╫╬█░▒■▲▶▼◀♦"

// TestConsoleGlyphsAreInEveryConsoleFont: a console draws a glyph its font lacks as '#' or a
// replacement mark, so everything a 256-colour path draws is ASCII or in consoleGlyphs: the
// code's tables, and the shipped scenarios, whose programs' frames and messages draw too.
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
	var logo logoCells
	for _, c := range logo.layout(parameter.AboutLogoRows*2, parameter.AboutLogoRows, vmath.RefAspect, visual.RgbOverlayBg, render.ConsoleFor(render.VGAPalette)) {
		if c.Rune != 0 {
			glyphs = append(glyphs, c.Rune)
		}
	}
	for _, quadrant := range "▘▝▖▗▀▄▌▐▚▞▛▜▙▟▓" {
		shade, _ := wallShade256(quadrant)
		glyphs = append(glyphs, shade)
	}
	// Stone art at every depth a cave reads and on every face, each glyph once
	stone := map[rune]bool{}
	for preset := range pattern.ArtPresetCount {
		for seed := range uint64(4) {
			for i := range 1 << 12 {
				cell := pattern.ArtCell{X: int64(i % 128), Y: int64(i / 128), Depth: i % (pattern.FaceReach + 1),
					Face: pattern.Face(i / 7 % 4), W: stoneGroupWidth, H: 32}
				if r, _, _ := pattern.Art(preset, seed, cell); !stone[r] {
					stone[r] = true
					glyphs = append(glyphs, r)
				}
			}
		}
	}
	for _, r := range glyphs {
		if (r < ' ' || r > '~') && !strings.ContainsRune(consoleGlyphs, r) {
			t.Errorf("%q is missing from some console font", r)
		}
	}
	for path, runes := range shippedScenarioRunes(t) {
		for _, r := range runes {
			if !strings.ContainsRune(consoleGlyphs, r) {
				t.Errorf("%s: %q is missing from some console font", path, r)
			}
		}
	}
}

// shippedScenarioRunes is each shipped scenario file's non-ASCII runes in its string
// values, which hold a program's frames and every message; a key or a comment draws
// nothing, and a macro's body is a string like any other.
func shippedScenarioRunes(t *testing.T) map[string][]rune {
	t.Helper()
	found := map[string][]rune{}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case string:
			for _, r := range v {
				if r > '~' && !strings.ContainsRune(string(found[path]), r) {
					found[path] = append(found[path], r)
				}
			}
		case []any:
			for _, e := range v {
				walk(path, e)
			}
		case []map[string]any:
			for _, e := range v {
				walk(path, e)
			}
		case map[string]any:
			for _, e := range v {
				walk(path, e)
			}
		}
	}
	for _, root := range []string{"../../../wad/scenario", "../../asset/scenario"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".toml" {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			doc, err := toml.NewParser(src).Parse()
			if err != nil {
				return err
			}
			walk(path, doc)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return found
}
