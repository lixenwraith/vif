package websocket

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FuzzReadFrames: any input, in either role, ends in an error or EOF having returned
// no more payload than it carried.
func FuzzReadFrames(f *testing.F) {
	f.Add([]byte{0x82, 0x7e, 0x01, 0x00}, true)
	f.Add([]byte{0x82, 0x7f, 0, 0, 0, 0, 0, 1, 0, 0}, true)
	f.Add(appendFrame(appendFrame(nil, opPing, []byte("p"), testKey), opBinary, []byte("x"), testKey), false)
	f.Add(appendFrame(nil, opClose, []byte{0x03, 0xe8, 'o', 'k'}, testKey), false)
	f.Fuzz(func(t *testing.T, input []byte, client bool) {
		c, _ := reading(input, client)
		n, _ := io.Copy(io.Discard, c)
		if n > int64(len(input)) {
			t.Fatalf("read %d payload bytes from %d input bytes", n, len(input))
		}
	})
}

// FuzzReadUpgrade: no answer panics the client, and one it accepts answers its key.
func FuzzReadUpgrade(f *testing.F) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	f.Add([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n"))
	f.Add([]byte("HTTP/1.1 403 Forbidden\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n"))
	f.Fuzz(func(t *testing.T, answer []byte) {
		br := bufio.NewReader(io.LimitReader(bytes.NewReader(answer), maxHandshake))
		if readUpgrade(br, key) == nil && !bytes.Contains(answer, []byte(acceptKey(key))) {
			t.Fatalf("accepted an answer without this key's accept: %q", answer)
		}
	})
}

// FuzzCheck: no request panics the server, and one it accepts is a version-13 upgrade
// with a 16-byte key.
func FuzzCheck(f *testing.F) {
	f.Add([]byte("GET /vif/ws/0123456789abcdef HTTP/1.1\r\nHost: h\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"))
	f.Fuzz(func(t *testing.T, request []byte) {
		r, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(request)))
		if err != nil {
			return
		}
		if _, err := Check(httptest.NewRecorder(), r); err != nil {
			return
		}
		nonce, err := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key"))
		if r.Header.Get("Sec-WebSocket-Version") != "13" || err != nil || len(nonce) != 16 || !IsUpgrade(r) {
			t.Fatalf("accepted %q", request)
		}
	})
}
