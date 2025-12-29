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
	if w < minTermWidth || h < minTermHeight {
		term.Fini()
		fmt.Fprintf(os.Stderr, "Terminal too small: %dx%d (need %dx%d)\n", w, h, minTermWidth, minTermHeight)
		os.Exit(1)
	}

	index, err := BuildIndex(".")
	if err != nil {
		term.Fini()
		fmt.Fprintln(os.Stderr, "index build:", err)
		os.Exit(1)
	}

	_, rgErr := exec.LookPath("rg")

	currentCat := ""
	if len(index.CategoryNames) > 0 {
		currentCat = index.CategoryNames[0]
	}

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
		CurrentCategory:  currentCat,
		CategoryNames:    index.CategoryNames,
		CategoryUI:       make(map[string]*CategoryUIState),
		DepByState:       NewDetailPaneState(),
		DepOnState:       NewDetailPaneState(),
		DepAnalysisCache: make(map[string]*DependencyAnalysis),
		TreeState:        tui.NewTreeState(h - 4),
		TreeExpand:       tui.NewTreeExpansion(),
		InputField:       tui.NewTextFieldState(""),
		Width:            w,
		Height:           h,
	}

	for _, cat := range index.CategoryNames {
		app.CategoryUI[cat] = NewCategoryUIState()
	}

	app.TreeRoot = BuildTree(index)
	app.RefreshTreeFlat()
	app.RefreshLixenFlat()

	app.refreshDetailPanes()

	app.Render()

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventResize:
			if ev.Width < minTermWidth || ev.Height < minTermHeight {
				continue
			}
			app.Width = ev.Width
			app.Height = ev.Height
			app.TreeState.SetVisible(ev.Height - 4)
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