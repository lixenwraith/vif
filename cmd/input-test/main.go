package main

import (
	"fmt"
	"os"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

func main() {
	term := terminal.New()
	if err := term.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
		os.Exit(1)
	}
	defer term.Fini()

	// Enable all mouse events
	term.SetMouseMode(terminal.MouseModeClick | terminal.MouseModeDrag | terminal.MouseModeMotion)

	w, h := term.Size()
	cells := make([]terminal.Cell, w*h)

	// Draggable object state
	objX, objY := w/2, h/2
	dragging := false

	// Event log (last N events)
	const maxLog = 10
	eventLog := make([]string, 0, maxLog)

	addLog := func(s string) {
		if len(eventLog) >= maxLog {
			copy(eventLog, eventLog[1:])
			eventLog = eventLog[:maxLog-1]
		}
		eventLog = append(eventLog, s)
	}

	render := func() {
		region := tui.NewRegion(cells, w, 0, 0, w, h)
		region.Fill(terminal.RGB{R: 20, G: 20, B: 30})

		// Title
		region.TextCenter(0, "Input Test - Press keys, move mouse, drag the [X] - Press Ctrl+C to quit",
			terminal.RGB{R: 200, G: 200, B: 200}, terminal.RGB{R: 40, G: 40, B: 60}, terminal.AttrBold)

		// Divider
		region.HLine(1, tui.LineSingle, terminal.RGB{R: 60, G: 60, B: 80})

		// Event log
		for i, entry := range eventLog {
			y := 2 + i
			if y >= h-3 {
				break
			}
			region.Text(1, y, entry, terminal.RGB{R: 180, G: 180, B: 180}, terminal.RGB{}, terminal.AttrNone)
		}

		// Draggable object
		if objX >= 0 && objX < w-2 && objY >= 0 && objY < h {
			fg := terminal.RGB{R: 100, G: 255, B: 100}
			if dragging {
				fg = terminal.RGB{R: 255, G: 255, B: 100}
			}
			region.Text(objX, objY, "[X]", fg, terminal.RGB{R: 40, G: 40, B: 60}, terminal.AttrBold)
		}

		// Status bar
		region.HLine(h-2, tui.LineSingle, terminal.RGB{R: 60, G: 60, B: 80})
		status := fmt.Sprintf("Size: %dx%d | Object: (%d,%d) | Dragging: %v", w, h, objX, objY, dragging)
		region.Text(1, h-1, status, terminal.RGB{R: 140, G: 140, B: 160}, terminal.RGB{}, terminal.AttrNone)

		term.Flush(cells, w, h)
	}

	render()

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventKey:
			if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyCtrlQ {
				return
			}
			addLog(formatKeyEvent(ev))

		case terminal.EventMouse:
			addLog(formatMouseEvent(ev))

			// Handle dragging
			switch ev.MouseAction {
			case terminal.MouseActionPress:
				if ev.MouseBtn == terminal.MouseBtnLeft {
					// Check if clicking on object
					if ev.MouseX >= objX && ev.MouseX < objX+3 && ev.MouseY == objY {
						dragging = true
					}
				}
			case terminal.MouseActionRelease:
				dragging = false
			case terminal.MouseActionDrag:
				if dragging {
					objX = ev.MouseX
					objY = ev.MouseY
					// Clamp to screen
					if objX < 0 {
						objX = 0
					}
					if objX > w-3 {
						objX = w - 3
					}
					if objY < 0 {
						objY = 0
					}
					if objY > h-1 {
						objY = h - 1
					}
				}
			}

		case terminal.EventResize:
			w, h = ev.Width, ev.Height
			cells = make([]terminal.Cell, w*h)
			addLog(fmt.Sprintf("RESIZE: %dx%d", w, h))

		case terminal.EventError:
			addLog(fmt.Sprintf("ERROR: %v", ev.Err))

		case terminal.EventClosed:
			return
		}

		render()
	}
}

func formatKeyEvent(ev terminal.Event) string {
	var mods string
	if ev.Modifiers&terminal.ModShift != 0 {
		mods += "Shift+"
	}
	if ev.Modifiers&terminal.ModAlt != 0 {
		mods += "Alt+"
	}
	if ev.Modifiers&terminal.ModCtrl != 0 {
		mods += "Ctrl+"
	}

	keyName := keyToString(ev.Key)
	if ev.Key == terminal.KeyRune {
		if ev.Rune >= 0x20 && ev.Rune < 0x7f {
			keyName = fmt.Sprintf("'%c'", ev.Rune)
		} else {
			keyName = fmt.Sprintf("U+%04X", ev.Rune)
		}
	}

	return fmt.Sprintf("KEY: %s%s", mods, keyName)
}

