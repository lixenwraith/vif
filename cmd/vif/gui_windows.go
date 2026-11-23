//go:build windows && vif_gui && !vif_headless && !vif_nogui

package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// A vif_gui build for Windows is linked with no console (-H=windowsgui), so it has
// no terminal to play in. Started from a console it takes that one and prints
// there; started from a launcher, which has none, it says why it ended in a box.
func init() {
	windowOnly = true
	flag.CommandLine.Init(os.Args[0], flag.ContinueOnError) // a refused flag is complain's to say
	flag.CommandLine.SetOutput(io.Discard)

	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if ok, _, _ := attach.Call(uintptr(^uint32(0))); ok != 0 { // ATTACH_PARENT_PROCESS
		for _, std := range []**os.File{&os.Stdout, &os.Stderr} {
			if kind, _ := windows.GetFileType(windows.Handle((*std).Fd())); kind != windows.FILE_TYPE_UNKNOWN {
				continue // directed to a file or a pipe, which keeps it
			}
			if console, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
				*std = console
			}
		}
		return
	}
	complain = func(msg string) {
		fmt.Fprintln(os.Stderr, msg) // for a launcher that reads it
		text, _ := windows.UTF16PtrFromString(msg)
		title, _ := windows.UTF16PtrFromString("vif")
		windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
	}
}
