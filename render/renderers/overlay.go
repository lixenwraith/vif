package renderers

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// OverlayRenderer draws the modal overlay window
type OverlayRenderer struct {
	gameCtx *engine.GameContext
}

// NewOverlayRenderer creates a new overlay renderer
func NewOverlayRenderer(gameCtx *engine.GameContext) *OverlayRenderer {
	return &OverlayRenderer{
		gameCtx: gameCtx,
	}
}

// IsVisible returns true when the overlay should be rendered
func (o *OverlayRenderer) IsVisible() bool {
	return o.gameCtx.IsOverlayMode() && o.gameCtx.OverlayActive
}

// Render draws the overlay window
func (o *OverlayRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)

	// Calculate overlay dimensions (80% of screen)
	overlayWidth := int(float64(ctx.Width) * constants.OverlayWidthPercent)
	overlayHeight := int(float64(ctx.Height) * constants.OverlayHeightPercent)

	// Ensure minimum dimensions
	if overlayWidth < 20 {
		overlayWidth = 20
	}
	if overlayHeight < 10 {
		overlayHeight = 10
	}

	// Ensure it fits on screen
	if overlayWidth > ctx.Width-4 {
		overlayWidth = ctx.Width - 4
	}
	if overlayHeight > ctx.Height-4 {
		overlayHeight = ctx.Height - 4
	}

	// Calculate centered position
	startX := (ctx.Width - overlayWidth) / 2
	startY := (ctx.Height - overlayHeight) / 2

	// Define styles
	borderStyle := defaultStyle.Foreground(render.RgbOverlayBorder).Background(render.RgbOverlayBg)
	bgStyle := defaultStyle.Foreground(render.RgbOverlayText).Background(render.RgbOverlayBg)
	titleStyle := defaultStyle.Foreground(render.RgbOverlayTitle).Background(render.RgbOverlayBg)

	// Draw top border with title
	buf.Set(startX, startY, '╔', borderStyle)
	for x := 1; x < overlayWidth-1; x++ {
		buf.Set(startX+x, startY, '═', borderStyle)
	}
	buf.Set(startX+overlayWidth-1, startY, '╗', borderStyle)

	// Draw title centered on top border
	if o.gameCtx.OverlayTitle != "" {
		titleX := startX + (overlayWidth-len(o.gameCtx.OverlayTitle))/2
		if titleX > startX {
			for i, ch := range o.gameCtx.OverlayTitle {
				if titleX+i < startX+overlayWidth-1 {
					buf.Set(titleX+i, startY, ch, titleStyle)
				}
			}
		}
	}

	// Draw content area and side borders
	contentHeight := overlayHeight - 2
	contentWidth := overlayWidth - 2

	for y := 1; y < overlayHeight-1; y++ {
		// Left border
		buf.Set(startX, startY+y, '║', borderStyle)

		// Fill background
		for x := 1; x < overlayWidth-1; x++ {
			buf.Set(startX+x, startY+y, ' ', bgStyle)
		}

		// Right border
		buf.Set(startX+overlayWidth-1, startY+y, '║', borderStyle)
	}

	// Draw bottom border
	buf.Set(startX, startY+overlayHeight-1, '╚', borderStyle)
	for x := 1; x < overlayWidth-1; x++ {
		buf.Set(startX+x, startY+overlayHeight-1, '═', borderStyle)
	}
	buf.Set(startX+overlayWidth-1, startY+overlayHeight-1, '╝', borderStyle)

	// Draw content lines
	contentStartY := startY + 1 + constants.OverlayPaddingY
	contentStartX := startX + constants.OverlayPaddingX
	maxContentLines := contentHeight - 2*constants.OverlayPaddingY

	// Calculate visible range based on scroll
	startLine := o.gameCtx.OverlayScroll
	endLine := startLine + maxContentLines
	if endLine > len(o.gameCtx.OverlayContent) {
		endLine = len(o.gameCtx.OverlayContent)
	}

	// Draw visible content lines
	lineY := contentStartY
	for i := startLine; i < endLine && lineY < startY+overlayHeight-1-constants.OverlayPaddingY; i++ {
		line := o.gameCtx.OverlayContent[i]
		maxLineWidth := contentWidth - 2*constants.OverlayPaddingX

		// Truncate line if too long
		displayLine := line
		if len(line) > maxLineWidth {
			displayLine = line[:maxLineWidth]
		}

		// Draw the line
		for j, ch := range displayLine {
			if contentStartX+j < startX+overlayWidth-1-constants.OverlayPaddingX {
				buf.Set(contentStartX+j, lineY, ch, bgStyle)
			}
		}
		lineY++
	}

	// Draw scroll indicator if content is scrollable
	if len(o.gameCtx.OverlayContent) > maxContentLines {
		scrollInfo := fmt.Sprintf("[%d/%d]", o.gameCtx.OverlayScroll+1, len(o.gameCtx.OverlayContent)-maxContentLines+1)
		scrollX := startX + overlayWidth - len(scrollInfo) - 2
		scrollY := startY + overlayHeight - 1
		for i, ch := range scrollInfo {
			buf.Set(scrollX+i, scrollY, ch, borderStyle)
		}
	}
}