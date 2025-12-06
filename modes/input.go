package modes

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// InputHandler processes user input events
type InputHandler struct {
	ctx      *engine.GameContext
	machine  *InputMachine
	bindings *BindingTable
}

// NewInputHandler creates a new input handler
func NewInputHandler(ctx *engine.GameContext) *InputHandler {
	bindings := DefaultBindings()
	return &InputHandler{
		ctx:      ctx,
		machine:  NewInputMachine(),
		bindings: bindings,
	}
}

// HandleEvent processes a terminal event and returns false if the game should exit
func (h *InputHandler) HandleEvent(ev terminal.Event) bool {
	switch ev.Type {
	case terminal.EventKey:
		if h.ctx.StatusMessage != "" {
			h.ctx.StatusMessage = ""
		}
		return h.handleKeyEvent(ev)
	case terminal.EventResize:
		h.ctx.HandleResize()
		return true
	}
	return true
}

func (h *InputHandler) handleKeyEvent(ev terminal.Event) bool {
	h.ctx.State.RecordAction()

	if ev.Key == terminal.KeyCtrlQ || ev.Key == terminal.KeyCtrlC {
		return false
	}

	if ev.Key == terminal.KeyCtrlS {
		if h.ctx.AudioEngine != nil {
			_ = h.ctx.ToggleAudioMute()
		}
		return true
	}

	if ev.Key == terminal.KeyEscape {
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
			h.ctx.SetPingActive(true)
			h.ctx.SetPingGridTimer(1.0)
		}
		return true
	}

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

func (h *InputHandler) handleNormalMode(ev terminal.Event) bool {
	// Special keys - wrapped in RunSafe
	switch ev.Key {
	case terminal.KeyUp, terminal.KeyDown, terminal.KeyLeft, terminal.KeyRight,
		terminal.KeyHome, terminal.KeyEnd, terminal.KeyTab, terminal.KeyEnter,
		terminal.KeyBackspace:
		h.ctx.World.RunSafe(func() {
			h.handleNormalModeSpecialKeys(ev)
		})
		return true
	}

	if ev.Key != terminal.KeyRune {
		return true
	}

	result := h.machine.Process(ev.Rune, h.bindings)

	if result.ModeChange != 0 {
		h.ctx.Mode = result.ModeChange
		if result.ModeChange == engine.ModeSearch {
			h.ctx.SearchText = ""
		} else if result.ModeChange == engine.ModeCommand {
			h.ctx.CommandText = ""
		}
	}

	if result.Action != nil {
		if result.CommandString != "" {
			h.ctx.LastCommand = result.CommandString

			// Push splash event for command execution
			// We need cursor position for the splash origin
			originX, originY := 0, 0
			if pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity); ok {
				originX, originY = pos.X, pos.Y
			}

			h.ctx.PushEvent(events.EventSplashRequest, &events.SplashRequestPayload{
				Text:    result.CommandString,
				Color:   components.SplashColorNormal,
				OriginX: originX,
				OriginY: originY,
			}, h.ctx.PausableClock.Now())
		}
		h.ctx.World.RunSafe(func() {
			result.Action(h.ctx)
		})
	}

	return result.Continue
}

func (h *InputHandler) handleNormalModeSpecialKeys(ev terminal.Event) {
	pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
	if !ok {
		return
	}

	switch ev.Key {
	case terminal.KeyUp:
		result := MotionUp(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'k')

	case terminal.KeyDown:
		result := MotionDown(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'j')

	case terminal.KeyLeft:
		result := MotionLeft(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'h')

	case terminal.KeyRight:
		result := MotionRight(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'l')

	case terminal.KeyHome:
		result := MotionLineStart(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, '0')

	case terminal.KeyEnd:
		result := MotionLineEnd(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, '$')

	case terminal.KeyBackspace:
		result := MotionLeft(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'h')

	case terminal.KeyTab:
		h.ctx.PushEvent(events.EventNuggetJumpRequest, nil, h.ctx.PausableClock.Now())

	case terminal.KeyEnter:
		currentHeat := h.ctx.State.GetHeat()
		if currentHeat >= 10 {
			h.ctx.State.AddHeat(-10)

			cursorPos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
			if ok {
				payload := &events.DirectionalCleanerPayload{
					OriginX: cursorPos.X,
					OriginY: cursorPos.Y,
				}
				h.ctx.PushEvent(events.EventDirectionalCleanerRequest, payload, h.ctx.PausableClock.Now())
			}
		}
	}

	h.ctx.LastCommand = ""
}

