// Package websocket is RFC 6455 as a byte stream: the version-13 upgrade as client
// and server, binary data, ping, pong and close. Read returns payload bytes across
// frames and messages and never holds a whole message; each Write is one binary
// frame. Text, extensions and subprotocols are refused, and every socket this
// package owns sets TCP_NODELAY.
package websocket

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	closeNormal      = 1000
	closeProtocol    = 1002
	closeUnsupported = 1003
	closeInvalid     = 1007

	maxControl = 125
	// closeWait bounds each half of the close handshake, so a peer that never
	// answers costs its closer this long and no more.
	closeWait = 2 * time.Second
)

var normalClose = binary.BigEndian.AppendUint16(nil, closeNormal)

// Conn is one WebSocket as a net.Conn. One goroutine reads while any number write;
// data frames, pong replies and the close frame share one write lock.
type Conn struct {
	conn   net.Conn
	br     *bufio.Reader // holds whatever the handshake read past its end
	client bool          // a client masks what it writes and refuses masked frames

	rmu       sync.Mutex
	remaining uint64 // payload left in the current data frame
	key       [4]byte
	keyAt     int
	masked    bool
	open      bool // a data message awaits its continuation
	readErr   error
	readDone  chan struct{} // closed when readErr is set

	wmu       sync.Mutex
	wbuf      []byte
	wkey      [4]byte
	werr      error
	closeSent bool
	pong      atomic.Pointer[[]byte] // the one reply a ping left for the write lock

	closeOnce sync.Once
	closeErr  error
}

func newConn(conn net.Conn, br *bufio.Reader, client bool) *Conn {
	return &Conn{conn: conn, br: br, client: client, readDone: make(chan struct{})}
}

// Read returns binary payload as it arrives. The peer's close, or its end of the
// socket between frames, is io.EOF; any error, a deadline's included, ends the read
// side, because a frame cut short leaves nowhere to resume from.
func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	return c.read(p)
}

// read is Read under rmu.
func (c *Conn) read(p []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	if len(p) == 0 {
		return 0, nil
	}
	for c.remaining == 0 {
		if err := c.nextFrame(); err != nil {
			return 0, c.endRead(err)
		}
	}
	if uint64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.br.Read(p)
	c.remaining -= uint64(n)
	if c.masked {
		c.keyAt = maskBytes(c.key, c.keyAt, p[:n])
	}
	if err != nil {
		return n, c.endRead(noEOF(err))
	}
	return n, nil
}

func (c *Conn) endRead(err error) error {
	c.readErr = err
	close(c.readDone)
	return err
}

// nextFrame reads headers until one opens data for read, answering the control
// frames it passes. Nothing is buffered on a length's say-so: data is read as the
// caller asks for it, and a control payload is at most 125 bytes.
func (c *Conn) nextFrame() error {
	var h [8]byte
	if _, err := io.ReadFull(c.br, h[:2]); err != nil {
		return err
	}
	fin, op, masked := h[0]&0x80 != 0, h[0]&0x0f, h[1]&0x80 != 0
	if h[0]&0x70 != 0 {
		return c.fail(closeProtocol, "reserved bits set")
	}
	if masked == c.client {
		return c.fail(closeProtocol, "a frame masked contrary to its sender's role")
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		if _, err := io.ReadFull(c.br, h[:2]); err != nil {
			return noEOF(err)
		}
		n = uint64(binary.BigEndian.Uint16(h[:2]))
	case 127:
		if _, err := io.ReadFull(c.br, h[:8]); err != nil {
			return noEOF(err)
		}
		if n = binary.BigEndian.Uint64(h[:8]); n>>63 != 0 {
			return c.fail(closeProtocol, "a length with its top bit set")
		}
	}
	if c.masked, c.keyAt = masked, 0; masked {
		if _, err := io.ReadFull(c.br, c.key[:]); err != nil {
			return noEOF(err)
		}
	}
	if op&0x8 != 0 {
		return c.control(fin, op, n)
	}
	switch {
	case op == opText:
		return c.fail(closeUnsupported, "a text frame")
	case op != opBinary && op != opContinuation:
		return c.fail(closeProtocol, fmt.Sprintf("unknown opcode %#x", op))
	case (op == opBinary) == c.open:
		return c.fail(closeProtocol, "a data frame out of its message's order")
	}
	c.open, c.remaining = !fin, n
	return nil
}

// control answers one control frame. A ping's reply waits for the write lock rather
// than for a writer, and a newer ping replaces it, so a flood grows nothing.
func (c *Conn) control(fin bool, op byte, n uint64) error {
	if !fin || n > maxControl {
		return c.fail(closeProtocol, "a control frame fragmented or over 125 bytes")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return noEOF(err)
	}
	if c.masked {
		maskBytes(c.key, 0, payload)
	}
	switch op {
	case opPing:
		c.pong.Store(&payload)
		if c.wmu.TryLock() {
			c.unlockWrite()
		}
		return nil
	case opPong:
		return nil
	case opClose:
		return c.receiveClose(payload)
	}
	return c.fail(closeProtocol, fmt.Sprintf("unknown opcode %#x", op))
}

