package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

const (
	minTermWidth  = 120
	minTermHeight = 24
)

var outputPath string

func init() {
	flag.StringVar(&outputPath, "o", "catalog.txt", "output file path")
}

func main() {
	flag.Parse()

	term := terminal.New()
	if err := term.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "terminal init:", err)
		os.Exit(1)
	}
	defer term.Fini()

	w, h := term.Size()

	index, err := BuildIndex(".")
	if err != nil {
		term.Fini()
		fmt.Fprintln(os.Stderr, "index build:", err)
		os.Exit(1)
	}

	_, rgErr := exec.LookPath("rg")

	app := &AppState{
		Term:             term,
		Index:            index,
		Theme:            DefaultTheme,
		FocusPane:        PaneTree,
		Selected:         make(map[string]bool),
		ExpandDeps:       true,
		DepthLimit:       2,
		Filter:           NewFilterState(),
		RgAvailable:      rgErr == nil,
		CategoryNames:    index.CategoryNames,
		LixenUI:          NewCategoryUIState(),
		DepByState:       NewDetailPaneState(),
		DepOnState:       NewDetailPaneState(),
		DepAnalysisCache: make(map[string]*DependencyAnalysis),
		TreeState:        tui.NewTreeState(h - 4),
		TreeExpand:       tui.NewTreeExpansion(),
		InputField:       tui.NewTextFieldState(""),
		Width:            w,
		Height:           h,
	}

	app.TreeRoot = BuildTree(index)
	app.RefreshTreeFlat()
	app.RefreshLixenFlat()

	app.refreshDetailPanes()

	app.Render()

	for {
		if app.Width < minTermWidth || app.Height < minTermHeight {
			renderSizeWarning(app.Term, app.Width, app.Height)
		} else {
			app.Render()
		}

		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventResize:
			// Update dimensions
			app.Width = ev.Width
			app.Height = ev.Height

			// Adjust UI state that depends on height
			treeH := ev.Height - 4
			if treeH < 1 {
				treeH = 1
			}
			app.TreeState.SetVisible(treeH)

			// Continue loop to re-render immediately
			continue

		case terminal.EventKey:
			if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyCtrlQ {
				return
			}

			// Only handle events if terminal is large enough
			if app.Width >= minTermWidth && app.Height >= minTermHeight {
				quit, _ := app.HandleEvent(ev)
				if quit {
					return
				}
			}
		}

		app.Render()
	}
}

// renderSizeWarning displays centered minimum size message
func renderSizeWarning(term terminal.Terminal, w, h int) {
	// Ensure buffer size matches current dimensions to avoid panics
	if w <= 0 || h <= 0 {
		return
	}

	cells := make([]terminal.Cell, w*h)
	bg := terminal.RGB{R: 20, G: 20, B: 30}
	fg := terminal.RGB{R: 200, G: 200, B: 200}
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: fg, Bg: bg}
	}

	root := tui.NewRegion(cells, w, 0, 0, w, h)

	msg1 := fmt.Sprintf("Terminal too small: %dx%d", w, h)
	msg2 := fmt.Sprintf("Minimum required: %dx%d", minTermWidth, minTermHeight)
	msg3 := "Resize terminal or press Ctrl+C/Q to exit"

	cy := h / 2
	if cy > 0 {
		root.TextCenter(cy-1, msg1, fg, bg, terminal.AttrNone)
	}
	root.TextCenter(cy, msg2, fg, bg, terminal.AttrNone)
	if cy+2 < h {
		root.TextCenter(cy+2, msg3, terminal.RGB{R: 140, G: 140, B: 140}, bg, terminal.AttrDim)
	}

	term.Flush(cells, w, h)
}