package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// === Render Data ===

const maxEyeFrames = 5

// eye256FlashFg is xterm-256 bright yellow for hit flash
const eye256FlashFg uint8 = 226

// eyeCell holds resolved per-cell render data for one animation frame position
type eyeCell struct {
	Ch    rune
	FgIdx int8          // -1 = no foreground
	BgIdx int8          // -1 = transparent (preserve underlying bg)
	Attr  terminal.Attr // style bits only (Bold, Dim)
}

// eyeTypeRender holds compiled render data for one eye type
type eyeTypeRender struct {
	FgPalette  [8]terminal.RGB
	BgPalette  [3]terminal.RGB
	Fg256      uint8
	Bg256      uint8
	FrameCount int
	Frames     [maxEyeFrames][parameter.EyeHeight][parameter.EyeWidth]eyeCell
}

var eyeRenderTable [parameter.EyeTypeCount]eyeTypeRender

// === Init: Parse Frame Specs → Render Table ===

type eyeFrameStrings struct {
	art  [3]string
	fg   [3]string
	bg   [3]string
	attr [3]string
}

type eyeTypeSpec struct {
	fgPalette  [8]terminal.RGB
	bgPalette  [3]terminal.RGB
	fg256      uint8
	bg256      uint8
	frameCount int
	frames     [maxEyeFrames]eyeFrameStrings
}

func parsePaletteIdx(b byte) int8 {
	if b >= '0' && b <= '9' {
		return int8(b - '0')
	}
	if b >= 'a' && b <= 'f' {
		return int8(b-'a') + 10
	}
	return -1
}

func parseAttr(b byte) terminal.Attr {
	switch b {
	case 'B':
		return terminal.AttrBold
	case 'D':
		return terminal.AttrDim
	default:
		return terminal.AttrNone
	}
}

func buildEyeType(spec *eyeTypeSpec) eyeTypeRender {
	r := eyeTypeRender{
		FgPalette:  spec.fgPalette,
		BgPalette:  spec.bgPalette,
		Fg256:      spec.fg256,
		Bg256:      spec.bg256,
		FrameCount: spec.frameCount,
	}
	for f := 0; f < spec.frameCount; f++ {
		fs := &spec.frames[f]
		for row := 0; row < parameter.EyeHeight; row++ {
			for col := 0; col < parameter.EyeWidth; col++ {
				cell := &r.Frames[f][row][col]
				if col < len(fs.art[row]) {
					ch := rune(fs.art[row][col])
					if ch != ' ' {
						cell.Ch = ch
					}
				}
				if col < len(fs.fg[row]) {
					cell.FgIdx = parsePaletteIdx(fs.fg[row][col])
				} else {
					cell.FgIdx = -1
				}
				if col < len(fs.bg[row]) {
					cell.BgIdx = parsePaletteIdx(fs.bg[row][col])
				} else {
					cell.BgIdx = -1
				}
				if col < len(fs.attr[row]) {
					cell.Attr = parseAttr(fs.attr[row][col])
				}
			}
		}
	}
	return r
}

