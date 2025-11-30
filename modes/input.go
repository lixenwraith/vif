package modes

import (
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
	bindings := DefaultBindings()
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

func (h *InputHandler) handleKeyEvent(ev *tcell.EventKey) bool {
	h.ctx.State.RecordAction()

	if ev.Key() == tcell.KeyCtrlQ || ev.Key() == tcell.KeyCtrlC {
		return false
	}

	if ev.Key() == tcell.KeyCtrlS {
		if h.ctx.AudioEngine != nil {
			_ = h.ctx.ToggleAudioMute()
		}
		return true
	}

	if ev.Key() == tcell.KeyEscape {
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

func (h *InputHandler) handleNormalMode(ev *tcell.EventKey) bool {
	// Special keys - wrapped in RunSafe
	switch ev.Key() {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight,
		tcell.KeyHome, tcell.KeyEnd, tcell.KeyTab, tcell.KeyEnter,
		tcell.KeyBackspace, tcell.KeyBackspace2:
		h.ctx.World.RunSafe(func() {
			h.handleNormalModeSpecialKeys(ev)
		})
		return true
	}

	if ev.Key() != tcell.KeyRune {
		return true
	}

	result := h.machine.Process(ev.Rune(), h.bindings)

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
		}
		h.ctx.World.RunSafe(func() {
			result.Action(h.ctx)
		})
	}

	return result.Continue
}

func (h *InputHandler) handleNormalModeSpecialKeys(ev *tcell.EventKey) {
	pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
	if !ok {
		return
	}

	switch ev.Key() {
	case tcell.KeyUp:
		result := MotionUp(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'k')
		h.ctx.State.SetHeat(0)

	case tcell.KeyDown:
		result := MotionDown(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'j')
		h.ctx.State.SetHeat(0)

	case tcell.KeyLeft:
		result := MotionLeft(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'h')
		h.ctx.State.SetHeat(0)

	case tcell.KeyRight:
		result := MotionRight(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'l')
		h.ctx.State.SetHeat(0)

	case tcell.KeyHome:
		result := MotionLineStart(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, '0')
		h.ctx.State.SetHeat(0)

	case tcell.KeyEnd:
		result := MotionLineEnd(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, '$')
		h.ctx.State.SetHeat(0)

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		result := MotionLeft(h.ctx, pos.X, pos.Y, 1)
		OpMove(h.ctx, result, 'h')

	case tcell.KeyTab:
		energy := h.ctx.State.GetEnergy()
		if energy < 10 {
			return
		}

		nuggetID := engine.Entity(h.ctx.State.GetActiveNuggetID())
		if nuggetID == 0 {
			return
		}

		nuggetPos, ok := h.ctx.World.Positions.Get(nuggetID)
		if !ok {
			return
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

	case tcell.KeyEnter:
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
	}

	h.ctx.LastCommand = ""
}

func (h *InputHandler) handleInsertMode(ev *tcell.EventKey) bool {
	pos, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
	if !ok {
		return true
	}

	switch ev.Key() {
	case tcell.KeyUp:
		h.ctx.World.RunSafe(func() {
			result := MotionUp(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'k')
			h.ctx.State.SetHeat(0)
		})
		return true

	case tcell.KeyDown:
		h.ctx.World.RunSafe(func() {
			result := MotionDown(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'j')
			h.ctx.State.SetHeat(0)
		})
		return true

	case tcell.KeyLeft:
		h.ctx.World.RunSafe(func() {
			result := MotionLeft(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'h')
			h.ctx.State.SetHeat(0)
		})
		return true

	case tcell.KeyRight:
		h.ctx.World.RunSafe(func() {
			result := MotionRight(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, 'l')
			h.ctx.State.SetHeat(0)
		})
		return true

	case tcell.KeyHome:
		h.ctx.World.RunSafe(func() {
			result := MotionLineStart(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, '0')
			h.ctx.State.SetHeat(0)
		})
		return true

	case tcell.KeyEnd:
		h.ctx.World.RunSafe(func() {
			result := MotionLineEnd(h.ctx, pos.X, pos.Y, 1)
			OpMove(h.ctx, result, '$')
			h.ctx.State.SetHeat(0)
		})
		return true

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		h.ctx.World.RunSafe(func() {
			p, ok := h.ctx.World.Positions.Get(h.ctx.CursorEntity)
			if ok && p.X > 0 {
				p.X--
				h.ctx.World.Positions.Add(h.ctx.CursorEntity, p)
			}
		})
		return true

	case tcell.KeyTab:
		h.ctx.World.RunSafe(func() {
			energy := h.ctx.State.GetEnergy()
			if energy < 10 {
				return
			}

			nuggetID := engine.Entity(h.ctx.State.GetActiveNuggetID())
			if nuggetID == 0 {
				return
			}

			nuggetPos, ok := h.ctx.World.Positions.Get(nuggetID)
			if !ok {
				return
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
		})
		return true

	case tcell.KeyEnter:
		h.ctx.World.RunSafe(func() {
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
		})
		return true

	case tcell.KeyRune:
		if ev.Rune() == ' ' {
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
		payload := engine.CharacterTypedPayloadPool.Get().(*engine.CharacterTypedPayload)
		payload.Char = ev.Rune()
		payload.X = pos.X
		payload.Y = pos.Y
		h.ctx.PushEvent(engine.EventCharacterTyped, payload, h.ctx.PausableClock.Now())
	}
	return true
}

func (h *InputHandler) handleSearchMode(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter {
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

func (h *InputHandler) handleCommandMode(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter {
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

func (h *InputHandler) handleOverlayMode(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter {
		h.ctx.OverlayActive = false
		h.ctx.OverlayTitle = ""
		h.ctx.OverlayContent = nil
		h.ctx.OverlayScroll = 0
		h.ctx.Mode = engine.ModeNormal
		h.ctx.SetPaused(false)
		return true
	}

	if ev.Key() == tcell.KeyUp || (ev.Key() == tcell.KeyRune && ev.Rune() == 'k') {
		if h.ctx.OverlayScroll > 0 {
			h.ctx.OverlayScroll--
		}
		return true
	}

	if ev.Key() == tcell.KeyDown || (ev.Key() == tcell.KeyRune && ev.Rune() == 'j') {
		if h.ctx.OverlayScroll < len(h.ctx.OverlayContent)-1 {
			h.ctx.OverlayScroll++
		}
		return true
	}

	return true
}