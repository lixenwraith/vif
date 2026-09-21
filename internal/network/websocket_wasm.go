//go:build js && wasm

package network

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall/js"
	"time"
)

// wsQueueMessages bounds the arrived-but-unread messages one link holds. A browser
// receiver has no backpressure to offer, so this is where an authority that outruns
// this instance is refused rather than left growing the tab's heap.
const wsQueueMessages = 1024

var errWSOverrun = errors.New("websocket: inbound queue overrun")

// wsAddr names the endpoint wherever a peer address is logged or shown.
type wsAddr string

func (a wsAddr) Network() string { return "websocket" }
func (a wsAddr) String() string  { return string(a) }

// wsConn is a byte stream over one browser WebSocket. Message boundaries are not
// protocol boundaries: Decode consumes the concatenation exactly as it does from a
// socket, which is what lets SocketPort serve a browser guest unchanged.
type wsConn struct {
	ws       js.Value
	url      string
	incoming chan []byte
	rest     []byte

	mu           sync.Mutex
	err          error
	readDeadline time.Time

	handlers    []js.Func
	closed      chan struct{}
	closeOnce   sync.Once
	releaseOnce sync.Once
}

// dialWebSocket opens the session route and returns it once the browser reports
// the connection open, so a caller holds a stream it can write to rather than one
// that may still fail its handshake.
func dialWebSocket(target string, timeout time.Duration) (conn net.Conn, err error) {
	ctor := js.Global().Get("WebSocket")
	if !ctor.Truthy() {
		return nil, errors.New("websocket: this browser exposes no WebSocket")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("websocket: %s refused by the browser: %v", target, r)
		}
	}()

	c := &wsConn{
		ws:       ctor.New(target),
		url:      target,
		incoming: make(chan []byte, wsQueueMessages),
		closed:   make(chan struct{}),
	}
	c.ws.Set("binaryType", "arraybuffer")

	open := make(chan struct{})
	var openOnce sync.Once
	c.on("open", func(js.Value) { openOnce.Do(func() { close(open) }) })
	c.on("message", c.receive)
	c.on("error", func(js.Value) { c.fail(errors.New("websocket: the browser reported a connection error")) })
	c.on("close", func(event js.Value) {
		c.fail(fmt.Errorf("websocket: closed with code %d: %w", event.Get("code").Int(), io.EOF))
	})

	var expiry <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		expiry = timer.C
	}
	select {
	case <-open:
		return c, nil
	case <-c.closed:
		_ = c.Close()
		return nil, c.failure()
	case <-expiry:
		_ = c.Close()
		return nil, fmt.Errorf("websocket: %s did not open within %s", target, timeout)
	}
}

// on registers one retained listener; every one of them is released by Close,
// because a js.Func the browser still holds keeps this instance alive after the
// game has finished with it.
func (c *wsConn) on(event string, handle func(js.Value)) {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			handle(args[0])
		}
		return nil
	})
	c.handlers = append(c.handlers, fn)
	c.ws.Call("addEventListener", event, fn)
}

func (c *wsConn) receive(event js.Value) {
	data := event.Get("data")
	if data.Type() != js.TypeObject {
		c.fail(errors.New("websocket: the session sent a text frame"))
		return
	}
	view := js.Global().Get("Uint8Array").New(data)
	payload := make([]byte, view.Get("length").Int())
	js.CopyBytesToGo(payload, view)
	select {
	case c.incoming <- payload:
	default:
		c.fail(errWSOverrun)
	}
}

// fail records the first cause and releases every reader; later causes are the
// consequences of it and would only rename the failure.
func (c *wsConn) fail(cause error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = cause
	}
	c.mu.Unlock()
	c.closeOnce.Do(func() { close(c.closed) })
}

func (c *wsConn) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		return net.ErrClosed
	}
	return c.err
}

func (c *wsConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.rest) == 0 {
		// Queued bytes before the closure that follows them: a session that ended
		// mid-frame is a decode error, not a truncated one this side invented.
		select {
		case data := <-c.incoming:
			c.rest = data
		default:
			data, err := c.await()
			if err != nil {
				return 0, err
			}
			c.rest = data
		}
	}
	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}

func (c *wsConn) await() ([]byte, error) {
	c.mu.Lock()
	deadline := c.readDeadline
	c.mu.Unlock()

	var expiry <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expiry = timer.C
	}
	select {
	case data := <-c.incoming:
		return data, nil
	case <-c.closed:
		return nil, c.failure()
	case <-expiry:
		return nil, os.ErrDeadlineExceeded
	}
}

// Write hands one whole encoded frame to the browser, which owns the send buffer
// and the flush. A deadline has nothing to expire here, so none is enforced.
func (c *wsConn) Write(p []byte) (n int, err error) {
	select {
	case <-c.closed:
		return 0, c.failure()
	default:
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("websocket: send refused: %v", r)
			n = 0
			c.fail(err)
		}
	}()
	buffer := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(buffer, p)
	c.ws.Call("send", buffer)
	return len(p), nil
}

func (c *wsConn) Close() error {
	c.fail(net.ErrClosed)
	c.releaseOnce.Do(func() {
		func() {
			defer func() { _ = recover() }() // a socket the browser already closed
			c.ws.Call("close")
		}()
		for _, fn := range c.handlers {
			fn.Release()
		}
		c.handlers = nil
	})
	return nil
}

func (c *wsConn) LocalAddr() net.Addr  { return wsAddr("browser") }
func (c *wsConn) RemoteAddr() net.Addr { return wsAddr(c.url) }

// Only the read half has anything to expire; see Write.
func (c *wsConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

func (c *wsConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return nil
}

func (c *wsConn) SetWriteDeadline(time.Time) error { return nil }
