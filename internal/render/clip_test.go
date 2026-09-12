package render

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

// centredContext is the geometry the defect appears in: a terminal larger than
// the map, so the map is centred and leaves a margin no simulation cell reaches.
func centredContext() RenderContext {
	return RenderContext{
		GameXOffset: 3, GameYOffset: 1,
		ViewportWidth: 20, ViewportHeight: 12,
		MapOffsetX: 5, MapOffsetY: 3,
		MapWidth: 10, MapHeight: 6,
	}
}

// croppedContext is the other half of the contract: a map larger than the
// viewport, where the camera crops and the playfield is the whole game area.
func croppedContext() RenderContext {
	return RenderContext{
		GameXOffset: 3, GameYOffset: 1,
		ViewportWidth: 20, ViewportHeight: 12,
		CameraX: 6, CameraY: 4,
		MapWidth: 40, MapHeight: 30,
	}
}

func TestPlayfieldRectIsTheMapNotTheViewport(t *testing.T) {
	t.Parallel()

	centred := centredContext()
	if got, want := centred.PlayfieldRect(), (Rect{X0: 8, Y0: 4, X1: 18, Y1: 10}); got != want {
		t.Fatalf("centred playfield = %+v, want %+v", got, want)
	}
	if got, want := centred.GameAreaRect(), (Rect{X0: 3, Y0: 1, X1: 23, Y1: 13}); got != want {
		t.Fatalf("centred game area = %+v, want %+v", got, want)
	}

	// A cropped map fills the game area, which is what keeps the clip a no-op on
	// the path every single-player run with crop_on_resize takes.
	cropped := croppedContext()
	if got, want := cropped.PlayfieldRect(), cropped.GameAreaRect(); got != want {
		t.Fatalf("cropped playfield = %+v, want the whole game area %+v", got, want)
	}
}

func TestOffMapCoordinatesAreNotVisible(t *testing.T) {
	t.Parallel()
	ctx := centredContext()

	// Inside the map: projects and is visible.
	if x, y, visible := ctx.MapToScreen(0, 0); !visible || x != 8 || y != 4 {
		t.Fatalf("map origin projected to (%d,%d) visible=%v, want (8,4) visible", x, y, visible)
	}

	// Outside the map but inside the viewport: this is the projection that used
	// to report itself visible and let effects draw into the margin.
	for _, c := range []struct{ x, y int }{{-1, 0}, {0, -1}, {ctx.MapWidth, 0}, {0, ctx.MapHeight}} {
		if _, _, visible := ctx.MapToScreen(c.x, c.y); visible {
			t.Fatalf("off-map cell (%d,%d) reported itself visible", c.x, c.y)
		}
		if ctx.IsInViewport(c.x, c.y) {
			t.Fatalf("off-map cell (%d,%d) reported itself in the viewport", c.x, c.y)
		}
	}

	// The projection is still returned so a renderer can anchor a shape on an
	// off-map centre and let the clip trim it.
	if vx, vy, _ := ctx.MapToViewport(-2, -1); vx != 3 || vy != 2 {
		t.Fatalf("off-map anchor projected to (%d,%d), want (3,2)", vx, vy)
	}
}

