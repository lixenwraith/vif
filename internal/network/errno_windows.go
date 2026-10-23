package network

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// sysErrno is each failure plainNetError names, as Winsock reports it beside the
// portable code, which errors.Is does not fold it into.
var sysErrno = struct{ addrInUse, addrNotAvail, connRefused, unreachable []error }{
	addrInUse:    []error{syscall.EADDRINUSE, windows.WSAEADDRINUSE},
	addrNotAvail: []error{syscall.EADDRNOTAVAIL, windows.WSAEADDRNOTAVAIL},
	connRefused:  []error{syscall.ECONNREFUSED, windows.WSAECONNREFUSED},
	unreachable:  []error{syscall.EHOSTUNREACH, syscall.ENETUNREACH, windows.WSAEHOSTUNREACH, windows.WSAENETUNREACH},
}
