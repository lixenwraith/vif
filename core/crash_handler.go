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

	// Restore terminal to sane state immediately
	// using os.Stdout directly as terminal package acts independently
	terminal.EmergencyReset(os.Stdout)

	// Print error and stack trace to stderr so it's visible after reset
	// Use \r\n for raw mode compatibility to avoid zig-zag output if reset failed partially
	fmt.Fprintf(os.Stderr, "\r\n\x1b[31mCRASH DETECTED: %v\x1b[0m\r\n", r)
	fmt.Fprintf(os.Stderr, "Stack Trace:\r\n%s\r\n", debug.Stack())
	os.Exit(1)
}

// Go runs a function in a new goroutine with panic recovery.
// Use this instead of the 'go' keyword to ensure terminal cleanup on crash.
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				HandleCrash(r)
			}
		}()
		fn()
	}()
}