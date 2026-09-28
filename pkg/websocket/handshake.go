package websocket

import (
	"bufio"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// acceptGUID is RFC 6455's constant behind Sec-WebSocket-Accept.
	acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	// maxHandshake bounds what a client reads before the frame stream: the answer's
	// head and, on a refusal, its body.
	maxHandshake = 16 << 10
	// maxRefusal is how much of a refusal's body its error keeps.
	maxRefusal       = 4 << 10
	handshakeTimeout = 10 * time.Second
)

var schemes = map[string]struct{ port, origin string }{
	"ws":  {"80", "http://"},
	"wss": {"443", "https://"},
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + acceptGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// HandshakeError is a server's answer other than 101.
type HandshakeError struct {
	Status int
	Body   []byte // the start of the answer's body
}

func (e *HandshakeError) Error() string {
	return fmt.Sprintf("websocket: upgrade refused with %d %s", e.Status, http.StatusText(e.Status))
}

// Dial opens a ws or wss URL, passing its path and query through unchanged. Its
// Origin is the URL's own, what a page served from that host sends: it guards a
// browser's cookies, which a native client does not hold. Without a deadline in
// ctx the handshake is bounded by handshakeTimeout.
func Dial(ctx context.Context, rawURL string) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	scheme, ok := schemes[u.Scheme]
	if !ok || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("websocket: %q is not a ws or wss URL with a host and no credentials or fragment", rawURL)
	}
	// Host and Origin omit a default port, as a browser's do.
	host := strings.TrimSuffix(u.Host, ":"+scheme.port)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(handshakeTimeout)
	}
	raw, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, "tcp",
		net.JoinHostPort(u.Hostname(), cmp.Or(u.Port(), scheme.port)))
	if err != nil {
		return nil, err
	}
	noDelay(raw)
	_ = raw.SetDeadline(deadline)
	conn := raw
	if u.Scheme == "wss" {
		secure := tls.Client(raw, &tls.Config{ServerName: u.Hostname(), NextProtos: []string{"http/1.1"}})
		if err := secure.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		conn = secure
	}
	c, err := clientHandshake(conn, u.RequestURI(), host, scheme.origin+host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return c, nil
}

func clientHandshake(conn net.Conn, target, host, origin string) (*Conn, error) {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	key := base64.StdEncoding.EncodeToString(nonce[:])
	// url.Parse refuses control characters, so no field here can end a line early.
	if _, err := io.WriteString(conn, "GET "+target+" HTTP/1.1\r\nHost: "+host+
		"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: "+key+
		"\r\nSec-WebSocket-Version: 13\r\nOrigin: "+origin+"\r\n\r\n"); err != nil {
		return nil, err
	}
	limit := &io.LimitedReader{R: conn, N: maxHandshake}
	br := bufio.NewReader(limit)
	if err := readUpgrade(br, key); err != nil {
		return nil, err
	}
	limit.N = math.MaxInt64 // the frame stream is not the handshake's to bound
	return newConn(conn, br, true), nil
}

// readUpgrade accepts only a faithful 101. This client asks for no extension or
// subprotocol, so an answer naming one is refused rather than ignored.
func readUpgrade(br *bufio.Reader, key string) error {
	answer, err := http.ReadResponse(br, nil)
	if err != nil {
		return fmt.Errorf("websocket: reading the upgrade's answer: %w", err)
	}
	if answer.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(answer.Body, maxRefusal))
		return &HandshakeError{Status: answer.StatusCode, Body: body}
	}
	h := answer.Header
	switch {
	case !hasToken(h, "Upgrade", "websocket") || !hasToken(h, "Connection", "upgrade"):
		return errors.New("websocket: a 101 that does not name the upgrade")
	case len(h.Values("Sec-WebSocket-Accept")) != 1 || h.Get("Sec-WebSocket-Accept") != acceptKey(key):
		return errors.New("websocket: a 101 that does not answer this key")
	case len(h.Values("Sec-WebSocket-Extensions")) != 0 || len(h.Values("Sec-WebSocket-Protocol")) != 0:
		return errors.New("websocket: a 101 naming an extension or subprotocol nobody asked for")
	}
	return nil
}

// hasToken reports whether the comma list in header name holds token.
func hasToken(h http.Header, name, token string) bool {
	for _, value := range h.Values(name) {
		for _, t := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// IsUpgrade reports whether r carries the two hop-by-hop headers of a WebSocket
// upgrade.
func IsUpgrade(r *http.Request) bool {
	return hasToken(r.Header, "Upgrade", "websocket") && hasToken(r.Header, "Connection", "upgrade")
}

// RequestError is a handshake Check refused, with the status to answer it.
type RequestError struct {
	Status int
	Reason string
}

func (e *RequestError) Error() string { return e.Reason }

// Handshake is a request Check accepted, which Upgrade answers.
type Handshake struct{ accept string }

// Check validates r as a version-13 upgrade before anything is spent on it. The
// http.Server bounds the request's size and time. A refusal is a *RequestError; a
// refused version also names 13 in w's headers, so the caller's answer carries it.
func Check(w http.ResponseWriter, r *http.Request) (Handshake, error) {
	if r.Method != http.MethodGet || !r.ProtoAtLeast(1, 1) || !IsUpgrade(r) {
		return Handshake{}, &RequestError{http.StatusBadRequest, "not a WebSocket upgrade"}
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return Handshake{}, &RequestError{http.StatusUpgradeRequired, "WebSocket version 13 is the one spoken"}
	}
	keys := r.Header.Values("Sec-WebSocket-Key")
	if len(keys) != 1 {
		return Handshake{}, &RequestError{http.StatusBadRequest, "not one Sec-WebSocket-Key"}
	}
	if nonce, err := base64.StdEncoding.DecodeString(keys[0]); err != nil || len(nonce) != 16 {
		return Handshake{}, &RequestError{http.StatusBadRequest, "a Sec-WebSocket-Key that is not 16 bytes"}
	}
	return Handshake{accept: acceptKey(keys[0])}, nil
}

// Upgrade takes w's connection and answers with the 101, choosing no extension or
// subprotocol. Bytes the server read past the request open the frame stream.
func (h Handshake) Upgrade(w http.ResponseWriter) (*Conn, error) {
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return nil, err
	}
	noDelay(conn)
	// The server's deadlines were the request's; the stream's are its owner's.
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n"+
		"Connection: Upgrade\r\nSec-WebSocket-Accept: "+h.accept+"\r\n\r\n"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return newConn(conn, rw.Reader, false), nil
}
