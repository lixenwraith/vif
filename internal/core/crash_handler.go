package core

var (
	crashTerminal interface{ Fini() }
	crashHook     func(r any, stack []byte)
)

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