// receiveClose echoes the peer's close code and ends the stream.
func (c *Conn) receiveClose(payload []byte) error {
	switch {
	case len(payload) == 1:
		return c.fail(closeProtocol, "a one-byte close payload")
	case len(payload) >= 2 && !validCloseCode(binary.BigEndian.Uint16(payload)):
		return c.fail(closeProtocol, "an invalid close code")
	case len(payload) >= 2 && !utf8.Valid(payload[2:]):
		return c.fail(closeInvalid, "a close reason that is not UTF-8")
	}
	_ = c.sendClose(payload[:min(len(payload), 2)], time.Now().Add(closeWait))
	return io.EOF
}

// fail refuses the stream with code; the caller's Close then ends the socket
// without waiting for a reply.
func (c *Conn) fail(code uint16, reason string) error {
	_ = c.sendClose(binary.BigEndian.AppendUint16(nil, code), time.Now().Add(closeWait))
	return fmt.Errorf("websocket: %s (close %d)", reason, code)
}

func validCloseCode(code uint16) bool {
	switch {
	case code >= 3000 && code <= 4999:
		return true
	case code < 1000 || code > 1014:
		return false
	}
	return code != 1004 && code != 1005 && code != 1006
}

// Write sends p as one binary frame, so a receiver's per-message buffer is bounded
// by the writer's batch.
func (c *Conn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.wmu.Lock()
	defer c.unlockWrite()
	if c.closeSent {
		return 0, net.ErrClosed
	}
	if err := c.writeFrame(opBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// writeFrame sends one final frame in one socket write. Caller MUST hold wmu. A
// failed write leaves the stream mid-frame, so its error answers every later one.
func (c *Conn) writeFrame(op byte, p []byte) error {
	if c.werr != nil {
		return c.werr
	}
	var key *[4]byte
	if c.client {
		_, _ = rand.Read(c.wkey[:]) // never fails
		key = &c.wkey
	}
	c.wbuf = appendFrame(c.wbuf[:0], op, p, key)
	if _, err := c.conn.Write(c.wbuf); err != nil {
		c.werr = err
	}
	return c.werr
}

// unlockWrite releases wmu after sending any pong a ping left meanwhile. A ping
// landing between that send and the release is sent by whichever of its reader and
// this writer takes the lock next.
func (c *Conn) unlockWrite() {
	for {
		if p := c.pong.Swap(nil); p != nil && !c.closeSent {
			_ = c.writeFrame(opPong, *p)
		}
		c.wmu.Unlock()
		if c.pong.Load() == nil || !c.wmu.TryLock() {
			return
		}
	}
}

// sendClose writes the close frame once, under its own deadline: nothing is written
// after it, so no caller's deadline is left to keep.
func (c *Conn) sendClose(payload []byte, deadline time.Time) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closeSent {
		return nil
	}
	c.closeSent = true
	_ = c.conn.SetWriteDeadline(deadline)
	return c.writeFrame(opClose, payload)
}

// CloseWrite sends the close frame, so the peer reads the stream's end while this
// side still reads its reply.
func (c *Conn) CloseWrite() error {
	return c.sendClose(normalClose, time.Now().Add(closeWait))
}

// Close sends the close frame, waits up to closeWait for the read side to end, then
// closes the socket. The deadline it sets first releases a Read or Write in progress.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		deadline := time.Now().Add(closeWait)
		_ = c.conn.SetDeadline(deadline)
		_ = c.sendClose(normalClose, deadline)
		c.awaitClose(deadline)
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}

// awaitClose waits for the peer's close: through the Read in progress if there is
// one, else by reading and discarding until it arrives.
func (c *Conn) awaitClose(deadline time.Time) {
	if c.rmu.TryLock() {
		defer c.rmu.Unlock()
		var discard [512]byte
		for c.readErr == nil {
			_, _ = c.read(discard[:])
		}
		return
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-c.readDone:
	case <-timer.C:
	}
}

func (c *Conn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *Conn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// appendFrame appends one final frame carrying p, masked with key when there is one.
func appendFrame(b []byte, op byte, p []byte, key *[4]byte) []byte {
	var mask byte
	if key != nil {
		mask = 0x80
	}
	b = append(b, 0x80|op)
	switch n := len(p); {
	case n <= maxControl:
		b = append(b, mask|byte(n))
	case n <= 0xffff:
		b = binary.BigEndian.AppendUint16(append(b, mask|126), uint16(n))
	default:
		b = binary.BigEndian.AppendUint64(append(b, mask|127), uint64(n))
	}
	if key == nil {
		return append(b, p...)
	}
	b = append(b, key[:]...)
	start := len(b)
	b = append(b, p...)
	maskBytes(*key, 0, b[start:])
	return b
}

// maskBytes XORs b with key from position at and returns the position after it.
func maskBytes(key [4]byte, at int, b []byte) int {
	for i := range b {
		b[i] ^= key[(at+i)&3]
	}
	return (at + len(b)) & 3
}

// noEOF names a stream that ended inside a frame.
func noEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// noDelay sets TCP_NODELAY on the socket under conn, TLS or not. It is Go's default;
// stating it keeps a changed default from bringing back delayed-ACK stalls.
func noDelay(conn net.Conn) {
	if t, ok := conn.(*tls.Conn); ok {
		conn = t.NetConn()
	}
	if t, ok := conn.(*net.TCPConn); ok {
		_ = t.SetNoDelay(true)
	}
}
