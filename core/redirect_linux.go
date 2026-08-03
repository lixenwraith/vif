//go:build linux

package core

import "syscall"

// dupToStderr points fd 2 at fd; Dup2 is absent on newer linux arches
func dupToStderr(fd int) error { return syscall.Dup3(fd, 2, 0) }

// dupFD duplicates fd onto the lowest free descriptor
func dupFD(fd int) (int, error) { return syscall.Dup(fd) }
