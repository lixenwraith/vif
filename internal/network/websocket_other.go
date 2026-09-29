//go:build !js || !wasm

package network

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/websocket"
)

// writeLinger is how long a link's writer gathers a burst before one flush.
const writeLinger = parameter.NetworkWriteLinger

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

// sessionLink is the front door's link: a native client is TCP end to end.
func sessionLink(joinTarget, _ string) string { return joinTarget }

// wsListener answers ws:// upgrades on a TCP port and yields each as a stream, so
// the accept loop's handshake budget and per-address admission see a WebSocket
// joiner as they see a TCP one. The HTTP request before it is bounded in time and
// size; Origin is not checked, since a session's join handshake is its admission.
type wsListener struct {
	tcp    net.Listener
	srv    *http.Server
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

// listenWebSocket binds target, a ws:// URL that Listenable accepted.
func listenWebSocket(target string, timeout time.Duration) (net.Listener, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp", u.Host)
	if err != nil {
		return nil, err
	}
	l := &wsListener{tcp: tcp, conns: make(chan net.Conn), closed: make(chan struct{})}
	l.srv = &http.Server{
		Handler:           http.HandlerFunc(l.upgrade),
		ReadHeaderTimeout: cmp.Or(timeout, DefaultConfig().ConnectTimeout),
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	l.srv.SetKeepAlivesEnabled(false)
	go func() { _ = l.srv.Serve(tcp) }()
	return l, nil
}

func (l *wsListener) upgrade(w http.ResponseWriter, r *http.Request) {
	h, err := websocket.Check(w, r)
	if err != nil {
		status := http.StatusBadRequest
		if refused := (*websocket.RequestError)(nil); errors.As(err, &refused) {
			status = refused.Status
		}
		http.Error(w, err.Error(), status)
		return
	}
	conn, err := h.Upgrade(w)
	if err != nil {
		return
	}
	select {
	case l.conns <- conn:
	case <-l.closed:
		_ = conn.Close()
	}
}

func (l *wsListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *wsListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	err := l.srv.Close()
	_ = l.tcp.Close() // Serve may not have tracked it yet
	return err
}

func (l *wsListener) Addr() net.Addr { return wsAddr("ws://" + l.tcp.Addr().String()) }
