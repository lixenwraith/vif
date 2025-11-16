package modes

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// InputHandler processes user input events
type InputHandler struct {
	ctx *engine.GameContext
}

// NewInputHandler creates a new input handler
func NewInputHandler(ctx *engine.GameContext) *InputHandler {
	return &InputHandler{
		ctx: ctx,
	}
}

// HandleEvent processes a tcell event and returns false if the game should exit
func (h *InputHandler) HandleEvent(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		return h.handleKeyEvent(ev)
	case *tcell.EventResize:
		h.ctx.HandleResize()
		return true
	}
	return true
}

// handleKeyEvent processes keyboard events
func (h *InputHandler) handleKeyEvent(ev *tcell.EventKey) bool {
	// Handle exit keys
	if ev.Key() == tcell.KeyCtrlQ || ev.Key() == tcell.KeyCtrlC {
		return false
	}

	// Handle Escape
	if ev.Key() == tcell.KeyEscape {
		if h.ctx.IsSearchMode() {
			h.ctx.Mode = engine.ModeNormal
			h.ctx.SearchText = ""
		} else if h.ctx.IsInsertMode() {
			h.ctx.Mode = engine.ModeNormal
		}
		return true
	}

	// Mode-specific handling
	if h.ctx.IsInsertMode() {
		return h.handleInsertMode(ev)
	} else if h.ctx.IsSearchMode() {
		return h.handleSearchMode(ev)
	} else {
		return h.handleNormalMode(ev)
	}
}

// handleInsertMode handles input in insert mode
func (h *InputHandler) handleInsertMode(ev *tcell.EventKey) bool {
	// Simplified insert mode - just handle mode switching
	// Full implementation would handle character typing
	return true
}

// handleSearchMode handles input in search mode
func (h *InputHandler) handleSearchMode(ev *tcell.EventKey) bool {
	// Simplified search mode
	if ev.Key() == tcell.KeyEnter {
		h.ctx.Mode = engine.ModeNormal
		return true
	}
	if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
		if len(h.ctx.SearchText) > 0 {
			h.ctx.SearchText = h.ctx.SearchText[:len(h.ctx.SearchText)-1]
		}
		return true
	}
	if ev.Key() == tcell.KeyRune {
		h.ctx.SearchText += string(ev.Rune())
	}
	return true
}

// handleNormalMode handles input in normal mode
func (h *InputHandler) handleNormalMode(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyRune {
		char := ev.Rune()

		// Enter insert mode
		if char == 'i' {
			h.ctx.Mode = engine.ModeInsert
			return true
		}

		// Enter search mode
		if char == '/' {
			h.ctx.Mode = engine.ModeSearch
			h.ctx.SearchText = ""
			return true
		}

		// Toggle ping
		if char == '\r' || char == '\n' {
			h.ctx.PingActive = !h.ctx.PingActive
			return true
		}

		// Basic movements (simplified)
		switch char {
		case 'h':
			h.moveCursor(-1, 0)
		case 'j':
			h.moveCursor(0, 1)
		case 'k':
			h.moveCursor(0, -1)
		case 'l':
			h.moveCursor(1, 0)
		}
	}
	return true
}

// moveCursor moves the cursor with bounds checking
func (h *InputHandler) moveCursor(dx, dy int) {
	h.ctx.CursorX += dx
	h.ctx.CursorY += dy

	// Clamp to game area
	if h.ctx.CursorX < 0 {
		h.ctx.CursorX = 0
	}
	if h.ctx.CursorX >= h.ctx.GameWidth {
		h.ctx.CursorX = h.ctx.GameWidth - 1
	}
	if h.ctx.CursorY < 0 {
		h.ctx.CursorY = 0
	}
	if h.ctx.CursorY >= h.ctx.GameHeight {
		h.ctx.CursorY = h.ctx.GameHeight - 1
	}
}
