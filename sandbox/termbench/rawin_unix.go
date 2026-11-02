//go:build !windows

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// consoleModes has nothing to report off Windows
func consoleModes() string { return "" }

// runRawIn prints each read from the terminal as hex and as text, with the mouse
// reports asked for, so what reaches a program can be told from what it parses
func runRawIn(args []string) error {
	fs := flag.NewFlagSet("rawin", flag.ExitOnError)
	mouse := fs.Bool("mouse", true, "ask for click, drag, motion and SGR mouse reports")
	dur := fs.Duration("dur", 30*time.Second, "how long to read; a lone q ends it sooner")
	fs.Parse(args)

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, old)
	if *mouse {
		os.Stdout.WriteString("\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h")
		defer os.Stdout.WriteString("\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?1006l")
	}
	fmt.Printf("TERM=%q COLORTERM=%q; reading %v, q quits\r\n", os.Getenv("TERM"), os.Getenv("COLORTERM"), *dur)

	buf := make([]byte, 512)
	start := time.Now()
	data := make(chan []byte)
	go func() {
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				close(data)
				return
			}
			data <- append([]byte(nil), buf[:n]...)
		}
	}()
	for {
		select {
		case b, ok := <-data:
			if !ok {
				return nil
			}
			fmt.Printf("%8.3fs %3dB % x  %q\r\n", time.Since(start).Seconds(), len(b), b, b)
			if len(b) == 1 && b[0] == 'q' {
				return nil
			}
		case <-time.After(time.Until(start.Add(*dur))):
			return nil
		}
	}
}
