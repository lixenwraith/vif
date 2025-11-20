package modes

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/systems"
)

// InputHandler processes user input events
type InputHandler struct {
	ctx           *engine.GameContext
	scoreSystem   *systems.ScoreSystem
	nuggetSystem  *systems.NuggetSystem
}

// NewInputHandler creates a new input handler
func NewInputHandler(ctx *engine.GameContext, scoreSystem *systems.ScoreSystem) *InputHandler {
	return &InputHandler{
		ctx:          ctx,
		scoreSystem:  scoreSystem,
	}
}

// SetNuggetSystem sets the nugget system reference for Tab jump functionality
func (h *InputHandler) SetNuggetSystem(nuggetSystem *systems.NuggetSystem) {
	h.nuggetSystem = nuggetSystem
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
			h.ctx.DeleteOperator = false
		} else if h.ctx.IsCommandMode() {
			h.ctx.Mode = engine.ModeNormal
			h.ctx.CommandText = ""
			h.ctx.DeleteOperator = false
		} else if h.ctx.IsInsertMode() {
			h.ctx.Mode = engine.ModeNormal
			h.ctx.DeleteOperator = false
		}
		return true
	}

	// Mode-specific handling
	if h.ctx.IsInsertMode() {
		return h.handleInsertMode(ev)
	} else if h.ctx.IsSearchMode() {
		return h.handleSearchMode(ev)
	} else if h.ctx.IsCommandMode() {
		return h.handleCommandMode(ev)
	} else {
		return h.handleNormalMode(ev)
	}
}

