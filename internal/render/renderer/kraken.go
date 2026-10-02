package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

type KrakenRenderer struct {
	gameCtx *engine.GameContext
	cells   []terminal.Cell
	palette bool
}

func NewKrakenRenderer(ctx *engine.GameContext) *KrakenRenderer {
	return &KrakenRenderer{gameCtx: ctx, palette: ctx.World.Resources.Config.ColorMode == terminal.ColorMode256}
}

func (r *KrakenRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	width, height := maxX-minX+1, maxY-minY+1
	if width <= 0 || height <= 0 {
		return
	}
	buf.SetWriteMask(visual.MaskComposite)
	for _, e := range r.gameCtx.World.Components.Kraken.Entities() {
		k, _ := r.gameCtx.World.Components.Kraken.GetPtr(e)
		motion, ok := r.gameCtx.World.Components.Kinetic.GetPtr(e)
		if !ok {
			continue
		}
		if cap(r.cells) < width*height {
			r.cells = make([]terminal.Cell, width*height)
		} else {
			r.cells = r.cells[:width*height]
			clear(r.cells)
		}
		k.TentacleSamples(motion.PreciseX, motion.PreciseY, func(x, y, radius, step float64, attacking bool) {
			heat := step * step * k.MoveBlend * 0.4
			if attacking {
				heat = max(heat, step*step*k.AttackT)
			}
			c := color.Lerp(visual.RgbKrakenLegBase, visual.RgbKrakenLegTip, step)
			c = color.Lerp(c, visual.RgbKrakenAttack, heat)
			for cy := max(minY, int(math.Ceil(y-(radius+1.8)/2-0.5))); cy <= min(maxY, int(math.Floor(y+(radius+1.8)/2-0.5))); cy++ {
				for cx := max(minX, int(math.Ceil(x-radius-1.8-0.5))); cx <= min(maxX, int(math.Floor(x+radius+1.8-0.5))); cx++ {
					dx, dy := float64(cx)+0.5-x, (float64(cy)+0.5-y)*2
					distance := math.Hypot(dx, dy)
					cell := &r.cells[(cy-minY)*width+cx-minX]
					if distance <= radius {
						*cell = terminal.Cell{Rune: ' ', Fg: c, Bg: c}
					} else if cell.Rune == 0 {
						fraction := (distance - radius) / 1.8
						var edge rune
						switch {
						case fraction < 0.25:
							edge = '▓'
						case fraction < 0.55:
							edge = '▒'
						case fraction < 0.85:
							edge = '░'
						}
						if edge != 0 {
							*cell = terminal.Cell{Rune: edge, Fg: c, Bg: visual.RgbBackground}
						}
					}
				}
			}
		})
		// The sandbox's default void profile fades smoothly through the leg roots.
		x, y := motion.PreciseX, motion.PreciseY
		rx, ry := parameter.KrakenBodyRadius*2, parameter.KrakenBodyRadius
		for cy := max(minY, int(math.Ceil(y-ry-0.5))); cy <= min(maxY, int(y+ry-0.5)); cy++ {
			for cx := max(minX, int(math.Ceil(x-rx-0.5))); cx <= min(maxX, int(x+rx-0.5)); cx++ {
				distance := math.Hypot((float64(cx)+0.5-x)/rx, (float64(cy)+0.5-y)/ry)
				if distance > 1 {
					continue
				}
				cell := &r.cells[(cy-minY)*width+cx-minX]
				if cell.Rune == 0 {
					*cell = terminal.Cell{Rune: ' ', Bg: visual.RgbBackground}
				}
				alpha := 1 - distance
				alpha *= alpha * (3 - 2*alpha)
				cell.Bg = color.Lerp(cell.Bg, visual.RgbKrakenVoid, alpha)
				if alpha > 0.3 {
					cell.Rune = ' '
				}
			}
		}
		for i, cell := range r.cells {
			if cell.Rune == 0 {
				continue
			}
			sx, sy, visible := ctx.MapToScreen(minX+i%width, minY+i/width)
			if !visible {
				continue
			}
			if r.palette {
				buf.SetBg256(sx, sy, color.RGBTo256(cell.Bg))
				buf.SetFgOnly(sx, sy, cell.Rune, color.RGB{R: color.RGBTo256(cell.Fg)}, terminal.AttrFg256)
			} else {
				buf.SetWithBg(sx, sy, cell.Rune, cell.Fg, cell.Bg)
			}
		}
	}
}
