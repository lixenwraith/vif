//go:build windows

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procReadConsoleInput = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")

// inputRecord is INPUT_RECORD: the event type, then its 16-byte union
type inputRecord struct {
	kind uint16
	_    uint16
	data [16]byte
}

type flagName struct {
	bit  uint32
	name string
}

var inFlags = []flagName{
	{windows.ENABLE_PROCESSED_INPUT, "PROCESSED"}, {windows.ENABLE_LINE_INPUT, "LINE"},
	{windows.ENABLE_ECHO_INPUT, "ECHO"}, {windows.ENABLE_WINDOW_INPUT, "WINDOW"},
	{windows.ENABLE_MOUSE_INPUT, "MOUSE"}, {windows.ENABLE_INSERT_MODE, "INSERT"},
	{windows.ENABLE_QUICK_EDIT_MODE, "QUICK_EDIT"}, {windows.ENABLE_EXTENDED_FLAGS, "EXTENDED"},
	{windows.ENABLE_AUTO_POSITION, "AUTO_POSITION"}, {windows.ENABLE_VIRTUAL_TERMINAL_INPUT, "VT_INPUT"},
}

var outFlags = []flagName{
	{windows.ENABLE_PROCESSED_OUTPUT, "PROCESSED"}, {windows.ENABLE_WRAP_AT_EOL_OUTPUT, "WRAP_AT_EOL"},
	{windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING, "VT"}, {windows.DISABLE_NEWLINE_AUTO_RETURN, "NO_AUTO_RETURN"},
	{windows.ENABLE_LVB_GRID_WORLDWIDE, "LVB_GRID"},
}

func modeString(m uint32, names []flagName) string {
	var s []string
	for _, f := range names {
		if m&f.bit != 0 {
			s = append(s, f.name)
		}
	}
	return fmt.Sprintf("%#x[%s]", m, strings.Join(s, "|"))
}

// consoleModes reports the console's modes as the library left them, the Windows
// build and what the environment says the host is
func consoleModes() string {
	var in, out uint32
	windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &in)
	windows.GetConsoleMode(windows.Handle(os.Stdout.Fd()), &out)
	v := windows.RtlGetVersion()
	var env []string
	for _, k := range []string{"TERM", "COLORTERM", "WT_SESSION", "ALACRITTY_WINDOW_ID", "ALACRITTY_LOG", "TERM_PROGRAM"} {
		if val := os.Getenv(k); val != "" {
			env = append(env, k+"="+val)
		}
	}
	return fmt.Sprintf("console in=%s out=%s; Windows %d.%d.%d; %s",
		modeString(in, inFlags), modeString(out, outFlags), v.MajorVersion, v.MinorVersion, v.BuildNumber, strings.Join(env, " "))
}

// runRawIn prints every console input record as it arrives, in the input mode the
// flags choose, so a mouse report the host passes as VT text, as a MOUSE record or
// not at all can be told apart
func runRawIn(args []string) error {
	fs := flag.NewFlagSet("rawin", flag.ExitOnError)
	vt := fs.Bool("vt", true, "ENABLE_VIRTUAL_TERMINAL_INPUT, as the terminal module and tcell set")
	mouseInput := fs.Bool("mouseinput", false, "ENABLE_MOUSE_INPUT, as the terminal module v0.6.1 sets")
	mouse := fs.Bool("mouse", true, "ask for click, drag, motion and SGR mouse reports (DECSET 1000/1002/1003/1006)")
	win32 := fs.Bool("win32", false, "ask for win32-input-mode (DECSET 9001), as tcell does on Windows")
	dur := fs.Duration("dur", 30*time.Second, "how long to read; q ends it sooner")
	fs.Parse(args)

	in, out := windows.Handle(os.Stdin.Fd()), windows.Handle(os.Stdout.Fd())
	var oldIn, oldOut uint32
	if err := windows.GetConsoleMode(in, &oldIn); err != nil {
		return fmt.Errorf("stdin is not a console: %w", err)
	}
	windows.GetConsoleMode(out, &oldOut)
	fmt.Printf("before: %s\r\n", consoleModes())

	mode := uint32(windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_WINDOW_INPUT)
	if *vt {
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	}
	if *mouseInput {
		mode |= windows.ENABLE_MOUSE_INPUT
	}
	if err := windows.SetConsoleMode(in, mode); err != nil {
		return fmt.Errorf("SetConsoleMode stdin: %w", err)
	}
	defer windows.SetConsoleMode(in, oldIn)
	windows.SetConsoleMode(out, windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
	defer windows.SetConsoleMode(out, oldOut)

	var on, off string
	if *mouse {
		on += "\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h"
		off += "\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?1006l"
	}
	if *win32 {
		on += "\x1b[?9001h"
		off += "\x1b[?9001l"
	}
	os.Stdout.WriteString(on)
	defer os.Stdout.WriteString(off)
	fmt.Printf("after:  %s\r\nreading %v, q quits\r\n", consoleModes(), *dur)

	var recs [64]inputRecord
	start := time.Now()
	for time.Since(start) < *dur {
		ev, err := windows.WaitForSingleObject(in, 100)
		if err != nil {
			return err
		}
		if ev == uint32(windows.WAIT_TIMEOUT) {
			continue
		}
		var n uint32
		if r, _, err := procReadConsoleInput.Call(uintptr(in), uintptr(unsafe.Pointer(&recs[0])), uintptr(len(recs)), uintptr(unsafe.Pointer(&n))); r == 0 {
			return fmt.Errorf("ReadConsoleInputW: %w", err)
		}
		at := time.Since(start).Seconds()
		for _, rec := range recs[:n] {
			line, quit := describe(rec)
			fmt.Printf("%8.3fs %s\r\n", at, line)
			if quit {
				return nil
			}
		}
	}
	return nil
}

func describe(r inputRecord) (string, bool) {
	d := r.data[:]
	u16 := func(o int) uint16 { return binary.LittleEndian.Uint16(d[o:]) }
	u32 := func(o int) uint32 { return binary.LittleEndian.Uint32(d[o:]) }
	switch r.kind {
	case windows.KEY_EVENT:
		down, ch := u32(0) != 0, rune(u16(10))
		return fmt.Sprintf("KEY   down=%-5t rep=%d vk=%#02x sc=%#02x char=%#04x %q ctrl=%#x",
			down, u16(4), u16(6), u16(8), ch, ch, u32(12)), down && ch == 'q'
	case windows.MOUSE_EVENT:
		return fmt.Sprintf("MOUSE x=%d y=%d buttons=%#x ctrl=%#x flags=%#x",
			int16(u16(0)), int16(u16(2)), u32(4), u32(8), u32(12)), false
	case windows.WINDOW_BUFFER_SIZE_EVENT:
		return fmt.Sprintf("SIZE  %dx%d", int16(u16(0)), int16(u16(2))), false
	case windows.FOCUS_EVENT:
		return fmt.Sprintf("FOCUS set=%t", u32(0) != 0), false
	case windows.MENU_EVENT:
		return fmt.Sprintf("MENU  id=%d", u32(0)), false
	}
	return fmt.Sprintf("type %#x", r.kind), false
}
