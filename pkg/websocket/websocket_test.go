package websocket

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamConn is a net.Conn reading a fixed input and recording what is written.
type streamConn struct {
	io.Reader
	out bytes.Buffer
}

func (s *streamConn) Write(p []byte) (int, error)      { return s.out.Write(p) }
func (s *streamConn) Close() error                     { return nil }
func (s *streamConn) LocalAddr() net.Addr              { return nil }
func (s *streamConn) RemoteAddr() net.Addr             { return nil }
func (s *streamConn) SetDeadline(time.Time) error      { return nil }
func (s *streamConn) SetReadDeadline(time.Time) error  { return nil }
func (s *streamConn) SetWriteDeadline(time.Time) error { return nil }

// reading is a Conn in role client reading input.
func reading(input []byte, client bool) (*Conn, *streamConn) {
	s := &streamConn{Reader: bytes.NewReader(input)}
	return newConn(s, bufio.NewReader(s), client), s
}

var testKey = &[4]byte{0x37, 0xfa, 0x21, 0x3d}

// rawFrame reads one frame without a Conn, unmasking it if it is masked.
func rawFrame(t *testing.T, br *bufio.Reader) (byte, []byte) {
	t.Helper()
	var h [8]byte
	if _, err := io.ReadFull(br, h[:2]); err != nil {
		t.Fatalf("reading a frame header: %v", err)
	}
	b0, n := h[0], int(h[1]&0x7f)
	switch n {
	case 126:
		_, _ = io.ReadFull(br, h[:2])
		n = int(binary.BigEndian.Uint16(h[:2]))
	case 127:
		_, _ = io.ReadFull(br, h[:8])
		n = int(binary.BigEndian.Uint64(h[:8]))
	}
	var key [4]byte
	if h[1]&0x80 != 0 {
		_, _ = io.ReadFull(br, key[:])
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("reading a %d-byte payload: %v", n, err)
	}
	maskBytes(key, 0, payload)
	return b0, payload
}

func TestAcceptKeyIsRFC6455Section1_3(t *testing.T) {
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("acceptKey = %q", got)
	}
}

// TestFramesAreRFC6455Section5_7s: its examples encode byte for byte, and the two
// binary ones read back through a client. Its "Hello" frames are text, which Read
// refuses, so those are checked on the writing side only.
func TestFramesAreRFC6455Section5_7s(t *testing.T) {
	for _, tc := range []struct {
		name    string
		op      byte
		payload []byte
		key     *[4]byte
		want    []byte // the whole frame, or its header when the payload is long
	}{
		{"masked Hello", opText, []byte("Hello"), testKey,
			[]byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}},
		{"unmasked Hello", opText, []byte("Hello"), nil, []byte{0x81, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}},
		{"a 16-bit length", opBinary, bytes.Repeat([]byte{7}, 256), nil, []byte{0x82, 0x7e, 0x01, 0x00}},
		{"a 64-bit length", opBinary, bytes.Repeat([]byte{7}, 65536), nil,
			[]byte{0x82, 0x7f, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}},
	} {
		frame := appendFrame(nil, tc.op, tc.payload, tc.key)
		if !bytes.HasPrefix(frame, tc.want) || (len(tc.payload) < 126 && len(frame) != len(tc.want)) {
			t.Errorf("%s: encoded % x, want % x", tc.name, frame[:min(len(frame), 16)], tc.want)
		}
		if tc.op != opBinary {
			continue
		}
		c, _ := reading(frame, true)
		got, err := io.ReadAll(c)
		if err != nil || !bytes.Equal(got, tc.payload) {
			t.Errorf("%s: read %d bytes, %v; want %d", tc.name, len(got), err, len(tc.payload))
		}
	}
}

