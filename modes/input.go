package modes

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// InputHandler processes user input events
type InputHandler struct {
	ctx      *engine.GameContext
	machine  *InputMachine
	bindings *BindingTable
}

// NewInputHandler creates a new input handler
func NewInputHandler(ctx *engine.GameContext) *InputHandler {
	bindings, _ := LoadBindings("")
	return &InputHandler{
		ctx:      ctx,
		machine:  NewInputMachine(),
		bindings: bindings,
	}
}

// HandleEvent processes a tcell event and returns false if the game should exit
func (h *InputHandler) HandleEvent(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		// Clear status message on any new key press interaction
		// This ensures error messages like "Unknown command" don't persist forever
		// We do this before processing the key so the user sees the clear immediately upon acting
		if h.ctx.StatusMessage != "" {
			h.ctx.StatusMessage = ""
		}
		return h.handleKeyEvent(ev)
	case *tcell.EventResize:
		h.ctx.HandleResize()
		return true
	}
	return true
}

// handleKeyEvent processes keyboard events
func (h *InputHandler) handleKeyEvent(ev *tcell.EventKey) bool {
	// Record action for APM calculation on any valid key event
	h.ctx.State.RecordAction()

	// Handle exit keys
	if ev.Key() == tcell.KeyCtrlQ || ev.Key() == tcell.KeyCtrlC {
		return false
	}

	// Handle Ctrl+S - Toggle audio mute
	if ev.Key() == tcell.KeyCtrlS {
		if h.ctx.AudioEngine != nil {
			_ = h.ctx.ToggleAudioMute()
		}
		return true
	}

	// Handle Escape
	if ev.Key() == tcell.KeyEscape {
		// Reset state machine on any ESC
		h.machine.Reset()

		if h.ctx.IsSearchMode() {
			h.ctx.Mode = engine.ModeNormal
			h.ctx.SearchText = ""
		} else if h.ctx.IsCommandMode() {
			h.ctx.Mode = engine.ModeNormal
			h.ctx.CommandText = ""
			h.ctx.SetPaused(false)
		} else if h.ctx.IsInsertMode() {
			h.ctx.Mode = engine.ModeNormal
		} else if h.ctx.IsOverlayMode() {
			h.ctx.OverlayActive = false
			h.ctx.OverlayTitle = ""
			h.ctx.OverlayContent = nil
			h.ctx.OverlayScroll = 0
			h.ctx.Mode = engine.ModeNormal
			h.ctx.SetPaused(false)
		} else {
			// Normal mode - activate ping grid
			h.ctx.SetPingActive(true)
			h.ctx.SetPingGridTimer(1.0)
		}
		return true
	}

	// Mode-specific handling
	if h.ctx.IsOverlayMode() {
		return h.handleOverlayMode(ev)
	} else if h.ctx.IsInsertMode() {
		return h.handleInsertMode(ev)
	} else if h.ctx.IsSearchMode() {
		return h.handleSearchMode(ev)
	} else if h.ctx.IsCommandMode() {
		return h.handleCommandMode(ev)
	} else {
		return h.handleNormalMode(ev)
	}
}

// handleNormalMode handles input in normal mode
func (h *InputHandler) handleNormalMode(ev *tcell.EventKey) bool {
	// Handle special keys (arrows, Tab, Enter, etc.)
	switch ev.Key() {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight,
		tcell.KeyHome, tcell.KeyEnd, tcell.KeyTab, tcell.KeyEnter:
		return h.handleNormalModeSpecialKeys(ev)
	}

	// Only process rune keys through state machine
	if ev.Key() != tcell.KeyRune {
		return true
	}

	// Delegate to state machine
	result := h.machine.Process(ev.Rune(), h.bindings)

	// Handle mode change
	if result.ModeChange != 0 {
		h.ctx.Mode = result.ModeChange
		if result.ModeChange == engine.ModeSearch {
			h.ctx.SearchText = ""
		} else if result.ModeChange == engine.ModeCommand {
			h.ctx.CommandText = ""
		}
	}

	// Execute action
	if result.Action != nil {
		result.Action(h.ctx)
	}

	return result.Continue
}