func init() {
	specs := [parameter.EyeTypeCount]eyeTypeSpec{

		// 0: Void Eye — 5×3, 5 frames
		// Deep ocean, slow blink cycle (O→o→=→O→shut)
		{
			fgPalette: [8]terminal.RGB{
				terminal.DimGray, terminal.SteelBlue, terminal.White,
				terminal.CeruleanBlue, terminal.NavyBlue, terminal.LightSkyBlue,
				terminal.CobaltBlue, terminal.DodgerBlue,
			},
			bgPalette: [3]terminal.RGB{
				terminal.DeepNavy, terminal.Gunmetal, terminal.CobaltBlue,
			},
			fg256: 75, bg256: 17,
			frameCount: 5,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{"[---]", "|(O)|", "[---]"},
					fg:   [3]string{"01110", "43234", "01110"},
					bg:   [3]string{"00000", "01210", "00000"},
					attr: [3]string{" BBB ", " BBB ", " BBB "},
				},
				{
					art:  [3]string{"[---]", "|(o)|", "[---]"},
					fg:   [3]string{"01110", "43534", "01110"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{" BBB ", " BBB ", " BBB "},
				},
				{
					art:  [3]string{"[===]", "|(=)|", "[===]"},
					fg:   [3]string{"06660", "43634", "06660"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{" BBB ", "  B  ", " BBB "},
				},
				{
					art:  [3]string{"[~~~]", "|(O)|", "[~~~]"},
					fg:   [3]string{"07770", "43234", "07770"},
					bg:   [3]string{"00100", "01210", "00100"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
				{
					art:  [3]string{"[---]", "|===|", "[---]"},
					fg:   [3]string{"01110", "46664", "01110"},
					bg:   [3]string{"00000", "00000", "00000"},
					attr: [3]string{" BBB ", "     ", " BBB "},
				},
			},
		},

		// 1: Flame Eye — 5×3, 4 frames
		// Aggressive flicker (<@>→{*}→<o>→<O>)
		{
			fgPalette: [8]terminal.RGB{
				terminal.LemonYellow, terminal.FlameOrange, terminal.White,
				terminal.BrightRed, terminal.Amber, terminal.DarkCrimson,
				terminal.Vermilion, terminal.WarmOrange,
			},
			bgPalette: [3]terminal.RGB{
				terminal.BlackRed, terminal.DarkAmber, terminal.Red,
			},
			fg256: 208, bg256: 88,
			frameCount: 4,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{"#---#", "|<@>|", "#---#"},
					fg:   [3]string{"51115", "54245", "51115"},
					bg:   [3]string{"00000", "01210", "00000"},
					attr: [3]string{"B   B", " BBB ", "B   B"},
				},
				{
					art:  [3]string{"#-#-#", "|{*}|", "#-#-#"},
					fg:   [3]string{"51615", "57275", "51615"},
					bg:   [3]string{"01010", "01210", "01010"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
				{
					art:  [3]string{"#---#", "|<o>|", "#---#"},
					fg:   [3]string{"51115", "54745", "51115"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"B   B", " BBB ", "B   B"},
				},
				{
					art:  [3]string{"#===#", "|<O>|", "#===#"},
					fg:   [3]string{"50005", "54245", "50005"},
					bg:   [3]string{"01110", "01210", "01110"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
			},
		},

		// 2: Frost Eye — 5×3, 4 frames
		// Crystalline pulse (<O>→(O)→{=}→(O))
		{
			fgPalette: [8]terminal.RGB{
				terminal.BrightCyan, terminal.White, terminal.LightSkyBlue,
				terminal.CeruleanBlue, terminal.SteelBlue, terminal.CoolSilver,
				terminal.AliceBlue, terminal.PaleCyan,
			},
			bgPalette: [3]terminal.RGB{
				terminal.DeepNavy, terminal.CobaltBlue, terminal.SteelBlue,
			},
			fg256: 81, bg256: 18,
			frameCount: 4,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{"*---*", "|<O>|", "*---*"},
					fg:   [3]string{"43334", "30103", "43334"},
					bg:   [3]string{"00000", "01210", "00000"},
					attr: [3]string{"B   B", " BBB ", "B   B"},
				},
				{
					art:  [3]string{"*-+-*", "|(O)|", "*-+-*"},
					fg:   [3]string{"43134", "30103", "43134"},
					bg:   [3]string{"00100", "01210", "00100"},
					attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
				},
				{
					art:  [3]string{"*---*", "|{=}|", "*---*"},
					fg:   [3]string{"43334", "30534", "43334"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"B   B", "  B  ", "B   B"},
				},
				{
					art:  [3]string{"*~+~*", "|(O)|", "*~+~*"},
					fg:   [3]string{"40104", "30103", "40104"},
					bg:   [3]string{"01210", "01210", "01210"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
			},
		},

		// 3: Storm Eye — 6→5×3, 3 frames (column 3 dropped)
		// Electric, rotating highlight
		{
			fgPalette: [8]terminal.RGB{
				terminal.BrightCyan, terminal.CeruleanBlue, terminal.White,
				terminal.LemonYellow, terminal.SteelBlue, terminal.DodgerBlue,
				terminal.SkyTeal, terminal.LightSkyBlue,
			},
			bgPalette: [3]terminal.RGB{
				terminal.DeepNavy, terminal.CobaltBlue, {},
			},
			fg256: 51, bg256: 17,
			frameCount: 3,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{"+~~~+", "|(O)|", "+~~~+"},
					fg:   [3]string{"40004", "41214", "40004"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
				},
				{
					art:  [3]string{"+~~~+", "|(=)|", "+~~~+"},
					fg:   [3]string{"40004", "41614", "40004"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"BBBBB", " B B ", "BBBBB"},
				},
				{
					art:  [3]string{"+~~~+", "|{O}|", "+~~~+"},
					fg:   [3]string{"43034", "43234", "43034"},
					bg:   [3]string{"00100", "01110", "00100"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
			},
		},

		// 4: Blood Eye — 5×3, 4 frames
		// Veined pulse (X pupil, dilate cycle)
		{
			fgPalette: [8]terminal.RGB{
				terminal.DarkCrimson, terminal.BrightRed, terminal.White,
				terminal.Vermilion, terminal.Coral, terminal.Red,
				terminal.Salmon, terminal.LightCoral,
			},
			bgPalette: [3]terminal.RGB{
				terminal.BlackRed, terminal.DarkCrimson, terminal.Red,
			},
			fg256: 160, bg256: 52,
			frameCount: 4,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{">---<", "|(X)|", ">---<"},
					fg:   [3]string{"31113", "05250", "31113"},
					bg:   [3]string{"00000", "01210", "00000"},
					attr: [3]string{"B   B", " BBB ", "B   B"},
				},
				{
					art:  [3]string{">===<", "|(X)|", ">===<"},
					fg:   [3]string{"35553", "05250", "35553"},
					bg:   [3]string{"01110", "01210", "01110"},
					attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
				},
				{
					art:  [3]string{">---<", "|-X-|", ">---<"},
					fg:   [3]string{"31113", "05250", "31113"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"B   B", "  B  ", "B   B"},
				},
				{
					art:  [3]string{">-#-<", "|(O)|", ">-#-<"},
					fg:   [3]string{"31513", "04240", "31513"},
					bg:   [3]string{"00100", "01210", "00100"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
			},
		},

		// 5: Golden Eye — 6→5×3, 4 frames (column 3 dropped)
		// Regal shimmer, warm amber
		{
			fgPalette: [8]terminal.RGB{
				terminal.Gold, terminal.Amber, terminal.White,
				terminal.LemonYellow, terminal.DarkGold, terminal.PaleGold,
				terminal.Buttercream, terminal.WarmOrange,
			},
			bgPalette: [3]terminal.RGB{
				terminal.DarkAmber, terminal.Amber, terminal.Gold,
			},
			fg256: 220, bg256: 94,
			frameCount: 4,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{"|===|", "|(O)|", "|===|"},
					fg:   [3]string{"40004", "41214", "40004"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
				},
				{
					art:  [3]string{"|=#=|", "|{O}|", "|=#=|"},
					fg:   [3]string{"40304", "71217", "40304"},
					bg:   [3]string{"00100", "01110", "00100"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
				{
					art:  [3]string{"|===|", "|(=)|", "|===|"},
					fg:   [3]string{"40004", "41514", "40004"},
					bg:   [3]string{"00000", "01110", "00000"},
					attr: [3]string{"BBBBB", " B B ", "BBBBB"},
				},
				{
					art:  [3]string{"|~#~|", "|(O)|", "|~#~|"},
					fg:   [3]string{"43334", "41214", "43334"},
					bg:   [3]string{"01210", "01210", "01210"},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
			},
		},

		// 6: Abyss Eye — 5×3, 4 frames
		// Transparent corners (bg skip), dimensional rift
		{
			fgPalette: [8]terminal.RGB{
				terminal.PaleLavender, terminal.ElectricViolet, terminal.White,
				terminal.SoftLavender, terminal.DarkViolet, terminal.MutedPurple,
				terminal.DeepPurple, terminal.Orchid,
			},
			bgPalette: [3]terminal.RGB{
				terminal.Obsidian, terminal.DeepPurple, {},
			},
			fg256: 135, bg256: 54,
			frameCount: 4,
			frames: [maxEyeFrames]eyeFrameStrings{
				{
					art:  [3]string{".---.", "|(O)|", "'---'"},
					fg:   [3]string{"64446", "41214", "64446"},
					bg:   [3]string{" 000 ", "01110", " 000 "},
					attr: [3]string{" BBB ", " BBB ", " BBB "},
				},
				{
					art:  [3]string{".---.", "|{O}|", "'---'"},
					fg:   [3]string{"64446", "47274", "64446"},
					bg:   [3]string{" 000 ", "01110", " 000 "},
					attr: [3]string{" BBB ", " BBB ", " BBB "},
				},
				{
					art:  [3]string{".~~~.", "|[O]|", "'~~~'"},
					fg:   [3]string{"65556", "41214", "65556"},
					bg:   [3]string{" 111 ", "01110", " 111 "},
					attr: [3]string{"DBBBD", " BBB ", "DBBBD"},
				},
				{
					art:  [3]string{".~~~.", "|(O)|", "'~~~'"},
					fg:   [3]string{"61116", "41214", "61116"},
					bg:   [3]string{" 111 ", "01110", " 111 "},
					attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
				},
			},
		},
	}

	for i := range specs {
		eyeRenderTable[i] = buildEyeType(&specs[i])
	}
}

// === Renderer ===

// eyeCellRenderer callback for color mode strategy (256 vs TrueColor)
type eyeCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, cell *eyeCell, tr *eyeTypeRender, flashColor terminal.RGB, hasFlash bool)

// EyeRenderer draws eye composite entities
type EyeRenderer struct {
	gameCtx    *engine.GameContext
	renderCell eyeCellRenderer
}

// NewEyeRenderer creates an eye renderer with color mode strategy
func NewEyeRenderer(gameCtx *engine.GameContext) *EyeRenderer {
	r := &EyeRenderer{gameCtx: gameCtx}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

// Render draws all active eye entities
func (r *EyeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headerEntities := r.gameCtx.World.Components.Eye.GetAllEntities()
	if len(headerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, headerEntity := range headerEntities {
		eyeComp, ok := r.gameCtx.World.Components.Eye.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}

		typeIdx := int(eyeComp.Type)
		if typeIdx >= parameter.EyeTypeCount {
			continue
		}

		tr := &eyeRenderTable[typeIdx]
		frameIdx := eyeComp.FrameIndex % tr.FrameCount
		frame := &tr.Frames[frameIdx]

		var flashColor terminal.RGB
		hasFlash := combatComp.RemainingHitFlash > 0
		if hasFlash {
			flashColor = calculateEyeFlashColor(combatComp.RemainingHitFlash)
		}

		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}

			row := member.OffsetY + parameter.EyeHeaderOffsetY
			col := member.OffsetX + parameter.EyeHeaderOffsetX
			if row < 0 || row >= parameter.EyeHeight || col < 0 || col >= parameter.EyeWidth {
				continue
			}

			r.renderCell(buf, screenX, screenY, &frame[row][col], tr, flashColor, hasFlash)
		}
	}
}

// cellTrueColor renders with per-cell palette colors and attributes
func (r *EyeRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, cell *eyeCell, tr *eyeTypeRender, flashColor terminal.RGB, hasFlash bool) {
	hasCh := cell.Ch != 0 && cell.FgIdx >= 0
	hasBg := cell.BgIdx >= 0

	if !hasCh && !hasBg {
		return
	}

	if hasBg {
		buf.SetBgOnly(screenX, screenY, tr.BgPalette[cell.BgIdx])
	}

	if hasCh {
		fg := tr.FgPalette[cell.FgIdx]
		if hasFlash {
			fg = flashColor
		}
		buf.SetFgOnly(screenX, screenY, cell.Ch, fg, cell.Attr)
	}
}

// cell256 renders with simplified per-type 256-color palette
func (r *EyeRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, cell *eyeCell, tr *eyeTypeRender, flashColor terminal.RGB, hasFlash bool) {
	hasCh := cell.Ch != 0
	hasBg := cell.BgIdx >= 0

	if !hasCh && !hasBg {
		return
	}

	if hasBg {
		buf.SetBg256(screenX, screenY, tr.Bg256)
	}

	if hasCh {
		fgIdx := tr.Fg256
		if hasFlash {
			fgIdx = eye256FlashFg
		}
		buf.SetFgOnly(screenX, screenY, cell.Ch, terminal.RGB{R: fgIdx}, terminal.AttrFg256|cell.Attr)
	}
}

// calculateEyeFlashColor returns pulsed yellow flash color matching combat convention
func calculateEyeFlashColor(remaining time.Duration) terminal.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	return render.Scale(visual.RgbCombatHitFlash, intensity)
}