// TestReadRefusesWhatRFC6455Forbids: each malformed frame ends the read side with an
// error other than EOF and is answered with the close code the RFC names.
func TestReadRefusesWhatRFC6455Forbids(t *testing.T) {
	masked := func(op byte, payload string) []byte { return appendFrame(nil, op, []byte(payload), testKey) }
	unfinished := func(frame []byte) []byte { frame[0] &^= 0x80; return frame }
	closing := func(payload ...byte) []byte { return appendFrame(nil, opClose, payload, testKey) }
	for _, tc := range []struct {
		name   string
		client bool
		input  []byte
		code   uint16
	}{
		{"reserved bits", false, func() []byte { f := masked(opBinary, "x"); f[0] |= 0x40; return f }(), closeProtocol},
		{"an unknown data opcode", false, masked(0x3, "x"), closeProtocol},
		{"an unknown control opcode", false, masked(0xB, "x"), closeProtocol},
		{"an unmasked frame to a server", false, appendFrame(nil, opBinary, []byte("x"), nil), closeProtocol},
		{"a masked frame to a client", true, masked(opBinary, "x"), closeProtocol},
		{"a length with its top bit set", true, []byte{0x82, 0x7f, 0x80, 0, 0, 0, 0, 0, 0, 1}, closeProtocol},
		{"a control frame over 125 bytes", false, masked(opPing, strings.Repeat("p", 126)), closeProtocol},
		{"a fragmented control frame", false, unfinished(masked(opPing, "p")), closeProtocol},
		{"a continuation opening nothing", false, masked(opContinuation, "x"), closeProtocol},
		{"a data frame inside an open message", false,
			append(unfinished(masked(opBinary, "x")), masked(opBinary, "y")...), closeProtocol},
		{"text", false, masked(opText, "x"), closeUnsupported},
		{"a close code nobody may send", false, closing(0x03, 0xed), closeProtocol},
		{"a one-byte close", false, closing(0x03), closeProtocol},
		{"a close reason that is not UTF-8", false, closing(0x03, 0xe8, 0xff), closeInvalid},
	} {
		c, s := reading(tc.input, tc.client)
		_, err := io.ReadAll(c)
		if err == nil {
			t.Errorf("%s: read to its end", tc.name)
			continue
		}
		op, payload := rawFrame(t, bufio.NewReader(&s.out))
		if op != 0x80|opClose || len(payload) != 2 || binary.BigEndian.Uint16(payload) != tc.code {
			t.Errorf("%s: answered %#x % x, want close %d", tc.name, op, payload, tc.code)
		}
	}
}

// TestAPeersCloseIsEchoedAndEndsTheStream: the stream ends as EOF, the peer is sent
// its own code back, and Close has no reply left to wait for.
func TestAPeersCloseIsEchoedAndEndsTheStream(t *testing.T) {
	c, s := reading(append([]byte{0x88, 0x02}, binary.BigEndian.AppendUint16(nil, 4001)...), true)
	if n, err := c.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Fatalf("Read = %d, %v; want EOF", n, err)
	}
	start := time.Now()
	if err := c.Close(); err != nil || time.Since(start) > closeWait/2 {
		t.Fatalf("Close = %v after %s", err, time.Since(start))
	}
	br := bufio.NewReader(&s.out)
	op, payload := rawFrame(t, br)
	if op != 0x80|opClose || binary.BigEndian.Uint16(payload) != 4001 || br.Buffered() != 0 {
		t.Fatalf("answered %#x % x with %d bytes more, want close 4001 alone", op, payload, br.Buffered())
	}
}

// TestAPingFloodLeavesOnePong: pings that arrive while a writer holds the lock
// leave one reply, the newest, sent when the lock is released.
func TestAPingFloodLeavesOnePong(t *testing.T) {
	var input []byte
	for i := range 100 {
		input = appendFrame(input, opPing, []byte{byte(i)}, testKey)
	}
	input = appendFrame(input, opBinary, []byte("x"), testKey)
	c, s := reading(input, false)
	c.wmu.Lock()
	if _, err := c.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if s.out.Len() != 0 {
		t.Fatalf("a pong was written under another writer's lock")
	}
	c.unlockWrite()
	br := bufio.NewReader(&s.out)
	op, payload := rawFrame(t, br)
	if op != 0x80|opPong || !bytes.Equal(payload, []byte{99}) || br.Buffered() != 0 {
		t.Fatalf("answered %#x % x with %d bytes more; want one pong for the last ping", op, payload, br.Buffered())
	}
}

func TestReadUpgradeAcceptsOnlyAFaithful101(t *testing.T) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	good := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n"
	for _, tc := range []struct {
		name, answer string
		ok           bool
	}{
		{"the RFC's answer", good + "\r\n", true},
		{"another key's accept", strings.Replace(good, "s3pP", "AAAA", 1) + "\r\n", false},
		{"no Upgrade header", strings.Replace(good, "Upgrade: websocket\r\n", "", 1) + "\r\n", false},
		{"an extension", good + "Sec-WebSocket-Extensions: permessage-deflate\r\n\r\n", false},
		{"a subprotocol", good + "Sec-WebSocket-Protocol: chat\r\n\r\n", false},
		{"a 200", "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", false},
	} {
		if err := readUpgrade(bufio.NewReader(strings.NewReader(tc.answer)), key); (err == nil) != tc.ok {
			t.Errorf("%s: readUpgrade = %v", tc.name, err)
		}
	}

	var refused *HandshakeError
	body := `{"error":{"code":"origin_refused","message":"This session route answers one origin"}}`
	err := readUpgrade(bufio.NewReader(strings.NewReader(
		fmt.Sprintf("HTTP/1.1 403 Forbidden\r\nContent-Length: %d\r\n\r\n%s", len(body), body))), key)
	if !errors.As(err, &refused) || refused.Status != http.StatusForbidden || string(refused.Body) != body {
		t.Fatalf("a refusal read as %v", err)
	}
}

