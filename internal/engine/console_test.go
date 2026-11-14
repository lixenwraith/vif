package engine

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestTheConsoleKeepsTheNewestAndFoldsARepeat: every message posted is listed, the
// ones the status bar refuses too; past parameter.ConsoleHistory the oldest goes, a
// message posted again in a row is counted on its line, and each change moves the
// count a reader copies by.
func TestTheConsoleKeepsTheNewestAndFoldsARepeat(t *testing.T) {
	ctx, _ := newMessageContext(t)
	ctx.SetStatusMessage("Host connection lost", parameter.StatusAlert)
	ctx.SetStatusMessage("Auto: on", parameter.StatusNotice) // refused by the bar, inside the alert's minimum
	lines, _ := ctx.ConsoleLines(nil)
	if len(lines) != 2 || lines[1].Text != "Auto: on" || lines[1].Kind != parameter.StatusNotice {
		t.Fatalf("the console lists %+v, want the alert and the notice the bar refused", lines)
	}

	for i := range parameter.ConsoleHistory {
		ctx.SetStatusMessage(fmt.Sprint(i), parameter.StatusNotice)
	}
	seq := ctx.ConsoleSeq()
	ctx.SetStatusMessage(fmt.Sprint(parameter.ConsoleHistory-1), parameter.StatusNotice)
	lines, at := ctx.ConsoleLines(lines[:0])
	last := lines[len(lines)-1]
	switch {
	case len(lines) != parameter.ConsoleHistory || lines[0].Text != "0":
		t.Fatalf("%d lines from %q, want the newest %d from \"0\"", len(lines), lines[0].Text, parameter.ConsoleHistory)
	case last.Count != 2:
		t.Fatalf("a repeat counts %d on its line, want 2", last.Count)
	case at != seq+1 || ctx.ConsoleSeq() != at:
		t.Fatalf("the repeat moved the count from %d to %d", seq, at)
	}

	ctx.SetStatusMessage(last.Text, parameter.StatusError) // the same words as another kind
	if lines, _ = ctx.ConsoleLines(lines[:0]); lines[len(lines)-1].Count != 1 || lines[0].Text != "1" {
		t.Fatal("the same text of another kind folded into the line before it")
	}
}

// TestAdoptedLinesGoBeforeTheRunsOwn: the lines of the run a new one replaces come
// first, as they were, ahead of what the new run posted while it started; clearing
// leaves none.
func TestAdoptedLinesGoBeforeTheRunsOwn(t *testing.T) {
	ctx, _ := newMessageContext(t)
	ctx.SetStatusMessage("Joined", parameter.StatusNotice)
	ctx.AdoptConsole([]ConsoleLine{
		{Text: ":join host:7777", Kind: parameter.StatusInput, At: 1, Count: 1},
		{Text: "Link recovered", Kind: parameter.StatusAlert, At: 2, Count: 3},
	})
	lines, _ := ctx.ConsoleLines(nil)
	if len(lines) != 3 || lines[0].At != 1 || lines[1].Count != 3 || lines[2].Text != "Joined" {
		t.Fatalf("after adopting, the console lists %+v", lines)
	}
	seq := ctx.ConsoleSeq()
	ctx.ClearConsole()
	if lines, _ = ctx.ConsoleLines(lines[:0]); len(lines) != 0 || ctx.ConsoleSeq() == seq {
		t.Fatalf("clearing left %d lines, count %d from %d", len(lines), ctx.ConsoleSeq(), seq)
	}
}

// TestTheConsoleIsABandOverTheStatusBar: the console spans the screen right above
// the status bar, its edge row toward the map and its scrollbar in the last column,
// and the overlay's geometry is the band's while the console is shown
func TestTheConsoleIsABandOverTheStatusBar(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {200, 60}, {parameter.ConsoleMinWidth, 6}} {
		g := ComputeConsoleGeometry(size[0], size[1])
		rows := size[1] - parameter.StatusRows
		switch {
		case !g.Valid || g.X != 0 || g.W != size[0] || g.Y+g.H != rows:
			t.Fatalf("size %v: band %+v, want the full width ending at row %d", size, g, rows)
		case g.HintY != 0 || g.ContentY != 1 || g.ContentH != g.H-1 || g.ContentH < parameter.ConsoleMinRows:
			t.Fatalf("size %v: band %+v, want its edge first and at least %d rows", size, g, parameter.ConsoleMinRows)
		case g.ScrollW != 1 || g.ScrollX != g.W-1 || g.ContentX+g.ContentW >= g.ScrollX:
			t.Fatalf("size %v: band %+v, want the scrollbar alone in the last column", size, g)
		}
	}
	if ComputeConsoleGeometry(parameter.ConsoleMinWidth-1, 24).Valid || ComputeConsoleGeometry(80, parameter.StatusRows+1).Valid {
		t.Fatal("a screen too narrow or too short for the console placed one")
	}

	ctx, _ := newMessageContext(t)
	window := ctx.OverlayGeometry()
	ctx.SetOverlayContent(&core.OverlayContent{Layout: core.OverlayLayoutConsole})
	if g := ctx.OverlayGeometry(); g != ComputeConsoleGeometry(80, 24) || !ctx.IsConsoleShown() {
		t.Fatalf("the console shown has the geometry %+v", g)
	}
	ctx.SetOverlayContent(&core.OverlayContent{Layout: core.OverlayLayoutDoc})
	if g := ctx.OverlayGeometry(); g != window || ctx.IsConsoleShown() {
		t.Fatalf("help has the geometry %+v, want the window %+v", g, window)
	}
}