func TestClipRejectsEveryWritePathOutsideThePlayfield(t *testing.T) {
	t.Parallel()
	ctx := centredContext()
	pf := ctx.PlayfieldRect()

	writes := map[string]func(b *RenderBuffer, x, y int){
		"Set": func(b *RenderBuffer, x, y int) {
			b.Set(x, y, 'X', color.RGB{R: 255}, color.RGB{G: 255}, BlendReplace, 1.0, terminal.AttrNone)
		},
		"SetFgOnly": func(b *RenderBuffer, x, y int) {
			b.SetFgOnly(x, y, 'X', color.RGB{R: 255}, terminal.AttrNone)
		},
		"SetBgOnly":   func(b *RenderBuffer, x, y int) { b.SetBgOnly(x, y, color.RGB{G: 255}) },
		"SetBgScreen": func(b *RenderBuffer, x, y int) { b.SetBgScreen(x, y, color.RGB{G: 255}, visual.RgbBackground, 0.5) },
		"SetWithBg": func(b *RenderBuffer, x, y int) {
			b.SetWithBg(x, y, 'X', color.RGB{R: 255}, color.RGB{G: 255})
		},
		"SetBg256": func(b *RenderBuffer, x, y int) { b.SetBg256(x, y, 42) },
	}

	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			buf := NewRenderBuffer(terminal.ColorModeTrueColor, 30, 16)
			buf.SetClip(pf)
			// Scribble the whole buffer, the way a renderer iterating the viewport
			// rather than the map effectively does.
			for y := range 16 {
				for x := range 30 {
					write(buf, x, y)
				}
			}
			var zero terminal.Cell
			for y := range 16 {
				for x := range 30 {
					drawn := buf.CellAt(x, y) != zero
					if drawn != pf.Contains(x, y) {
						t.Fatalf("cell (%d,%d) drawn=%v, playfield=%v", x, y, drawn, pf.Contains(x, y))
					}
				}
			}
		})
	}
}

func TestBgScreenUsesBaseOnlyForUntouchedCells(t *testing.T) {
	t.Parallel()
	buf := NewRenderBuffer(terminal.ColorModeTrueColor, 2, 1)
	base := color.RGB{R: 20, G: 30, B: 40}
	underlay := color.RGB{R: 80, G: 70, B: 60}
	source := color.RGB{R: 200, G: 150, B: 100}

	buf.SetBgScreen(0, 0, source, base, 0.3)
	buf.SetBgOnly(1, 0, underlay)
	buf.SetBgScreen(1, 0, source, base, 0.3)

	if got, want := buf.CellAt(0, 0).Bg, color.Screen(base, source, 0.3); got != want {
		t.Fatalf("untouched blend = %v, want base blend %v", got, want)
	}
	if got, want := buf.CellAt(1, 0).Bg, color.Screen(underlay, source, 0.3); got != want {
		t.Fatalf("layered blend = %v, want underlay blend %v", got, want)
	}
}

func TestClearReopensTheClip(t *testing.T) {
	t.Parallel()
	buf := NewRenderBuffer(terminal.ColorModeTrueColor, 8, 4)
	buf.SetClip(Rect{X0: 1, Y0: 1, X1: 2, Y1: 2})
	buf.Clear()
	if got, want := buf.Clip(), buf.Bounds(); got != want {
		t.Fatalf("clip after clear = %+v, want the whole buffer %+v", got, want)
	}
}

func TestClipConstrainsPostProcessing(t *testing.T) {
	t.Parallel()
	buf := NewRenderBuffer(terminal.ColorMode256, 4, 1)
	buf.SetWriteMask(visual.MaskGlyph)
	for x := range 4 {
		buf.SetFgOnly(x, 0, 'X', color.RGB{R: 200, G: 200, B: 200}, terminal.AttrNone)
	}

	buf.SetClip(Rect{X0: 1, Y0: 0, X1: 3, Y1: 1})
	buf.MutateDim(0.5, visual.MaskGlyph)

	for x := range 4 {
		dimmed := buf.CellAt(x, 0).Fg.R == 100
		if want := x >= 1 && x < 3; dimmed != want {
			t.Fatalf("cell %d dimmed=%v, want %v", x, dimmed, want)
		}
	}
}