func TestCheckRefusesWhatUpgradeCannotAnswer(t *testing.T) {
	request := func(edit func(http.Header)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", "keep-alive, Upgrade")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		edit(r.Header)
		return r
	}
	for _, tc := range []struct {
		name   string
		edit   func(http.Header)
		status int
	}{
		{"no upgrade", func(h http.Header) { h.Del("Upgrade") }, http.StatusBadRequest},
		{"version 8", func(h http.Header) { h.Set("Sec-WebSocket-Version", "8") }, http.StatusUpgradeRequired},
		{"a 15-byte key", func(h http.Header) { h.Set("Sec-WebSocket-Key", "AAAAAAAAAAAAAAAAAAAA") }, http.StatusBadRequest},
		{"two keys", func(h http.Header) { h.Add("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==") }, http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		_, err := Check(w, request(tc.edit))
		var refused *RequestError
		if !errors.As(err, &refused) || refused.Status != tc.status {
			t.Errorf("%s: Check = %v, want status %d", tc.name, err, tc.status)
		}
		if tc.status == http.StatusUpgradeRequired && w.Header().Get("Sec-WebSocket-Version") != "13" {
			t.Errorf("%s: the refusal does not name version 13", tc.name)
		}
	}
	if h, err := Check(httptest.NewRecorder(), request(func(http.Header) {})); err != nil ||
		h.accept != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("Check = %+v, %v", h, err)
	}
}

// echoServer answers upgrades and echoes each one's stream until the peer closes it,
// reporting how the echo ended.
func echoServer(t *testing.T) (string, chan error) {
	t.Helper()
	ended := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, err := Check(w, r)
		if err != nil {
			http.Error(w, err.Error(), err.(*RequestError).Status)
			return
		}
		conn, err := h.Upgrade(w)
		if err != nil {
			ended <- err
			return
		}
		_, err = io.Copy(conn, conn)
		_ = conn.Close()
		ended <- err
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), ended
}

// TestRoundTripAcrossFragmentsPingsAndClose runs a client against a server: a
// fragmented message with a ping inside it arrives as one stream and is answered by
// a pong between its halves, and Close completes the handshake well inside its bound.
func TestRoundTripAcrossFragmentsPingsAndClose(t *testing.T) {
	url, ended := echoServer(t)
	c, err := Dial(t.Context(), url+"/any/path?q=1")
	if err != nil {
		t.Fatal(err)
	}
	var frames []byte
	frames = append(frames, appendFrame(nil, opBinary, []byte("Hel"), testKey)...)
	frames[0] &^= 0x80
	frames = appendFrame(frames, opPing, []byte("p"), testKey)
	frames = appendFrame(frames, opContinuation, []byte("lo"), testKey)
	if _, err := c.conn.Write(frames); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		op      byte
		payload string
	}{{opBinary, "Hel"}, {opPong, "p"}, {opBinary, "lo"}} {
		if op, payload := rawFrame(t, c.br); op != 0x80|want.op || string(payload) != want.payload {
			t.Fatalf("read %#x %q, want %#x %q", op, payload, 0x80|want.op, want.payload)
		}
	}

	if _, err := c.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "world" {
		t.Fatalf("read back %q, %v", got, err)
	}
	start := time.Now()
	if err := c.Close(); err != nil || time.Since(start) > closeWait/2 {
		t.Fatalf("Close = %v after %s", err, time.Since(start))
	}
	if err := c.Close(); err != nil {
		t.Fatalf("a second Close = %v", err)
	}
	if err := <-ended; err != nil {
		t.Fatalf("the server's stream ended with %v, want the client's close", err)
	}
}

// TestHandshakeLeavesBytesReadPastItToTheStream: a frame sent in the same write as
// the request or the 101 is the stream's first, on both sides.
func TestHandshakeLeavesBytesReadPastItToTheStream(t *testing.T) {
	url, _ := echoServer(t)
	raw, err := net.Dial("tcp", strings.TrimPrefix(url, "ws://"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := clientHandshake(&earlyFrame{Conn: raw, frame: appendFrame(nil, opBinary, []byte("early"), testKey)},
		"/", "h", "http://h")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "early" {
		t.Fatalf("the server echoed %q, %v", got, err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		_, _ = conn.Write(append([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n"+
			"Connection: Upgrade\r\nSec-WebSocket-Accept: "+acceptKey(r.Header.Get("Sec-WebSocket-Key"))+"\r\n\r\n"),
			appendFrame(nil, opBinary, []byte("first"), nil)...))
	}()
	d, err := Dial(t.Context(), "ws://"+listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := io.ReadFull(d, got); err != nil || string(got) != "first" {
		t.Fatalf("the client read %q, %v", got, err)
	}
}

// earlyFrame appends frame to the first write, which is the handshake's request.
type earlyFrame struct {
	net.Conn
	frame []byte
}

func (e *earlyFrame) Write(p []byte) (int, error) {
	if e.frame == nil {
		return e.Conn.Write(p)
	}
	joined := append(append([]byte(nil), p...), e.frame...)
	e.frame = nil
	_, err := e.Conn.Write(joined)
	return len(p), err
}
