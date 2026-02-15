package core

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