func formatMouseEvent(ev terminal.Event) string {
	var mods string
	if ev.Modifiers&terminal.ModShift != 0 {
		mods += "Shift+"
	}
	if ev.Modifiers&terminal.ModAlt != 0 {
		mods += "Alt+"
	}
	if ev.Modifiers&terminal.ModCtrl != 0 {
		mods += "Ctrl+"
	}

	return fmt.Sprintf("MOUSE: %s%s %s @ (%d,%d)",
		mods, ev.MouseBtn.String(), ev.MouseAction.String(), ev.MouseX, ev.MouseY)
}

func keyToString(k terminal.Key) string {
	switch k {
	case terminal.KeyNone:
		return "None"
	case terminal.KeyRune:
		return "Rune"
	case terminal.KeyEscape:
		return "Escape"
	case terminal.KeyEnter:
		return "Enter"
	case terminal.KeyTab:
		return "Tab"
	case terminal.KeyBacktab:
		return "Backtab"
	case terminal.KeyBackspace:
		return "Backspace"
	case terminal.KeyDelete:
		return "Delete"
	case terminal.KeySpace:
		return "Space"
	case terminal.KeyUp:
		return "Up"
	case terminal.KeyDown:
		return "Down"
	case terminal.KeyLeft:
		return "Left"
	case terminal.KeyRight:
		return "Right"
	case terminal.KeyHome:
		return "Home"
	case terminal.KeyEnd:
		return "End"
	case terminal.KeyPageUp:
		return "PageUp"
	case terminal.KeyPageDown:
		return "PageDown"
	case terminal.KeyInsert:
		return "Insert"
	case terminal.KeyF1:
		return "F1"
	case terminal.KeyF2:
		return "F2"
	case terminal.KeyF3:
		return "F3"
	case terminal.KeyF4:
		return "F4"
	case terminal.KeyF5:
		return "F5"
	case terminal.KeyF6:
		return "F6"
	case terminal.KeyF7:
		return "F7"
	case terminal.KeyF8:
		return "F8"
	case terminal.KeyF9:
		return "F9"
	case terminal.KeyF10:
		return "F10"
	case terminal.KeyF11:
		return "F11"
	case terminal.KeyF12:
		return "F12"
	case terminal.KeyCtrlA:
		return "Ctrl+A"
	case terminal.KeyCtrlB:
		return "Ctrl+B"
	case terminal.KeyCtrlC:
		return "Ctrl+C"
	case terminal.KeyCtrlD:
		return "Ctrl+D"
	case terminal.KeyCtrlE:
		return "Ctrl+E"
	case terminal.KeyCtrlF:
		return "Ctrl+F"
	case terminal.KeyCtrlG:
		return "Ctrl+G"
	case terminal.KeyCtrlH:
		return "Ctrl+H"
	case terminal.KeyCtrlK:
		return "Ctrl+K"
	case terminal.KeyCtrlL:
		return "Ctrl+L"
	case terminal.KeyCtrlN:
		return "Ctrl+N"
	case terminal.KeyCtrlO:
		return "Ctrl+O"
	case terminal.KeyCtrlP:
		return "Ctrl+P"
	case terminal.KeyCtrlQ:
		return "Ctrl+Q"
	case terminal.KeyCtrlR:
		return "Ctrl+R"
	case terminal.KeyCtrlS:
		return "Ctrl+S"
	case terminal.KeyCtrlT:
		return "Ctrl+T"
	case terminal.KeyCtrlU:
		return "Ctrl+U"
	case terminal.KeyCtrlV:
		return "Ctrl+V"
	case terminal.KeyCtrlW:
		return "Ctrl+W"
	case terminal.KeyCtrlX:
		return "Ctrl+X"
	case terminal.KeyCtrlY:
		return "Ctrl+Y"
	case terminal.KeyCtrlZ:
		return "Ctrl+Z"
	case terminal.KeyCtrlSpace:
		return "Ctrl+Space"
	case terminal.KeyCtrlBackslash:
		return "Ctrl+\\"
	case terminal.KeyCtrlBracketLeft:
		return "Ctrl+["
	case terminal.KeyCtrlBracketRight:
		return "Ctrl+]"
	case terminal.KeyCtrlCaret:
		return "Ctrl+^"
	case terminal.KeyCtrlUnderscore:
		return "Ctrl+_"
	default:
		return fmt.Sprintf("Key(%d)", k)
	}
}