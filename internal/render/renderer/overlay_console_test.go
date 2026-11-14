package renderer

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// consoleShot draws the console of ctx and reads back its rows as text
type consoleShot struct {
	buf *render.RenderBuffer
	g   engine.OverlayGeometry
}

func shootConsole(t *testing.T, r *OverlayRenderer, ctx *engine.GameContext, w, h int) consoleShot {
	t.Helper()
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, w, h)
	r.Render(render.RenderContext{}, buf)
	return consoleShot{buf: buf, g: ctx.OverlayGeometry()}
}

// row is the text of band row y, from the content's first column
func (s consoleShot) row(y int) string {
	var b strings.Builder
	for x := s.g.X + s.g.ContentX; x < s.g.X+s.g.ContentX+s.g.ContentW; x++ {
		b.WriteRune(s.buf.CellAt(x, s.g.Y+y).Rune)
	}
	return strings.TrimRight(b.String(), " ")
}

// content is the text of the console's message row i, counted from its top
func (s consoleShot) content(i int) string { return s.row(s.g.ContentY + i) }

// TestTheConsoleListsMessagesNewestAtItsFoot: the band's edge names it, the newest
// message is its last row behind its stamp, in its kind's colour, a long one wraps
// under its text, a repeat ends with its count, and the rows it lays out are
// published for the router to scroll by
func TestTheConsoleListsMessagesNewestAtItsFoot(t *testing.T) {
	ctx := engine.NewGameContext(engine.NewWorld(), 60, 20)
	long := strings.TrimSpace(strings.Repeat("word ", 20))
	ctx.SetStatusMessage(long, parameter.StatusNotice)
	ctx.SetStatusMessage(":speed 2", parameter.StatusInput)
	ctx.SetStatusMessage("Speed 2x", parameter.StatusNotice)
	ctx.SetStatusMessage("Speed 2x", parameter.StatusNotice)
	ctx.SetOverlayContent(&core.OverlayContent{Title: parameter.ConsoleTitle, Layout: core.OverlayLayoutConsole})
	ctx.SetOverlayScroll(parameter.ConsoleFollow)
	r := NewOverlayRenderer(ctx)
	s := shootConsole(t, r, ctx, 60, 20)
	g := s.g
	stampW := len(parameter.ConsoleStamp) + 1

	if edge := s.row(g.HintY); !strings.Contains(edge, parameter.ConsoleTitle) {
		t.Fatalf("the edge reads %q", edge)
	}
	foot := s.content(g.ContentH - 1)
	if _, err := time.Parse(parameter.ConsoleStamp, foot[:stampW-1]); err != nil || foot[stampW:] != "Speed 2x (x2)" {
		t.Fatalf("the foot reads %q, want a stamp then the repeat with its count", foot)
	}
	input := s.content(g.ContentH - 2)
	if cell := s.buf.CellAt(g.X+g.ContentX+stampW, g.Y+g.ContentY+g.ContentH-2); input[stampW:] != ":speed 2" ||
		cell.Fg != visual.RgbConsoleKind[parameter.StatusInput] {
		t.Fatalf("the input row reads %q in %v", input, cell.Fg)
	}
	rows, wrapped := ctx.GetOverlayContentH(), len(tui.WrapText(long, g.ContentW-stampW))
	first, second := s.content(g.ContentH-rows), s.content(g.ContentH-rows+1)
	if wrapped < 2 || rows != wrapped+2 || !strings.HasPrefix(long, first[stampW:]) ||
		strings.TrimSpace(second[:stampW]) != "" || !strings.Contains(long, strings.TrimSpace(second)) {
		t.Fatalf("%d rows; the long message reads %q then %q, want it wrapped under its text", rows, first, second)
	}
	if top := s.content(g.ContentH - rows - 1); top != "" {
		t.Fatalf("the row above the oldest message reads %q, want the rows at the foot", top)
	}
}

// TestTheConsoleViewHoldsWhereItWasScrolled: a view off the foot keeps the rows it
// shows as messages come in; a view at the foot follows them
func TestTheConsoleViewHoldsWhereItWasScrolled(t *testing.T) {
	ctx := engine.NewGameContext(engine.NewWorld(), 60, 20)
	ctx.SetOverlayContent(&core.OverlayContent{Title: parameter.ConsoleTitle, Layout: core.OverlayLayoutConsole})
	g := ctx.OverlayGeometry()
	for i := range 2 * g.ContentH {
		ctx.SetStatusMessage(strings.Repeat("x", i+1), parameter.StatusNotice)
	}
	r := NewOverlayRenderer(ctx)
	stampW := len(parameter.ConsoleStamp) + 1

	ctx.SetOverlayScroll(0)
	shootConsole(t, r, ctx, 60, 20)
	ctx.SetStatusMessage("newest", parameter.StatusAlert)
	if top := shootConsole(t, r, ctx, 60, 20).content(0); top[stampW:] != "x" {
		t.Fatalf("a view at the top moved to %q as a message came in", top)
	}

	ctx.SetOverlayScroll(parameter.ConsoleFollow)
	ctx.SetStatusMessage("newer still", parameter.StatusAlert)
	if foot := shootConsole(t, r, ctx, 60, 20).content(g.ContentH - 1); foot[stampW:] != "newer still" {
		t.Fatalf("a view at the foot shows %q, not the message that came in", foot)
	}
}