// handleInsertMode handles input in insert mode
func (h *InputHandler) handleInsertMode(ev *tcell.EventKey) bool {
	// Handle arrow keys (reset heat)
	switch ev.Key() {
	case tcell.KeyUp:
		if h.ctx.CursorY > 0 {
			h.ctx.CursorY--
		}
		h.ctx.State.SetCursorY(h.ctx.CursorY)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyDown:
		if h.ctx.CursorY < h.ctx.GameHeight-1 {
			h.ctx.CursorY++
		}
		h.ctx.State.SetCursorY(h.ctx.CursorY)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyLeft:
		if h.ctx.CursorX > 0 {
			h.ctx.CursorX--
		}
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyRight:
		if h.ctx.CursorX < h.ctx.GameWidth-1 {
			h.ctx.CursorX++
		}
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyHome:
		h.ctx.CursorX = 0
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyEnd:
		h.ctx.CursorX = findLineEnd(h.ctx)
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyTab:
		// Tab: Jump to nugget if score >= 10
		if h.nuggetSystem != nil {
			score := h.ctx.State.GetScore()
			if score >= 10 {
				// Get nugget position
				x, y := h.nuggetSystem.JumpToNugget(h.ctx.World)
				if x >= 0 && y >= 0 {
					// Deduct 10 from score
					h.ctx.State.AddScore(-10)
					// Update cursor position atomically
					h.ctx.CursorX = x
					h.ctx.CursorY = y
					h.ctx.State.SetCursorX(x)
					h.ctx.State.SetCursorY(y)
				}
			}
		}
		return true
	case tcell.KeyRune:
		// SPACE key: move right without typing, no heat contribution
		if ev.Rune() == ' ' {
			if h.ctx.CursorX < h.ctx.GameWidth-1 {
				h.ctx.CursorX++
			}
			h.ctx.State.SetCursorX(h.ctx.CursorX)
			return true
		}
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

// handleCommandMode handles input in command mode
func (h *InputHandler) handleCommandMode(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter {
		// Prepare for command execution (just clear and exit for now)
		h.ctx.Mode = engine.ModeNormal
		h.ctx.CommandText = ""
		return true
	}
	if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
		if len(h.ctx.CommandText) > 0 {
			h.ctx.CommandText = h.ctx.CommandText[:len(h.ctx.CommandText)-1]
		}
		return true
	}
	if ev.Key() == tcell.KeyRune {
		h.ctx.CommandText += string(ev.Rune())
	}
	return true
}

// handleNormalMode handles input in normal mode
func (h *InputHandler) handleNormalMode(ev *tcell.EventKey) bool {
	// Handle arrow keys (work like h/j/k/l, but reset heat)
	switch ev.Key() {
	case tcell.KeyUp:
		if h.ctx.CursorY > 0 {
			h.ctx.CursorY--
		}
		h.ctx.State.SetCursorY(h.ctx.CursorY)
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = "" // Clear last command on next key
		return true
	case tcell.KeyDown:
		if h.ctx.CursorY < h.ctx.GameHeight-1 {
			h.ctx.CursorY++
		}
		h.ctx.State.SetCursorY(h.ctx.CursorY)
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = "" // Clear last command on next key
		return true
	case tcell.KeyLeft:
		if h.ctx.CursorX > 0 {
			h.ctx.CursorX--
		}
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = "" // Clear last command on next key
		return true
	case tcell.KeyRight:
		if h.ctx.CursorX < h.ctx.GameWidth-1 {
			h.ctx.CursorX++
		}
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = "" // Clear last command on next key
		return true
	case tcell.KeyHome:
		h.ctx.CursorX = 0
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = "" // Clear last command on next key
		return true
	case tcell.KeyEnd:
		h.ctx.CursorX = findLineEnd(h.ctx)
		h.ctx.State.SetCursorX(h.ctx.CursorX)
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = "" // Clear last command on next key
		return true
	case tcell.KeyTab:
		// Tab: Jump to nugget if score >= 10
		if h.nuggetSystem != nil {
			score := h.ctx.State.GetScore()
			if score >= 10 {
				// Get nugget position
				x, y := h.nuggetSystem.JumpToNugget(h.ctx.World)
				if x >= 0 && y >= 0 {
					// Deduct 10 from score
					h.ctx.State.AddScore(-10)
					// Update cursor position atomically
					h.ctx.CursorX = x
					h.ctx.CursorY = y
					h.ctx.State.SetCursorX(x)
					h.ctx.State.SetCursorY(y)
				}
			}
		}
		h.ctx.LastCommand = "" // Clear last command
		return true
	case tcell.KeyEnter:
		// Activate ping grid for 1 second
		h.ctx.SetPingActive(true)
		h.ctx.SetPingGridTimer(1.0)
		h.ctx.MotionCount = 0
		h.ctx.MotionCommand = ""
		h.ctx.LastCommand = ""
		return true
	}

	if ev.Key() == tcell.KeyRune {
		char := ev.Rune()

		// Handle waiting for 'f' character
		if h.ctx.WaitingForF {
			// Use PendingCount if set, otherwise default to 1
			count := h.ctx.PendingCount
			if count == 0 {
				count = 1
			}

			// Build command string: [count]f[char]
			cmd := h.buildCommandString('f', char, count, false)
			ExecuteFindChar(h.ctx, char, count)

			// Clear multi-keystroke state
			h.ctx.WaitingForF = false
			h.ctx.PendingCount = 0
			h.ctx.MotionCount = 0
			h.ctx.LastCommand = cmd
			return true
		}

		// Handle waiting for 'F' character (backward find)
		if h.ctx.WaitingForFBackward {
			// Use PendingCount if set, otherwise default to 1
			count := h.ctx.PendingCount
			if count == 0 {
				count = 1
			}

			// Build command string: [count]F[char]
			cmd := h.buildCommandString('F', char, count, false)
			ExecuteFindCharBackward(h.ctx, char, count)

			// Clear multi-keystroke state
			h.ctx.WaitingForFBackward = false
			h.ctx.PendingCount = 0
			h.ctx.MotionCount = 0
			h.ctx.LastCommand = cmd
			return true
		}

		// Handle waiting for 't' character (till forward)
		if h.ctx.WaitingForT {
			// Use PendingCount if set, otherwise default to 1
			count := h.ctx.PendingCount
			if count == 0 {
				count = 1
			}

			// Build command string: [count]t[char]
			cmd := h.buildCommandString('t', char, count, false)
			ExecuteTillChar(h.ctx, char, count)

			// Clear multi-keystroke state
			h.ctx.WaitingForT = false
			h.ctx.PendingCount = 0
			h.ctx.MotionCount = 0
			h.ctx.LastCommand = cmd
			return true
		}

		// Handle waiting for 'T' character (till backward)
		if h.ctx.WaitingForTBackward {
			// Use PendingCount if set, otherwise default to 1
			count := h.ctx.PendingCount
			if count == 0 {
				count = 1
			}

			// Build command string: [count]T[char]
			cmd := h.buildCommandString('T', char, count, false)
			ExecuteTillCharBackward(h.ctx, char, count)

			// Clear multi-keystroke state
			h.ctx.WaitingForTBackward = false
			h.ctx.PendingCount = 0
			h.ctx.MotionCount = 0
			h.ctx.LastCommand = cmd
			return true
		}

		// Handle numbers for count (BEFORE DeleteOperator check, so d2w works)
		if char >= '0' && char <= '9' {
			// Special case: '0' is a motion (line start) when not following a number
			// and not in delete operator mode
			if char == '0' && h.ctx.MotionCount == 0 && !h.ctx.DeleteOperator {
				ExecuteMotion(h.ctx, char, 1)
				h.ctx.MotionCommand = ""
				h.ctx.LastCommand = "0"
				return true
			}
			h.ctx.MotionCount = h.ctx.MotionCount*10 + int(char-'0')
			// Don't clear LastCommand when building count
			return true
		}

		// Handle delete operator
		if h.ctx.DeleteOperator {
			// Build command string: [count]d[motion]
			cmd := h.buildCommandString('d', char, h.ctx.MotionCount, false)
			ExecuteDeleteMotion(h.ctx, char, h.ctx.MotionCount)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.CommandPrefix = 0
			h.ctx.LastCommand = cmd
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
			h.ctx.DeleteOperator = false
			h.ctx.LastCommand = "" // Clear on mode change
			return true
		}

		// Enter search mode
		if char == '/' {
			h.ctx.Mode = engine.ModeSearch
			h.ctx.SearchText = ""
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.DeleteOperator = false
			h.ctx.LastCommand = "" // Clear on mode change
			return true
		}

		// Enter command mode
		if char == ':' {
			h.ctx.Mode = engine.ModeCommand
			h.ctx.CommandText = ""
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.DeleteOperator = false
			h.ctx.LastCommand = "" // Clear on mode change
			return true
		}

		// Repeat search
		if char == 'n' {
			RepeatSearch(h.ctx, true)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.LastCommand = "n"
			return true
		}

		if char == 'N' {
			RepeatSearch(h.ctx, false)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.LastCommand = "N"
			return true
		}

		// D - delete to end of line (equivalent to d$)
		if char == 'D' {
			cmd := h.buildCommandString('D', 0, count, true)
			ExecuteDeleteMotion(h.ctx, '$', count)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.CommandPrefix = 0
			h.ctx.LastCommand = cmd
			return true
		}

		// Delete operator
		if char == 'd' {
			if h.ctx.CommandPrefix == 'd' {
				// dd - delete line
				cmd := h.buildCommandString('d', 'd', count, false)
				ExecuteDeleteMotion(h.ctx, 'd', count)
				h.ctx.MotionCount = 0
				h.ctx.MotionCommand = ""
				h.ctx.CommandPrefix = 0
				h.ctx.LastCommand = cmd
			} else {
				h.ctx.DeleteOperator = true
				h.ctx.CommandPrefix = 'd'
				// Don't clear LastCommand yet - waiting for motion
			}
			return true
		}

		// Handle 'g' prefix commands
		if h.ctx.CommandPrefix == 'g' {
			// Second character after 'g'
			if char == 'g' {
				// gg - goto top (same column)
				cmd := h.buildCommandString('g', 'g', count, false)
				h.ctx.CursorY = 0
				h.ctx.State.SetCursorY(h.ctx.CursorY)
				h.ctx.CommandPrefix = 0
				h.ctx.MotionCount = 0
				h.ctx.LastCommand = cmd
				return true
			} else if char == 'o' {
				// go - goto top left (first row, first column)
				cmd := h.buildCommandString('g', 'o', count, false)
				h.ctx.CursorY = 0
				h.ctx.CursorX = 0
				h.ctx.State.SetCursorX(h.ctx.CursorX)
				h.ctx.State.SetCursorY(h.ctx.CursorY)
				h.ctx.CommandPrefix = 0
				h.ctx.MotionCount = 0
				h.ctx.LastCommand = cmd
				return true
			} else {
				// Unknown 'g' command, reset
				h.ctx.CommandPrefix = 0
				h.ctx.MotionCount = 0
				h.ctx.LastCommand = "" // Clear on error
				return true
			}
		}

		// Start 'g' prefix
		if char == 'g' {
			h.ctx.CommandPrefix = 'g'
			// Don't clear LastCommand yet - waiting for second char
			return true
		}

		// Handle 'f' command (find character - multi-keystroke)
		if char == 'f' {
			h.ctx.WaitingForF = true
			h.ctx.PendingCount = count // Preserve count for when target char is typed
			// Don't clear MotionCount yet - will be cleared when command completes
			// Don't set LastCommand yet - waiting for target character
			return true
		}

		// Handle 'F' command (find character backward - multi-keystroke)
		if char == 'F' {
			h.ctx.WaitingForFBackward = true
			h.ctx.PendingCount = count // Preserve count for when target char is typed
			// Don't clear MotionCount yet - will be cleared when command completes
			// Don't set LastCommand yet - waiting for target character
			return true
		}

		// Handle 't' command (till character forward - multi-keystroke)
		if char == 't' {
			h.ctx.WaitingForT = true
			h.ctx.PendingCount = count // Preserve count for when target char is typed
			// Don't clear MotionCount yet - will be cleared when command completes
			// Don't set LastCommand yet - waiting for target character
			return true
		}

		// Handle 'T' command (till character backward - multi-keystroke)
		if char == 'T' {
			h.ctx.WaitingForTBackward = true
			h.ctx.PendingCount = count // Preserve count for when target char is typed
			// Don't clear MotionCount yet - will be cleared when command completes
			// Don't set LastCommand yet - waiting for target character
			return true
		}

		// Handle ';' command (repeat last find/till in same direction)
		if char == ';' {
			RepeatFindChar(h.ctx, false)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.LastCommand = ";"
			return true
		}

		// Handle ',' command (repeat last find/till in opposite direction)
		if char == ',' {
			RepeatFindChar(h.ctx, true)
			h.ctx.MotionCount = 0
			h.ctx.MotionCommand = ""
			h.ctx.LastCommand = ","
			return true
		}

		// Execute motion commands
		cmd := h.buildCommandString(char, 0, count, true)
		ExecuteMotion(h.ctx, char, count)
		h.ctx.MotionCount = 0
		h.ctx.MotionCommand = ""
		h.ctx.CommandPrefix = 0
		h.ctx.LastCommand = cmd
	}
	return true
}

// buildCommandString builds a display string for the last command
func (h *InputHandler) buildCommandString(action rune, motion rune, count int, singleChar bool) string {
	var cmd string

	// Add count prefix if present
	if count > 1 {
		cmd = fmt.Sprintf("%d", count)
	}

	// Add action
	cmd += string(action)

	// Add motion if present
	if !singleChar && motion != 0 {
		cmd += string(motion)
	}

	return cmd
}