func (h *InputHandler) handleInsertMode(ev terminal.Event) bool {
	pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
	if !ok {
		return true
	}

	switch ev.Key {
	case terminal.KeyUp:
		h.ctx.World.RunSafe(func() {
			result := MotionUp(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'k')
		})
		return true

	case terminal.KeyDown:
		h.ctx.World.RunSafe(func() {
			result := MotionDown(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'j')
		})
		return true

	case terminal.KeyLeft:
		h.ctx.World.RunSafe(func() {
			result := MotionLeft(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'h')
		})
		return true

	case terminal.KeyRight:
		h.ctx.World.RunSafe(func() {
			result := MotionRight(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'l')
		})
		return true

	case terminal.KeyHome:
		h.ctx.World.RunSafe(func() {
			result := MotionLineStart(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, '0')
		})
		return true

	case terminal.KeyEnd:
		h.ctx.World.RunSafe(func() {
			result := MotionLineEnd(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, '$')
		})
		return true

	case terminal.KeyBackspace:
		h.ctx.World.RunSafe(func() {
			p, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
			if ok && p.X > 0 {
				p.X--
				h.ctx.World.Positions.Add(h.ctx.CursorEntity, p)
			}
		})
		return true

	case terminal.KeyTab:
		h.ctx.PushEvent(events.EventNuggetJumpRequest, nil, h.ctx.PausableClock.Now())
		return true

	case terminal.KeyEnter:
		h.ctx.World.RunSafe(func() {
			currentHeat := h.ctx.State.GetHeat()
			if currentHeat >= 10 {
				h.ctx.State.AddHeat(-10)

				cursorPos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
				if ok {
					payload := &events.DirectionalCleanerPayload{
						OriginX: cursorPos.X,
						OriginY: cursorPos.Y,
					}
					h.ctx.PushEvent(events.EventDirectionalCleanerRequest, payload, h.ctx.PausableClock.Now())
				}
			}
		})
		return true

	case terminal.KeyRune:
		if ev.Rune == ' ' {
			h.ctx.World.RunSafe(func() {
				p, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
				if ok && p.X < h.ctx.GameWidth-1 {
					p.X++
					h.ctx.World.Positions.Add(h.ctx.CursorEntity, p)
				}
			})
			return true
		}
		// Push typing event (processed by EnergySystem via EventRouter)
		payload := events.CharacterTypedPayloadPool.Get().(*events.CharacterTypedPayload)
		payload.Char = ev.Rune
		payload.X = pos.X
		payload.Y = pos.Y
		h.ctx.PushEvent(events.EventCharacterTyped, payload, h.ctx.PausableClock.Now())
	}
	return true
}

func (h *InputHandler) handleSearchMode(ev terminal.Event) bool {
	if ev.Key == terminal.KeyEnter {
		if h.ctx.SearchText != "" {
			// Wrap in RunSafe as PerformSearch writes to ECS
			h.ctx.World.RunSafe(func() {
				if PerformSearch(h.ctx, h.ctx.SearchText, true) {
					h.ctx.LastSearchText = h.ctx.SearchText
				}
			})
		}
		h.ctx.Mode = engine.ModeNormal
		h.ctx.SearchText = ""
		return true
	}
	if ev.Key == terminal.KeyBackspace {
		if len(h.ctx.SearchText) > 0 {
			h.ctx.SearchText = h.ctx.SearchText[:len(h.ctx.SearchText)-1]
		}
		return true
	}
	if ev.Key == terminal.KeyRune {
		h.ctx.SearchText += string(ev.Rune)
	}
	return true
}

func (h *InputHandler) handleCommandMode(ev terminal.Event) bool {
	if ev.Key == terminal.KeyEnter {
		command := h.ctx.CommandText
		var shouldContinue bool

		// Wrap in RunSafe as commands like :new mutate world
		h.ctx.World.RunSafe(func() {
			shouldContinue = ExecuteCommand(h.ctx, command)
		})

		h.ctx.CommandText = ""

		if h.ctx.Mode == engine.ModeOverlay {
			// Command switched to Overlay mode
		} else {
			h.ctx.Mode = engine.ModeNormal
			h.ctx.SetPaused(false)
		}

		return shouldContinue
	}
	if ev.Key == terminal.KeyBackspace {
		if len(h.ctx.CommandText) > 0 {
			h.ctx.CommandText = h.ctx.CommandText[:len(h.ctx.CommandText)-1]
		}
		return true
	}
	if ev.Key == terminal.KeyRune {
		h.ctx.CommandText += string(ev.Rune)
	}
	return true
}

func (h *InputHandler) handleOverlayMode(ev terminal.Event) bool {
	if ev.Key == terminal.KeyEscape || ev.Key == terminal.KeyEnter {
		h.ctx.OverlayActive = false
		h.ctx.OverlayTitle = ""
		h.ctx.OverlayContent = nil
		h.ctx.OverlayScroll = 0
		h.ctx.Mode = engine.ModeNormal
		h.ctx.SetPaused(false)
		return true
	}

	if ev.Key == terminal.KeyUp || (ev.Key == terminal.KeyRune && ev.Rune == 'k') {
		if h.ctx.OverlayScroll > 0 {
			h.ctx.OverlayScroll--
		}
		return true
	}

	if ev.Key == terminal.KeyDown || (ev.Key == terminal.KeyRune && ev.Rune == 'j') {
		if h.ctx.OverlayScroll < len(h.ctx.OverlayContent)-1 {
			h.ctx.OverlayScroll++
		}
		return true
	}

	return true
}