//go:build wasm

package core

import (
	"fmt"
	"runtime/debug"
	"syscall/js"
)

// HandleCrash logs to browser console (no os.Exit in WASM)
func HandleCrash(r any) {
	if r == nil {
		return
	}

	// Clean up xterm.js state if terminal registered
	if crashTerminal != nil {
		crashTerminal.Fini()
	}

	console := js.Global().Get("console")
	console.Call("error", fmt.Sprintf("CRASH: %v", r))
	console.Call("error", fmt.Sprintf("Stack:\n%s", debug.Stack()))

	// Re-panic to halt goroutine; browser dev tools show error
	panic(r)
}