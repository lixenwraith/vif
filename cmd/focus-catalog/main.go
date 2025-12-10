package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/lixenwraith/vi-fighter/terminal"
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
		Term:          term,
		Index:         index,
		FocusPane:     PaneLeft,
		Selected:      make(map[string]bool),
		ExpandDeps:    true,
		DepthLimit:    2,
		Filter:        NewFilterState(),
		RgAvailable:   rgErr == nil,
		GroupExpanded: make(map[string]bool),
		Width:         w,
		Height:        h,
	}

	// Build tree from index
	app.TreeRoot = BuildTree(index)
	app.RefreshTreeFlat()

	// Build tag list
	app.RefreshTagFlat()

	app.Render()

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventResize:
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