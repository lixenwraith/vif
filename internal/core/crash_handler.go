package core

import "os"

var (
	crashTerminal interface{ Fini() }
	crashHook     func(r any, stack []byte)
)

// StdoutIsTerminal reports whether standard output is a terminal device.
//
// It is what separates a run that has a terminal to restore from one whose output
// is a pipe, a file or a container's log: an escape sequence written to the latter
// is not a reset, it is the only thing that run ever emitted. Stat rather than an
// ioctl, because the question is about the file rather than about its modes and
// the answer must hold on every unix this builds for.
func StdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// SetCrashTerminal registers terminal for crash cleanup
func SetCrashTerminal(t interface{ Fini() }) {
	crashTerminal = t
}

// SetCrashHook registers a pre-teardown observer
// invoked with the panic value and stack before the terminal is restored
func SetCrashHook(fn func(r any, stack []byte)) {
	crashHook = fn
}

// Go to be used instead of 'go' to run a function in a new goroutine with panic recovery, to cleanup terminal on crash
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
