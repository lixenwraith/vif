//go:build !windows

package network

import "syscall"

// sysErrno is each failure plainNetError names, as this platform reports it.
var sysErrno = struct{ addrInUse, addrNotAvail, connRefused, unreachable []error }{
	addrInUse:    []error{syscall.EADDRINUSE},
	addrNotAvail: []error{syscall.EADDRNOTAVAIL},
	connRefused:  []error{syscall.ECONNREFUSED},
	unreachable:  []error{syscall.EHOSTUNREACH, syscall.ENETUNREACH},
}