// handleNormalModeSpecialKeys handles non-rune keys in normal mode (arrows, Tab, Enter, etc.)
func (h *InputHandler) handleNormalModeSpecialKeys(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyUp:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if ok && pos.Y > 0 {
			pos.Y--
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyDown:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if ok && pos.Y < h.ctx.GameHeight-1 {
			pos.Y++
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyLeft:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if ok && pos.X > 0 {
			pos.X--
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyRight:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if ok && pos.X < h.ctx.GameWidth-1 {
			pos.X++
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyHome:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if ok {
			pos.X = 0
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyEnd:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if ok {
			pos.X = findLineEnd(h.ctx, pos.Y)
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyTab:
		// Tab: Jump to nugget if energy >= 10
		energy := h.ctx.State.GetEnergy()
		if energy < 10 {
			h.ctx.LastCommand = ""
			return true
		}

		nuggetID := engine.Entity(h.ctx.State.GetActiveNuggetID())
		if nuggetID == 0 {
			h.ctx.LastCommand = ""
			return true
		}

		nuggetPos, ok := h.ctx.World.Positions.Get(nuggetID)
		if !ok {
			h.ctx.LastCommand = ""
			return true
		}

		h.ctx.World.Positions.Add(h.ctx.CursorEntity, components.PositionComponent{
			X: nuggetPos.X,
			Y: nuggetPos.Y,
		})

		payload := &engine.EnergyTransactionPayload{
			Amount: -10,
			Source: "NuggetJump",
		}
		h.ctx.PushEvent(engine.EventEnergyTransaction, payload, h.ctx.PausableClock.Now())

		if h.ctx.AudioEngine != nil {
			cmd := audio.AudioCommand{
				Type:       audio.SoundBell,
				Priority:   1,
				Generation: uint64(h.ctx.State.GetFrameNumber()),
				Timestamp:  h.ctx.PausableClock.Now(),
			}
			h.ctx.AudioEngine.SendState(cmd)
		}
		h.ctx.LastCommand = ""
		return true

	case tcell.KeyEnter:
		// Directional cleaners if heat >= 10
		currentHeat := h.ctx.State.GetHeat()
		if currentHeat >= 10 {
			h.ctx.State.AddHeat(-10)

			cursorPos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
			if ok {
				payload := &engine.DirectionalCleanerPayload{
					OriginX: cursorPos.X,
					OriginY: cursorPos.Y,
				}
				h.ctx.PushEvent(engine.EventDirectionalCleanerRequest, payload, h.ctx.PausableClock.Now())
			}
		}
		h.ctx.LastCommand = ""
		return true
	}

	return true
}

// handleInsertMode handles input in insert mode
func (h *InputHandler) handleInsertMode(ev *tcell.EventKey) bool {
	// Handle arrow keys (reset heat)
	switch ev.Key() {
	case tcell.KeyUp:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if !ok {
			return true
		}
		if pos.Y > 0 {
			pos.Y--
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyDown:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if !ok {
			return true
		}
		if pos.Y < h.ctx.GameHeight-1 {
			pos.Y++
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyLeft:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if !ok {
			return true
		}
		if pos.X > 0 {
			pos.X--
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyRight:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if !ok {
			return true
		}
		if pos.X < h.ctx.GameWidth-1 {
			pos.X++
			h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		}
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyHome:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if !ok {
			return true
		}
		pos.X = 0
		h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyEnd:
		pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		if !ok {
			return true
		}
		pos.X = findLineEnd(h.ctx, pos.Y)
		h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
		h.ctx.State.SetHeat(0)
		return true
	case tcell.KeyTab:
		// Tab: Jump to nugget if energy >= 10
		energy := h.ctx.State.GetEnergy()
		if energy < 10 {
			return true
		}

		// Get active nugget from centralized state
		nuggetID := engine.Entity(h.ctx.State.GetActiveNuggetID())
		if nuggetID == 0 {
			return true
		}

		// Query nugget position from World
		nuggetPos, ok := h.ctx.World.Positions.Get(nuggetID)
		if !ok {
			return true
		}

		// Move cursor to nugget position
		h.ctx.World.Positions.Add(h.ctx.CursorEntity, components.PositionComponent{
			X: nuggetPos.X,
			Y: nuggetPos.Y,
		})

		// Deduct energy via event
		payload := &engine.EnergyTransactionPayload{
			Amount: -10,
			Source: "NuggetJump",
		}
		h.ctx.PushEvent(engine.EventEnergyTransaction, payload, h.ctx.PausableClock.Now())

		// Play bell sound
		if h.ctx.AudioEngine != nil {
			cmd := audio.AudioCommand{
				Type:       audio.SoundBell,
				Priority:   1,
				Generation: uint64(h.ctx.State.GetFrameNumber()),
				Timestamp:  h.ctx.PausableClock.Now(),
			}
			h.ctx.AudioEngine.SendState(cmd)
		}
		return true
	case tcell.KeyRune:
		// SPACE key: move right without typing, no heat contribution
		if ev.Rune() == ' ' {
			pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
			if ok && pos.X < h.ctx.GameWidth-1 {
				pos.X++
				h.ctx.World.Positions.Add(h.ctx.CursorEntity, pos)
			}
			return true
		}
		// Push typing event to queue (processed by EnergySystem via EventRouter)
		pos, _ := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
		// Acquire payload from pool to minimize GC pressure
		payload := engine.CharacterTypedPayloadPool.Get().(*engine.CharacterTypedPayload)
		payload.Char = ev.Rune()
		payload.X = pos.X
		payload.Y = pos.Y
		h.ctx.PushEvent(engine.EventCharacterTyped, payload, h.ctx.PausableClock.Now())
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
		// Execute command
		command := h.ctx.CommandText
		shouldContinue := ExecuteCommand(h.ctx, command)

		// Clear command text after execution
		h.ctx.CommandText = ""

		// Check if command switched to a different mode (e.g., :debug or :help activates overlay)
		// Only reset to normal mode if we're still in command mode
		if h.ctx.Mode == engine.ModeOverlay {
			// Command switched to Overlay mode - preserve mode and pause state
			// The overlay handler will manage cleanup when user exits
		} else {
			// Standard command finished - return to normal mode
			h.ctx.Mode = engine.ModeNormal
			h.ctx.SetPaused(false)
		}

		// Return the result from ExecuteCommand (false = exit game)
		return shouldContinue
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

// handleOverlayMode handles input in overlay mode
func (h *InputHandler) handleOverlayMode(ev *tcell.EventKey) bool {
	// ESC or ENTER closes the overlay
	if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter {
		h.ctx.OverlayActive = false
		h.ctx.OverlayTitle = ""
		h.ctx.OverlayContent = nil
		h.ctx.OverlayScroll = 0
		h.ctx.Mode = engine.ModeNormal
		h.ctx.SetPaused(false)
		return true
	}

	// TODO: Future enhancement - scroll with arrow keys or j/k
	// Handle up/down scrolling
	if ev.Key() == tcell.KeyUp || (ev.Key() == tcell.KeyRune && ev.Rune() == 'k') {
		if h.ctx.OverlayScroll > 0 {
			h.ctx.OverlayScroll--
		}
		return true
	}

	if ev.Key() == tcell.KeyDown || (ev.Key() == tcell.KeyRune && ev.Rune() == 'j') {
		// Only scroll if there's more content to show
		if h.ctx.OverlayScroll < len(h.ctx.OverlayContent)-1 {
			h.ctx.OverlayScroll++
		}
		return true
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