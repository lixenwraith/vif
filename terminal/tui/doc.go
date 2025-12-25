// FILE: terminal/tui/doc.go
// Package tui provides immediate-mode TUI primitives for the terminal package.
//
// Core abstraction is Region, representing a rectangular area within a cell buffer.
// All drawing operations are relative to region bounds with automatic clipping.
//
// Design principles:
//   - Immediate mode: no retained widget state, app owns render loop
//   - Zero allocation in hot paths: Region is a small value type
//   - Composable: regions nest via Sub(), layout helpers split regions
//   - Responsive: BreakpointH/V enable adaptive layouts
//
// Usage pattern:
//
//	cells := make([]terminal.Cell, w*h)
//	root := tui.NewRegion(cells, w, 0, 0, w, h)
//	root.Fill(bgColor)
//
//	// Responsive layout
//	switch tui.BreakpointH(w, 120, 80) {
//	case 0: // wide
//	    panes := tui.SplitH(root, 0.5, 0.5)
//	case 1: // medium
//	    panes := tui.SplitV(root, 0.5, 0.5)
//	}
//
//	// Card with content
//	content := panes[0].Card("TITLE", tui.LineDouble, borderColor)
//	content.Text(0, 0, "Hello", fg, bg, 0)
//
//	term.Flush(cells, w, h)
package tui