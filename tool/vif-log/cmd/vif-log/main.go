// vif-log — terminal viewer for vi-fighter JSONL diagnostic logs.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif-log/internal/app"
	"github.com/lixenwraith/vif-log/internal/filter"
)

const tickInterval = 50 * time.Millisecond // 20 fps clock

// filterSpec collects repeatable -f kind:arg flags.
type filterSpec []string

func (f *filterSpec) String() string { return strings.Join(*f, " ") }
func (f *filterSpec) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func main() {
	var specs filterSpec
	flag.Var(&specs, "f", "filter, repeatable: kind:regexp (sub msg tick run level find fields)")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: vif-log [-f kind:arg]... [file.jsonl... | directory]")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nfilters:")
		for _, d := range filter.Kinds() {
			fmt.Fprintf(os.Stderr, "  %-6s %s\n", d.Kind, d.Help)
		}
	}
	flag.Parse()

	term := terminal.New()
	defer func() {
		if r := recover(); r != nil {
			terminal.EmergencyReset(os.Stdout)
			panic(r)
		}
	}()
	if err := term.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "terminal init:", err)
		os.Exit(1)
	}

	a, err := app.New(term, flag.Args(), specs)
	if err != nil {
		term.Fini()
		fmt.Fprintln(os.Stderr, "init:", err)
		os.Exit(1)
	}
	defer a.Close()

	// Synthetic tick: the input reader never emits {EventKey, KeyNone}, so it is
	// a collision-free wake-up for index progress and incremental filtering.
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(tickInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				term.PostEvent(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
			}
		}
	}()

	for !a.Quit() {
		a.Render()
		a.Handle(term.PollEvent())
	}
	close(done)
	term.Fini()
}
