package renderer

import (
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestStartScreenAsksOnItsButton: the start screen covers every row above the status bar
// with the prompt on its button, adds the help button only where help opens, draws
// nothing once it is down, and on a console uses only glyphs every console font has.
func TestStartScreenAsksOnItsButton(t *testing.T) {
	t.Parallel()
	const w, h, prompt = 80, 24, "Click or press a key to start"
	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		ctx := engine.NewGameContext(engine.NewWorld(), w, h)
		ctx.World.Resources.Config.ColorMode = mode
		r := NewStartScreenRenderer(ctx)
		rc := render.RenderContext{CellAspect: vmath.RefAspect, ScreenWidth: w, ScreenHeight: h,
			GameXOffset: 1, GameYOffset: 1, ViewportWidth: w - 1, ViewportHeight: h - 4, Prompt: prompt}
		page := rc.GameYOffset + rc.ViewportHeight + 1
		draw := func(rc render.RenderContext) string {
			buf := render.NewRenderBuffer(mode, w, h)
			buf.Clear()
			r.Render(rc, buf)
			var text strings.Builder
			for y := range h {
				for x := range w {
					c := buf.CellAt(x, y)
					if y >= page && c.Rune != 0 {
						t.Errorf("%v: drew %q on the status bar at %d,%d", mode, c.Rune, x, y)
					}
					if mode == terminal.ColorMode256 && c.Rune > '~' && !strings.ContainsRune(consoleGlyphs, c.Rune) {
						t.Errorf("%v: %q is missing from some console font", mode, c.Rune)
					}
					text.WriteRune(max(c.Rune, ' '))
				}
				text.WriteByte('\n')
			}
			return text.String()
		}

		if text := draw(rc); strings.TrimSpace(text) != "" {
			t.Fatalf("%v: drew with the start screen down:\n%s", mode, text)
		}
		rc.StartScreen = true
		if text := draw(rc); !strings.Contains(text, prompt) || strings.Contains(text, parameter.StartHelpWord) {
			t.Fatalf("%v: a join's wait shows the prompt and no help button:\n%s", mode, text)
		}
		rc.StartHelp = "F1 " + parameter.StartHelpWord
		text := draw(rc)
		help := render.StartHelpRect(w, rc.StartHelp)
		if row := []rune(strings.Split(text, "\n")[help.Y0+1]); !strings.Contains(string(row[help.X0:help.X1]), rc.StartHelp) {
			t.Fatalf("%v: the help button is not where a click opens help:\n%s", mode, text)
		}
	}
}
