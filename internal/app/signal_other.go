//go:build !unix

package app

import "os"

// notifySignals is inert where signal delivery is unavailable or meaningless;
// a nil channel never fires in select
func notifySignals() (<-chan os.Signal, func()) {
	return nil, func() {}
}
