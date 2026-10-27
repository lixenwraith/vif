package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// startPalette is the start screen's colors: lixen.com's in truecolor, console entries in
// 256 colors but for an emulator's button, which keeps the site's through xterm's cube
type startPalette struct {
	bg, button, buttonText, ghost, ghostText color.RGB
}

// StartScreenRenderer draws the start screen while the launch waits for its press on a
// host without one of its own: every row above the status bar in the site's colors, the
// mark centred two fifths of the shorter side, the prompt under it as the site's button,
// and the help button, where help opens, in the top right corner. The status bar stays,
// for commands.
type StartScreenRenderer struct {
	adapter *TUIAdapter
	logo    logoCells
	console *render.Console
	pal     startPalette
	frame   bool // a console's fonts lack the half blocks that round the button
}

// NewStartScreenRenderer creates the start screen renderer
func NewStartScreenRenderer(gameCtx *engine.GameContext) *StartScreenRenderer {
	r := &StartScreenRenderer{
		pal: startPalette{visual.RgbStartBg, visual.RgbStartButton, visual.RgbStartButtonText,
			visual.RgbStartGhost, visual.RgbStartGhostText},
	}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		c := render.ConsoleFor(cfg.ConsolePalette)
		r.console, r.frame = c, cfg.TextConsole
		r.pal.bg, r.pal.ghost, r.pal.ghostText = c.Color(visual.Start256Bg), c.Color(visual.Start256Ghost), c.Color(visual.Start256GhostText)
		if r.frame {
			r.pal.button, r.pal.buttonText = c.Color(visual.Start256Button), c.Color(visual.Start256ButtonText)
		}
	}
	return r
}

// Render implements SystemRenderer
func (r *StartScreenRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	w, h := ctx.ScreenWidth, min(ctx.GameYOffset+ctx.ViewportHeight+1, ctx.ScreenHeight)
	if !ctx.StartScreen || w < 1 || h < 1 {
		return
	}
	if r.adapter == nil {
		r.adapter = NewTUIAdapter(w, h)
	} else if r.adapter.Width() != w || r.adapter.Height() != h {
		r.adapter.Resize(w, h)
	}
	r.adapter.Clear(r.pal.bg)
	root := r.adapter.Region()

	label := tui.Truncate(ctx.Prompt, w-2*parameter.StartButtonPadX)
	bw, bh := tui.RuneLen(label)+2*parameter.StartButtonPadX, 3
	if label == "" {
		bw, bh = 0, 0
	}

	// The mark centred, as the page's is, moved up or shrunk so the button fits below it
	lh := int(math.Round(parameter.StartLogoShare * min(float64(h), float64(w)*float64(ctx.CellAspect))))
	lh = min(lh, h-bh-parameter.StartPromptGap)
	if lh < parameter.StartLogoMinRows || ctx.CellAspect <= 0 {
		lh = 0
	}
	top := (h - lh) / 2
	gap := 0
	if lh > 0 {
		gap = parameter.StartPromptGap
		top = min(top, h-lh-gap-bh)
		lw := int(math.Round(float64(lh) / float64(ctx.CellAspect)))
		region := root.Sub((w-lw)/2, top, lw, lh)
		for i, c := range r.logo.layout(region.W, region.H, ctx.CellAspect, r.pal.bg, r.console) {
			if c.Rune != 0 {
				region.CellOpaque(i%region.W, i/region.W, c.Rune, c.Fg, c.Bg, terminal.AttrNone)
			}
		}
	} else {
		top = (h - bh) / 2
	}
	var button tui.Region
	if bh > 0 {
		button = root.Sub((w-bw)/2, top+lh+gap, bw, bh)
		r.button(button, label)
	}

	if help := render.StartHelpRect(w, ctx.StartHelp); ctx.StartHelp != "" && help.X0 >= 0 && help.Y1 <= h {
		box := root.Sub(help.X0, help.Y0, help.Width(), help.Height())
		box.Box(tui.LineSingle, r.pal.ghost) // the glyph atlas a window draws with has no rounded corners
		box.Text(2, 1, ctx.StartHelp, r.pal.ghostText, r.pal.bg, terminal.AttrNone)
	}

	r.adapter.FlushTo(buf, 0, 0, visual.MaskUI)
	// An emulator in 256 colors draws 0-15 in its theme but xterm's cube as it is
	if r.console != nil && !r.frame && button.H > 0 {
		x, y, bw, bh := button.Bounds()
		buf.SetExact(render.RectWH(x, y, bw, bh))
		// The caps' page halves stay the theme's black, which the cube lacks
		for i := x; i < x+bw && bh >= 3; i++ {
			buf.SetBg256(i, y, visual.Start256Bg)
			buf.SetBg256(i, y+bh-1, visual.Start256Bg)
		}
	}
}

// button draws the prompt as the site's primary button: a filled row between half-block
// rows whose quadrant corners round it, or on a console a frame of lines every font has
func (r *StartScreenRenderer) button(region tui.Region, label string) {
	mid := region.H / 2
	if r.frame {
		if region.H >= 3 {
			region.Box(tui.LineDouble, r.pal.button)
		}
		region.TextCenter(mid, label, r.pal.buttonText, r.pal.bg, terminal.AttrNone)
		return
	}
	region.Sub(0, mid, region.W, 1).Fill(r.pal.button)
	region.TextCenter(mid, label, r.pal.buttonText, r.pal.button, terminal.AttrBold)
	if region.H < 3 {
		return
	}
	for x := range region.W {
		end := 1
		switch x {
		case 0:
			end = 0
		case region.W - 1:
			end = 2
		}
		region.CellOpaque(x, mid-1, visual.ButtonCapTop[end], r.pal.button, r.pal.bg, terminal.AttrNone)
		region.CellOpaque(x, mid+1, visual.ButtonCapBottom[end], r.pal.button, r.pal.bg, terminal.AttrNone)
	}
}
