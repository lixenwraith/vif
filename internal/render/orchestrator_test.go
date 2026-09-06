package render

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

// captureTerminal is the smallest host the pipeline will accept: it keeps the
// cells the orchestrator flushed so a test can read the finished frame.
type captureTerminal struct {
	cells         []terminal.Cell
	width, height int
}

func (t *captureTerminal) Init() error                             { return nil }
func (t *captureTerminal) Fini()                                   {}
func (t *captureTerminal) Size() (int, int)                        { return t.width, t.height }
func (t *captureTerminal) ResizeChan() <-chan terminal.ResizeEvent { return nil }
func (t *captureTerminal) ColorMode() terminal.ColorMode           { return terminal.ColorModeTrueColor }
func (t *captureTerminal) Clear(color.RGB)                         {}
func (t *captureTerminal) SetCursorVisible(bool)                   {}
func (t *captureTerminal) MoveCursor(int, int)                     {}
func (t *captureTerminal) Sync()                                   {}
func (t *captureTerminal) PollEvent() terminal.Event               { return terminal.Event{} }
func (t *captureTerminal) PostEvent(terminal.Event)                {}
func (t *captureTerminal) SetMouseMode(terminal.MouseMode) error   { return nil }

func (t *captureTerminal) Flush(cells []terminal.Cell, width, height int) {
	t.cells = append(t.cells[:0], cells...)
	t.width, t.height = width, height
}

func (t *captureTerminal) at(x, y int) terminal.Cell { return t.cells[y*t.width+x] }

// scribbler draws one colour over every cell of the buffer it is handed. It is
// the worst case the clip exists for: a renderer that iterates the viewport, or
// a shape whose bounding box runs past the map, with no bound of its own.
type scribbler struct{ c color.RGB }

func (s *scribbler) Render(_ RenderContext, buf *RenderBuffer) {
	buf.SetWriteMask(visual.MaskTransient)
	// Background only, so the assertions read the composed colour rather than the
	// occlusion dimming a rune would trigger in finalize.
	b := buf.Bounds()
	for y := b.Y0; y < b.Y1; y++ {
		for x := b.X0; x < b.X1; x++ {
			buf.SetBgOnly(x, y, s.c)
		}
	}
}

// TestScreenSpaceLayersStayUnclipped keeps the clip from reaching the layers
// that address the screen on purpose: the status bar, the gutters, the overlay
// panels and the post-processing passes all live outside the map.
func TestScreenSpaceLayersStayUnclipped(t *testing.T) {
	t.Parallel()

	const screenW, screenH = 30, 16
	term := &captureTerminal{width: screenW, height: screenH}
	o := NewRenderOrchestrator(term, screenW, screenH)

	ui := color.RGB{B: 200}
	o.Register(&scribbler{c: ui}, PriorityOverlay)
	o.RenderFrame(centredContext(), engine.NewWorld())

	for y := range screenH {
		for x := range screenW {
			if got := term.at(x, y).Bg; got != ui {
				t.Fatalf("cell (%d,%d) = %v, want the unclipped UI colour %v", x, y, got, ui)
			}
		}
	}
}

// TestTheMarginIsPresentedAsOutOfPlay covers the same frame without a UI layer
// over it: the simulation layer stops at the map edge and the margin the map is
// centred in is filled rather than left looking like empty playable space.
func TestTheMarginIsPresentedAsOutOfPlay(t *testing.T) {
	t.Parallel()

	const screenW, screenH = 30, 16
	term := &captureTerminal{width: screenW, height: screenH}
	o := NewRenderOrchestrator(term, screenW, screenH)

	drawn := color.RGB{R: 200}
	o.Register(&scribbler{c: drawn}, PriorityBullet)

	ctx := centredContext()
	o.RenderFrame(ctx, engine.NewWorld())

	pf, area := ctx.PlayfieldRect(), ctx.GameAreaRect()
	for y := range screenH {
		for x := range screenW {
			got := term.at(x, y).Bg
			var want color.RGB
			switch {
			case pf.Contains(x, y):
				want = drawn
			case area.Contains(x, y):
				want = visual.RgbVoid
			default:
				want = visual.RgbBackground
			}
			if got != want {
				t.Fatalf("cell (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// TestACroppedMapIsUnchanged is the safety property: every run whose map matches
// its viewport — crop_on_resize, or any camera-cropped map — composes exactly as
// it did before the clip existed.
func TestACroppedMapIsUnchanged(t *testing.T) {
	t.Parallel()

	const screenW, screenH = 30, 16
	term := &captureTerminal{width: screenW, height: screenH}
	o := NewRenderOrchestrator(term, screenW, screenH)

	drawn := color.RGB{R: 200}
	o.Register(&scribbler{c: drawn}, PriorityBullet)

	ctx := croppedContext()
	o.RenderFrame(ctx, engine.NewWorld())

	area := ctx.GameAreaRect()
	for y := range screenH {
		for x := range screenW {
			got := term.at(x, y).Bg
			want := visual.RgbBackground
			if area.Contains(x, y) {
				want = drawn
			}
			if got != want {
				t.Fatalf("cell (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}
