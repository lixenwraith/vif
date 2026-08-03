//go:build !unix && !wasm

package core

import (
	"fmt"
	"os"
	"runtime/debug"
)

// HandleCrash is the fallback panic handler for platforms with no dedicated
// terminal reset path
func HandleCrash(r any) {
	if r == nil {
		return
	}

	stack := debug.Stack()

	if crashHook != nil {
		crashHook(r, stack)
	}
	if crashTerminal != nil {
		crashTerminal.Fini()
	}

	// Dev capture owns fd 2; give it back so the banner reaches the terminal
	RestoreStderr()

	fmt.Fprintf(os.Stderr, "\nCRASH DETECTED: %v\n", r)
	fmt.Fprintf(os.Stderr, "Stack Trace:\n%s\n", stack)

	os.Exit(1)
}
