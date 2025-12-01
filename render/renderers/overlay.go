package renderers

import (
	"fmt"

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

	// Draw top border with title
	buf.SetWithBg(startX, startY, '╔', render.RgbOverlayBorder, render.RgbOverlayBg)
	for x := 1; x < overlayWidth-1; x++ {
		buf.SetWithBg(startX+x, startY, '═', render.RgbOverlayBorder, render.RgbOverlayBg)
	}
	buf.SetWithBg(startX+overlayWidth-1, startY, '╗', render.RgbOverlayBorder, render.RgbOverlayBg)

	// Draw title centered on top border
	if o.gameCtx.OverlayTitle != "" {
		titleX := startX + (overlayWidth-len(o.gameCtx.OverlayTitle))/2
		if titleX > startX {
			for i, ch := range o.gameCtx.OverlayTitle {
				if titleX+i < startX+overlayWidth-1 {
					buf.SetWithBg(titleX+i, startY, ch, render.RgbOverlayTitle, render.RgbOverlayBg)
				}
			}
		}
	}

	// Draw content area and side borders
	contentHeight := overlayHeight - 2
	contentWidth := overlayWidth - 2

	for y := 1; y < overlayHeight-1; y++ {
		screenY := startY + y
		// Left border
		buf.SetWithBg(startX, screenY, '║', render.RgbOverlayBorder, render.RgbOverlayBg)
		// Content area
		for x := 1; x < overlayWidth-1; x++ {
			buf.SetWithBg(startX+x, screenY, ' ', render.RgbOverlayText, render.RgbOverlayBg)
		}
		// Right border
		buf.SetWithBg(startX+overlayWidth-1, screenY, '║', render.RgbOverlayBorder, render.RgbOverlayBg)
	}

	// Draw bottom border
	buf.SetWithBg(startX, startY+overlayHeight-1, '╚', render.RgbOverlayBorder, render.RgbOverlayBg)
	for x := 1; x < overlayWidth-1; x++ {
		buf.SetWithBg(startX+x, startY+overlayHeight-1, '═', render.RgbOverlayBorder, render.RgbOverlayBg)
	}
	buf.SetWithBg(startX+overlayWidth-1, startY+overlayHeight-1, '╝', render.RgbOverlayBorder, render.RgbOverlayBg)

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
				buf.SetWithBg(contentStartX+j, lineY, ch, render.RgbOverlayText, render.RgbOverlayBg)
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
			buf.SetWithBg(scrollX+i, scrollY, ch, render.RgbOverlayBorder, render.RgbOverlayBg)
		}
	}
}