package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/lixenwraith/vi-fighter/terminal"
)

const (
	minTermWidth  = 120
	minTermHeight = 24
)

var outputPath string

// init registers command-line flags
func init() {
	flag.StringVar(&outputPath, "o", "catalog.txt", "output file path")
}

// main initializes terminal, builds index, and runs event loop
func main() {
	flag.Parse()

	term := terminal.New()
	if err := term.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "terminal init:", err)
		os.Exit(1)
	}
	defer term.Fini()

	// Block until terminal meets minimum size
	if !waitForMinSize(term) {
		return
	}

	w, h := term.Size()

	index, err := BuildIndex(".")
	if err != nil {
		term.Fini()
		fmt.Fprintln(os.Stderr, "index build:", err)
		os.Exit(1)
	}

	_, rgErr := exec.LookPath("rg")

	// Determine initial category
	currentCat := ""
	if len(index.CategoryNames) > 0 {
		currentCat = index.CategoryNames[0]
	}

	app := &AppState{
		Term:            term,
		Index:           index,
		FocusPane:       PaneTree,
		Selected:        make(map[string]bool),
		ExpandDeps:      true,
		DepthLimit:      2,
		Filter:          NewFilterState(),
		RgAvailable:     rgErr == nil,
		CurrentCategory: currentCat,
		CategoryNames:   index.CategoryNames,
		CategoryUI:      make(map[string]*CategoryUIState),
		StartCollapsed:  true,
		Width:           w,
		Height:          h,
	}

	// Initialize UI state for all categories
	for _, cat := range index.CategoryNames {
		app.CategoryUI[cat] = NewCategoryUIState()
	}

	app.TreeRoot = BuildTree(index)
	app.RefreshTreeFlat()
	app.RefreshLixenFlat()

	// Apply initial collapsed state
	if app.StartCollapsed {
		app.applyInitialCollapsedState()
	}

	app.Render()

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventResize:
			if ev.Width < minTermWidth || ev.Height < minTermHeight {
				renderSizeWarning(term, ev.Width, ev.Height)
				continue
			}
			app.Width = ev.Width
			app.Height = ev.Height
			app.Render()
			continue

		case terminal.EventKey:
			if ev.Key == terminal.KeyCtrlC {
				return
			}

			quit, _ := app.HandleEvent(ev)
			if quit {
				return
			}
		}

		app.Render()
	}
}

// waitForMinSize blocks until terminal meets minimum dimensions or user quits
func waitForMinSize(term terminal.Terminal) bool {
	w, h := term.Size()
	if w >= minTermWidth && h >= minTermHeight {
		return true
	}

	renderSizeWarning(term, w, h)

	for {
		ev := term.PollEvent()
		switch ev.Type {
		case terminal.EventResize:
			if ev.Width >= minTermWidth && ev.Height >= minTermHeight {
				return true
			}
			renderSizeWarning(term, ev.Width, ev.Height)
		case terminal.EventKey:
			if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyEscape {
				return false
			}
		}
	}
}

// renderSizeWarning displays centered minimum size message
func renderSizeWarning(term terminal.Terminal, w, h int) {
	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: terminal.RGB{200, 200, 200}, Bg: terminal.RGB{20, 20, 30}}
	}

	msg1 := fmt.Sprintf("Terminal too small: %dx%d", w, h)
	msg2 := fmt.Sprintf("Minimum required: %dx%d", minTermWidth, minTermHeight)
	msg3 := "Resize terminal or press Ctrl+C to exit"

	cy := h / 2
	drawCentered(cells, w, cy-1, msg1)
	drawCentered(cells, w, cy, msg2)
	drawCentered(cells, w, cy+2, msg3)

	term.Flush(cells, w, h)
}

// drawCentered renders text centered on row
func drawCentered(cells []terminal.Cell, w, y int, text string) {
	if y < 0 || y*w >= len(cells) {
		return
	}
	x := (w - len(text)) / 2
	if x < 0 {
		x = 0
	}
	for i, r := range text {
		if x+i >= w {
			break
		}
		cells[y*w+x+i] = terminal.Cell{
			Rune: r,
			Fg:   terminal.RGB{255, 200, 100},
			Bg:   terminal.RGB{20, 20, 30},
		}
	}
}