//go:build unix && !linux

package core

import "syscall"

// dupToStderr points fd 2 at fd
func dupToStderr(fd int) error { return syscall.Dup2(fd, 2) }

// dupFD duplicates fd onto the lowest free descriptor
func dupFD(fd int) (int, error) { return syscall.Dup(fd) }
