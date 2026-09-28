//go:build !js || !wasm

package network

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/lixenwraith/vif/pkg/websocket"
)

// dialWebSocket opens a session route natively, for a network that blocks the TCP
// port. A refusal before the upgrade carries the allocator's JSON error, and its
// message is reported as a join reply's reason is.
func dialWebSocket(target string, timeout time.Duration) (net.Conn, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	conn, err := websocket.Dial(ctx, target)
	if err == nil {
		return conn, nil
	}
	var refused *websocket.HandshakeError
	var body struct {
		Error struct{ Message string } `json:"error"`
	}
	if errors.As(err, &refused) && json.Unmarshal(refused.Body, &body) == nil && body.Error.Message != "" {
		return nil, errors.New(body.Error.Message)
	}
	return nil, err
}
