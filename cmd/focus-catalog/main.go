package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"

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
		Term:           term,
		Index:          index,
		Selected:       make(map[string]bool),
		ExpandDeps:     true,
		DepthLimit:     2,
		KeywordMatches: make(map[string]bool),
		RgAvailable:    rgErr == nil,
		Width:          w,
		Height:         h,
	}

	app.AllPackages = make([]string, 0, len(index.Packages))
	for name := range index.Packages {
		app.AllPackages = append(app.AllPackages, name)
	}
	sort.Strings(app.AllPackages)
	app.UpdatePackageList()

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

			quit, output := app.HandleEvent(ev)
			if quit {
				return
			}
			if output {
				files := app.ComputeOutputFiles()
				err := WriteOutputFile(outputPath, files)
				if err != nil {
					app.Message = fmt.Sprintf("write error: %v", err)
					app.Render()
					continue
				}
				app.Message = fmt.Sprintf("wrote %d files to %s", len(files), outputPath)
				app.Render()
				// Brief pause to show message before exit
				term.PollEvent()
				return
			}
		}

		app.Render()
	}
}
