//go:build !vif_headless && !vif_noaudio && !wasm

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/lixenwraith/vif/internal/soundlab"
)

func runREPL(s *soundlab.Session, in io.Reader, out io.Writer) {
	fmt.Fprintln(out, "soundlab — vif audio editor. `help` lists commands, `quit` exits.")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for {
		fmt.Fprint(out, "> ")
		if !sc.Scan() {
			fmt.Fprintln(out)
			return
		}
		err := soundlab.Execute(s, sc.Text())
		if errors.Is(err, soundlab.ErrQuit) {
			return
		}
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
		}
	}
}