func TestVoidFillPaintsOnlyTheUndrawnMargin(t *testing.T) {
	t.Parallel()
	ctx := centredContext()
	pf, area := ctx.PlayfieldRect(), ctx.GameAreaRect()

	buf := NewRenderBuffer(terminal.ColorModeTrueColor, 30, 16)
	buf.SetVoidRegion(area, pf, visual.RgbVoid)

	// A UI layer reaching into the margin keeps its own colours: the fill is for
	// cells nothing drew, not a mask over the ones something did.
	ui := color.RGB{R: 10, G: 20, B: 30}
	buf.SetWithBg(area.X0, area.Y0, 'U', color.RGB{R: 255}, ui)

	buf.finalize()

	for y := range 16 {
		for x := range 30 {
			got := buf.CellAt(x, y).Bg
			switch {
			case x == area.X0 && y == area.Y0:
				if got != ui {
					t.Fatalf("drawn margin cell (%d,%d) = %v, want the colour it drew %v", x, y, got, ui)
				}
			case area.Contains(x, y) && !pf.Contains(x, y):
				if got != visual.RgbVoid {
					t.Fatalf("margin cell (%d,%d) = %v, want void %v", x, y, got, visual.RgbVoid)
				}
			default:
				if got != visual.RgbBackground {
					t.Fatalf("cell (%d,%d) = %v, want theme background %v", x, y, got, visual.RgbBackground)
				}
			}
		}
	}
}

func TestVoidRegionIsInertWhenTheMapFillsTheGameArea(t *testing.T) {
	t.Parallel()
	ctx := croppedContext()
	buf := NewRenderBuffer(terminal.ColorModeTrueColor, 30, 16)
	buf.SetVoidRegion(ctx.GameAreaRect(), ctx.PlayfieldRect(), visual.RgbVoid)
	buf.finalize()

	for y := range 16 {
		for x := range 30 {
			if got := buf.CellAt(x, y).Bg; got != visual.RgbBackground {
				t.Fatalf("cell (%d,%d) = %v, want theme background %v", x, y, got, visual.RgbBackground)
			}
		}
	}
}

// TestEveryLayerDeclaresItsClip fails when a priority is added without deciding
// which side of the playfield boundary it belongs on. The expectation is spelled
// out per layer rather than derived, so the decision is made once, here.
func TestEveryLayerDeclaresItsClip(t *testing.T) {
	t.Parallel()
	screenSpace := map[RenderPriority]bool{
		PriorityGrayout: true, PriorityStrobe: true, PriorityDim: true,
		PriorityHeat: true, PriorityIndicator: true, PriorityStatusBar: true,
		PriorityFlowField: true, PriorityPinnedState: true,
		PriorityOverlay: true, PriorityDebug: true,
	}
	playfield := map[RenderPriority]bool{
		PriorityBackground: true, PriorityGrid: true, PriorityPing: true,
		PriorityWall: true, PriorityChargeLine: true,
		PrioritySigil: true, PriorityGlyph: true, PriorityGold: true,
		PriorityNugget: true, PriorityHealthBar: true,
		PriorityPylon: true, PriorityTower: true, PriorityStorm: true,
		PriorityEye: true, PrioritySnake: true, PriorityDrain: true,
		PriorityQuasar: true, PrioritySwarm: true, PriorityCleaner: true,
		PriorityMaterialize: true, PriorityTeleportLine: true,
		PriorityShield: true, PriorityEmber: true, PriorityOrb: true,
		PriorityLightning: true, PriorityMissile: true, PriorityPulse: true,
		PriorityBullet: true, PriorityFlash: true, PriorityFadeout: true,
		PriorityExplosion: true, PrioritySpirit: true, PrioritySplash: true,
		PriorityMarker: true, PriorityPeerCursor: true, PriorityCursor: true,
	}

	for p := PriorityBackground; p <= PriorityDebug; p++ {
		inScreen, inPlayfield := screenSpace[p], playfield[p]
		if inScreen == inPlayfield {
			t.Fatalf("priority %d is in neither list or both; declare which space it draws in", p)
		}
		if got := p.ClipsToPlayfield(); got != inPlayfield {
			t.Fatalf("priority %d ClipsToPlayfield() = %v, want %v", p, got, inPlayfield)
		}
	}
}
