//go:build unix

package app

import (
	"os"
	"os/signal"
	"syscall"
)

// notifySignals delivers termination signals for graceful shutdown.
// SIGINT is included despite Ctrl+C reaching the input machine: it still
// arrives outside raw mode, during startup and teardown.
func notifySignals() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return ch, func() { signal.Stop(ch) }
}
