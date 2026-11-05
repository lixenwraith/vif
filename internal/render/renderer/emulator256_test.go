package renderer

import (
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TestEmulatorUIKeepsItsOwnColors: an emulator in 256 colors draws the console's entries in
// its theme, so the status bar and the overlay leave the console pass with truecolor's colors,
// which xterm's cube shows as they are, the bar labelled 256; a text console gets both in its
// entries.
func TestEmulatorUIKeepsItsOwnColors(t *testing.T) {
	t.Parallel()
	const w, h = 80, 24
	for _, textConsole := range []bool{true, false} {
		ctx := engine.NewGameContextWithClock(engine.NewWorld(), w, h, engine.NewManualClock())
		cfg := ctx.World.Resources.Config
		cfg.ColorMode, cfg.TextConsole = terminal.ColorMode256, textConsole
		bar, overlay := NewStatusBarRenderer(ctx), NewOverlayRenderer(ctx)
		ctx.SetOverlayContent(&core.OverlayContent{Title: "Menu", Layout: core.OverlayLayoutMenu,
			Menu: &core.OverlayMenu{Rows: []core.OverlayMenuRow{{Key: "game", Label: "Game", Value: ">"}}}})

		rc := statusContext(w)
		buf := render.NewRenderBuffer(terminal.ColorMode256, w, h)
		buf.SetConsole(render.ConsoleFor(cfg.ConsolePalette), !textConsole)
		bar.Render(rc, buf)
		overlay.Render(rc, buf)
		var frame frameSurface
		buf.FlushToTerminal(&frame)

		itemsY := rc.GameYOffset + rc.ViewportHeight + 1
		var row strings.Builder
		for x := range w {
			row.WriteRune(frame.cells[itemsY*w+x].Rune)
		}
		label := strings.Index(row.String(), " 256 ")
		g := ctx.OverlayGeometry()
		border, blank := frame.cells[g.Y*w+g.X], frame.cells[(itemsY+parameter.StatusRows-1)*w+w-1]
		if label < 0 {
			t.Fatalf("console %v: the bar has no 256 label: %q", textConsole, row.String())
		}
		tag := frame.cells[itemsY*w+label+1]
		if textConsole {
			for _, cell := range []terminal.Cell{tag, border, blank} {
				if cell.Attrs != terminal.AttrFg256|terminal.AttrBg256 {
					t.Errorf("console: %q left as %+v, want the console's entries", cell.Rune, cell)
				}
			}
			continue
		}
		if tag.Bg != visual.RgbColorModeIndicator || tag.Attrs != terminal.AttrNone {
			t.Errorf("emulator: the 256 label left as %+v, want truecolor's", tag)
		}
		if border.Fg != visual.RgbOverlayBorder || border.Bg != visual.RgbOverlayBg || border.Attrs != terminal.AttrNone {
			t.Errorf("emulator: the overlay's border left as %+v, want its own colors", border)
		}
		if blank.Bg != visual.RgbBackground || blank.Attrs != terminal.AttrNone {
			t.Errorf("emulator: the bar's background left as %+v, want the theme background", blank)
		}
	}
}
