package renderer

import (
	"slices"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// Peer cursors keep their slot colour and hexadecimal label across instances.
type PeerCursorRenderer struct {
	gameCtx    *engine.GameContext
	renderCell peerCursorCellRenderer
	slots256   []uint8
}

// peerCursorCellRenderer draws one peer cursor in the colour mode chosen at construction
type peerCursorCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, char rune, slot uint8)

// peerSlotOrder256 are the console backgrounds peers take in turn: none that the local cursor
// wears (yellow, white, error red) or the playfield's black
var peerSlotOrder256 = [...]uint8{
	visual.ConCyan, visual.ConMagenta, visual.ConGreen, visual.ConBlue,
	visual.ConBrightCyan, visual.ConBrightMagenta, visual.ConBrightGreen, visual.ConBrightBlue,
}

// NewPeerCursorRenderer creates a renderer for the non-local cursors.
func NewPeerCursorRenderer(gameCtx *engine.GameContext) *PeerCursorRenderer {
	r := &PeerCursorRenderer{gameCtx: gameCtx}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		// A bright background shows as its normal counterpart on eight-background consoles,
		// so only the colours this one tells apart are kept
		c := render.ConsoleFor(cfg.ConsolePalette)
		for _, e := range peerSlotOrder256 {
			if bg := c.Background(e); !slices.Contains(r.slots256, bg) {
				r.slots256 = append(r.slots256, bg)
			}
		}
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

// IsVisible reports whether the roster holds a cursor other than this one's.
// A solo run draws nothing and pays one integer compare for the privilege.
func (r *PeerCursorRenderer) IsVisible() bool {
	return r.gameCtx.World.Resources.Player.Count() > 1
}

// Render draws every rostered cursor this instance does not simulate.
func (r *PeerCursorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	roster := world.Resources.Player

	buf.SetWriteMask(visual.MaskUI)

	world.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if roster.IsLocal(e) {
			return true
		}
		pos, ok := world.Positions.GetPosition(e)
		if !ok {
			return true
		}
		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		char := rune("0123456789ABCDEF"[c.Slot%16])

		r.renderCell(buf, screenX, screenY, char, c.Slot)
		return true
	})
}

func (r *PeerCursorRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, char rune, slot uint8) {
	buf.SetWithBg(screenX, screenY, char, visual.RgbPeerCursorText, peerCursorColor(slot))
}

func (r *PeerCursorRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, char rune, slot uint8) {
	buf.SetWithBg(screenX, screenY, char, visual.RgbPeerCursorText, visual.RgbBlack)
	buf.SetBg256(screenX, screenY, r.slots256[int(slot)%len(r.slots256)])
}

// peerCursorColor is one roster slot's colour, wrapped so a slot beyond the
// palette is drawn in some peer's colour rather than in none.
func peerCursorColor(slot uint8) color.RGB {
	return visual.RgbPeerCursor[int(slot)%len(visual.RgbPeerCursor)]
}
