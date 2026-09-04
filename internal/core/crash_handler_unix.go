//go:build unix

package core

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/lixenwraith/terminal"
)

// HandleCrash is the unified panic handler that resets the terminal and prints the stack trace
func HandleCrash(r any) {
	if r == nil {
		return
	}

	stack := debug.Stack()

	// Observer runs first: it may need to flush before teardown
	if crashHook != nil {
		crashHook(r, stack)
	}

	// Terminal cleanup if available
	if crashTerminal != nil {
		crashTerminal.Fini()
	} else if StdoutIsTerminal() {
		// Fallback for a run that put a terminal into raw mode without registering
		// one here. Gated on stdout actually being a terminal: a headless run —
		// a dedicated host, a script, a container — has none to reset, and the
		// escape sequence would be the only thing it ever wrote to its output.
		terminal.EmergencyReset(os.Stdout)
	}

	// Dev capture owns fd 2; give it back so the banner reaches the terminal
	RestoreStderr()

	fmt.Fprintf(os.Stderr, "\n\x1b[31mCRASH DETECTED: %v\x1b[0m\n", r)
	fmt.Fprintf(os.Stderr, "Stack Trace:\n%s\n", stack)
	if p := StderrCapturePath(); p != "" {
		fmt.Fprintf(os.Stderr, "captured stderr: %s\n", p)
	}

	os.Exit(1)
}
