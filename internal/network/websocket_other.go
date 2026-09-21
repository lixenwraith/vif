//go:build !js || !wasm

package network

import (
	"fmt"
	"net"
	"time"
)

// dialWebSocket exists so the join path has one shape everywhere. Only a browser
// holds a WebSocket implementation this repository did not have to carry; a native
// client dials the session's own TCP port instead.
func dialWebSocket(target string, _ time.Duration) (net.Conn, error) {
	return nil, fmt.Errorf("join %s: a WebSocket session route is joinable only from a browser build", target)
}
