package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

func runREPL(s *Session, in io.Reader) {
	fmt.Fprintln(s.out, "soundlab — vi-fighter audio editor. `help` lists commands, `quit` exits.")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for {
		fmt.Fprint(s.out, "> ")
		if !sc.Scan() {
			fmt.Fprintln(s.out)
			return
		}
		err := Execute(s, sc.Text())
		if errors.Is(err, errQuit) {
			return
		}
		if err != nil {
			fmt.Fprintf(s.out, "error: %v\n", err)
		}
	}
}

// runScript is strict: any unexpected outcome aborts with its line number.
// The -/! prefixes are the tolerance mechanism; an unprefixed failure in a
// script is a bug in the script or the editor, not something to shrug past.
func runScript(s *Session, in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	ln := 0
	for sc.Scan() {
		ln++
		err := Execute(s, sc.Text())
		if errors.Is(err, errQuit) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", ln, err)
		}
	}
	return sc.Err()
}
