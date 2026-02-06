package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Loot shield ellipse radii in Q32.32
// 5×3 footprint: horizontal ±2, vertical ±1
var (
	lootRadiusX    = vmath.FromFloat(2.0)
	lootRadiusY    = vmath.FromFloat(1.0)
	lootInvRxSq    int64
	lootInvRySq    int64
	lootMaxOpacity = vmath.FromFloat(0.9)

	// Center threshold: normalizedDistSq < this renders inner rune instead of bg-only
	// 0.15² ≈ 0.02 in Q32.32 — only the center cell
	lootCenterThreshold = vmath.FromFloat(0.02)

	// 256-color palette
	loot256Rim    uint8 = 198 // Hot pink
	loot256Center uint8 = 16  // Near black
)

func init() {
	lootInvRxSq, lootInvRySq = vmath.EllipseInvRadiiSq(lootRadiusX, lootRadiusY)
}

// lootCellRenderer matches shieldCellRenderer signature for future unification
type lootCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

type LootRenderer struct {
	gameCtx *engine.GameContext

	renderCell lootCellRenderer

	// Per-entity frame state
	frameInnerRune  rune
	frameInnerColor terminal.RGB

	// Boost glow state (future: rotating indicator)
	boostGlowActive  bool
	rotDirX, rotDirY int64
}

func NewLootRenderer(ctx *engine.GameContext) *LootRenderer {
	r := &LootRenderer{
		gameCtx: ctx,
	}

	if ctx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}

	return r
}

func (r *LootRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Loot.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, entity := range entities {
		loot, ok := r.gameCtx.World.Components.Loot.GetComponent(entity)
		if !ok {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		r.frameInnerRune = loot.Rune
		r.frameInnerColor = loot.InnerColor

		radiusXInt := vmath.ToInt(lootRadiusX)
		radiusYInt := vmath.ToInt(lootRadiusY)

		startX := max(0, pos.X-radiusXInt)
		endX := min(ctx.GameWidth-1, pos.X+radiusXInt)
		startY := max(0, pos.Y-radiusYInt)
		endY := min(ctx.GameHeight-1, pos.Y+radiusYInt)

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				dx := vmath.FromInt(x - pos.X)
				dy := vmath.FromInt(y - pos.Y)

				normalizedDistSq := vmath.EllipseDistSq(dx, dy, lootInvRxSq, lootInvRySq)
				if normalizedDistSq > vmath.Scale {
					continue
				}

				screenX := ctx.GameXOffset + x
				screenY := ctx.GameYOffset + y

				if screenX < ctx.GameXOffset || screenX >= ctx.GameXOffset+ctx.GameWidth ||
					screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
					continue
				}

				r.renderCell(buf, screenX, screenY, normalizedDistSq)
			}
		}
	}
}

// cellTrueColor renders loot shield cell with inverted gradient (bright edge → dark center)
// Aligned with ShieldRenderer.cellTrueColor for future unification
func (r *LootRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// Center cell: draw inner rune on near-black bg
	if normalizedDistSq < lootCenterThreshold {
		buf.Set(screenX, screenY, r.frameInnerRune,
			visual.RgbLootShieldCenter, r.frameInnerColor,
			render.BlendReplace, 1.0, terminal.AttrNone)
		return
	}

	// Bg-only shield: normalizedDistSq drives alpha (same curve as ShieldRenderer)
	// Dark center → bright pink edge via BlendScreen
	alphaFixed := vmath.Mul(normalizedDistSq, lootMaxOpacity)
	alpha := vmath.ToFloat(alphaFixed)

	buf.Set(screenX, screenY, 0, visual.RgbBlack,
		visual.RgbLootShieldBorder, render.BlendScreen, alpha, terminal.AttrNone)
}

// cell256 renders loot shield cell for 256-color mode
func (r *LootRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// Center cell: inner rune
	if normalizedDistSq < lootCenterThreshold {
		buf.SetBg256(screenX, screenY, loot256Center)
		buf.SetFgOnly(screenX, screenY, r.frameInnerRune, r.frameInnerColor, terminal.AttrNone)
		return
	}

	// Rim only beyond threshold (same pattern as ShieldRenderer.cell256)
	if normalizedDistSq < vmath.FromFloat(0.36) {
		return // Transparent inner zone
	}

	buf.SetBg256(screenX, screenY, loot256Rim)
}