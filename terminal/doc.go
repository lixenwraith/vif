// @focus: #sys { term }
// Package terminal provides direct ANSI terminal control with zero-alloc rendering.
//
// Features:
//   - True color (24-bit) and 256-color palette support
//   - Double-buffered output with cell-level diffing
//   - Raw stdin input parsing with escape sequence handling
//   - SIGWINCH resize detection
//   - Clean terminal restoration on exit/panic
//
// This package bypasses terminfo/termcap entirely, emitting direct ANSI sequences.
// Target environments: Linux, macOS, BSDs with xterm-compatible terminals.
package terminal
