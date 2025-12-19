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
	terminal.EmergencyReset(os.Stdout)

	// TODO: Though cleaner now, still zig-zags in stack trace print, add more '\r\n' and sync/flushes to test
	// Force flush stdout/stderr before printing to stderr
	os.Stdout.Sync()
	os.Stderr.Sync()

	// Print error and stack trace to stderr
	fmt.Fprintf(os.Stderr, "\r\n\x1b[31mCRASH DETECTED: %v\x1b[0m\r\n", r)
	fmt.Fprintf(os.Stderr, "Stack Trace:\r\n%s\r\n", debug.Stack())

	// Sync stderr before exit
	os.Stderr.Sync()

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