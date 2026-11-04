package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/lixenwraith/stapes"
)

func runREPL(s *stapes.Session, in io.Reader, out io.Writer) {
	fmt.Fprintln(out, "stapes — audio editor. `help` lists commands, `quit` exits.")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for {
		fmt.Fprint(out, "> ")
		if !sc.Scan() {
			fmt.Fprintln(out)
			return
		}
		err := stapes.Execute(s, sc.Text())
		if errors.Is(err, stapes.ErrQuit) {
			return
		}
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
		}
	}
}
