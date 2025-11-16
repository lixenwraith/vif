package modes

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/systems"
)

// InputHandler processes user input events
type InputHandler struct {
	ctx         *engine.GameContext
	scoreSystem *systems.ScoreSystem
}

// NewInputHandler creates a new input handler
func NewInputHandler(ctx *engine.GameContext, scoreSystem *systems.ScoreSystem) *InputHandler {
	return &InputHandler{
		ctx:         ctx,
		scoreSystem: scoreSystem,
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
	if ev.Key() == tcell.KeyRune {
		// Delegate character typing to score system
		h.scoreSystem.HandleCharacterTyping(h.ctx.World, h.ctx.CursorX, h.ctx.CursorY, ev.Rune())
	}
	return true
}

// handleSearchMode handles input in search mode
func (h *InputHandler) handleSearchMode(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter {
		// Execute search
		if h.ctx.SearchText != "" {
			if PerformSearch(h.ctx, h.ctx.SearchText, true) {
				h.ctx.LastSearchText = h.ctx.SearchText
			}
		}
		h.ctx.Mode = engine.ModeNormal
		h.ctx.SearchText = ""
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

		// Handle waiting for 'f' character
		if h.ctx.WaitingForF {
			ExecuteFindChar(h.ctx, char)
			h.ctx.WaitingForF = false
			return true
		}

		// Handle delete operator
		if h.ctx.DeleteOperator {
			ExecuteDeleteMotion(h.ctx, char, h.ctx.MotionCount)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			return true
		}

		// Handle numbers for count
		if char >= '0' && char <= '9' {
			// Special case: '0' is a motion (line start) when not following a number
			if char == '0' && h.ctx.MotionCount == 0 {
				ExecuteMotion(h.ctx, char, 1)
				h.ctx.MotionCommand = ""
				return true
			}
			h.ctx.MotionCount = h.ctx.MotionCount*10 + int(char-'0')
			return true
		}

		// Get count (default to 1 if not specified)
		count := h.ctx.MotionCount
		if count == 0 {
			count = 1
		}

		// Enter insert mode
		if char == 'i' {
			h.ctx.Mode = engine.ModeInsert
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			return true
		}

		// Enter search mode
		if char == '/' {
			h.ctx.Mode = engine.ModeSearch
			h.ctx.SearchText = ""
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			return true
		}

		// Repeat search
		if char == 'n' {
			RepeatSearch(h.ctx, true)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			return true
		}

		if char == 'N' {
			RepeatSearch(h.ctx, false)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			return true
		}

		// D - delete to end of line (equivalent to d$)
		if char == 'D' {
			ExecuteDeleteMotion(h.ctx, '$', count)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.CommandPrefix = 0
			return true
		}

		// Delete operator
		if char == 'd' {
			if h.ctx.CommandPrefix == 'd' {
				// dd - delete line
				ExecuteDeleteMotion(h.ctx, 'd', count)
				h.ctx.MotionCount = 0
				h.ctx.MotionCommand = ""
				h.ctx.CommandPrefix = 0
			} else {
				h.ctx.DeleteOperator = true
				h.ctx.CommandPrefix = 'd'
			}
			return true
		}

		// Handle 'g' prefix commands
		if h.ctx.CommandPrefix == 'g' {
			// Second character after 'g'
			if char == 'g' {
				// gg - goto top (same column)
				h.ctx.CursorY = 0
				h.ctx.CommandPrefix = 0
				h.ctx.MotionCount = 0
				return true
			} else if char == 'o' {
				// go - goto top left (first row, first column)
				h.ctx.CursorY = 0
				h.ctx.CursorX = 0
				h.ctx.CommandPrefix = 0
				h.ctx.MotionCount = 0
				return true
			} else {
				// Unknown 'g' command, reset
				h.ctx.CommandPrefix = 0
				h.ctx.MotionCount = 0
				return true
			}
		}

		// Start 'g' prefix
		if char == 'g' {
			h.ctx.CommandPrefix = 'g'
			return true
		}

		// Toggle ping
		if char == '\r' || char == '\n' {
			h.ctx.PingActive = !h.ctx.PingActive
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			return true
		}

		// Execute motion commands
		ExecuteMotion(h.ctx, char, count)
		h.ctx.MotionCount = 0
		h.ctx.MotionCommand = ""
		h.ctx.CommandPrefix = 0
	}
	return true
}
