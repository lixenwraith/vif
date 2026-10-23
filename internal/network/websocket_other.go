//go:build !js || !wasm

package network

import (
	"cmp"
	"context"
	"crypto/tls"
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
// message is reported as a join reply's reason is. A URL with no host is this
// machine, as a host-less host:port is to TCP.
func dialWebSocket(target string, timeout time.Duration, tlsCfg *tls.Config) (net.Conn, error) {
	if u, err := url.Parse(target); err == nil && u.Hostname() == "" {
		u.Host = net.JoinHostPort("localhost", u.Port())
		target = u.String()
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	conn, err := websocket.Dial(ctx, target, tlsCfg)
	if err == nil {
		return conn, nil
	}
	var refused *websocket.HandshakeError
	var body struct {
		Error struct{ Message string } `json:"error"`
	}
	if errors.As(err, &refused) && json.Unmarshal(refused.Body, &body) == nil && body.Error.Message != "" {
		return nil, plainError{body.Error.Message, err}
	}
	return nil, err
}

// routeAnswered reports whether a failed dial reached a route that refused it, with
// a status and usually a reason: that answer stands rather than sending the link to
// its site.
func routeAnswered(err error) bool {
	var refused *websocket.HandshakeError
	return errors.As(err, &refused)
}

// buildDialsTCP is whether this build has a socket to dial a host:port with.
const buildDialsTCP = true

// wsListener answers ws:// upgrades on a TCP port, under TLS for wss://, and yields
// each as a stream, so the accept loop's handshake budget and per-address admission
// see a WebSocket joiner as they see a TCP one. The HTTP request before it is
// bounded in time and size, the TLS handshake with it; Origin is not checked, since
// a session's join handshake is its admission.
type wsListener struct {
	tcp    net.Listener
	scheme string
	srv    *http.Server
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

// listenWebSocket binds target, a ws:// or wss:// URL that Listenable accepted; the
// latter serves tlsCfg.
func listenWebSocket(target string, timeout time.Duration, tlsCfg *tls.Config) (net.Listener, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp", u.Host)
	if err != nil {
		return nil, err
	}
	l := &wsListener{tcp: tcp, scheme: u.Scheme, conns: make(chan net.Conn), closed: make(chan struct{})}
	l.srv = &http.Server{
		Handler:           http.HandlerFunc(l.upgrade),
		ReadHeaderTimeout: cmp.Or(timeout, DefaultConfig().ConnectTimeout),
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	l.srv.SetKeepAlivesEnabled(false)
	serving := tcp
	if u.Scheme == "wss" {
		serving = tls.NewListener(tcp, tlsCfg)
	}
	go func() { _ = l.srv.Serve(serving) }()
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

func (l *wsListener) Addr() net.Addr { return wsAddr(l.scheme + "://" + l.tcp.Addr().String()) }
