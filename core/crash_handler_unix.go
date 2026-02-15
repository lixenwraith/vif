//go:build unix

package core

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// HandleCrash is the unified panic handler that resets the terminal and prints the stack trace
func HandleCrash(r any) {
	if r == nil {
		return
	}

	// Terminal cleanup if available
	if crashTerminal != nil {
		crashTerminal.Fini()
	} else {
		// Fallback for edge cases
		terminal.EmergencyReset(os.Stdout)
	}

	fmt.Fprintf(os.Stderr, "\n\x1b[31mCRASH DETECTED: %v\x1b[0m\n", r)
	fmt.Fprintf(os.Stderr, "Stack Trace:\n%s\n", debug.Stack())

	os.Exit(1)
}