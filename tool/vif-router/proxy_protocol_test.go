package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// TestAProxyHeaderNamesTheDialer: the version 1 line nginx writes and the version 2
// header HAProxy writes each yield the source they name, a LOCAL header the peer
// itself, and anything else is refused before a route frame is read.
func TestAProxyHeaderNamesTheDialer(t *testing.T) {
	v2 := func(command byte, family byte, block []byte) []byte {
		h := append([]byte(nil), proxyV2Signature[:]...)
		h = append(h, 0x20|command, family<<4|1)
		h = binary.BigEndian.AppendUint16(h, uint16(len(block)))
		return append(h, block...)
	}
	v4 := []byte{203, 0, 113, 9, 10, 0, 0, 1, 0x9c, 0x40, 0x1e, 0x61} // 40000 -> 7777
	v6 := append(append(net.ParseIP("2001:db8::9").To16(), net.ParseIP("fd00::1").To16()...), 0x9c, 0x40, 0x1e, 0x61)
	for name, tt := range map[string]struct {
		header []byte
		want   string
		peer   bool
	}{
		"v1 tcp4":     {[]byte("PROXY TCP4 203.0.113.9 10.0.0.1 40000 7777\r\n"), "203.0.113.9:40000", false},
		"v1 tcp6":     {[]byte("PROXY TCP6 2001:db8::9 fd00::1 40000 7777\r\n"), "[2001:db8::9]:40000", false},
		"v2 tcp4":     {v2(1, 1, v4), "203.0.113.9:40000", false},
		"v2 tcp6":     {v2(1, 2, v6), "[2001:db8::9]:40000", false},
		"v2 local":    {v2(0, 0, nil), "", true},
		"v1 unknown":  {[]byte("PROXY UNKNOWN\r\n"), "", false},
		"v1 no end":   {[]byte("PROXY TCP4 203.0.113.9 10.0.0.1 40000 7777 and then some more bytes than a line holds, on and on, past the bound of one"), "", false},
		"a vif frame": {[]byte{5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 16}, "", false},
	} {
		client, server := net.Pipe()
		go func() {
			_, _ = client.Write(tt.header)
			_, _ = client.Write([]byte("rest"))
		}()
		_ = server.SetReadDeadline(time.Now().Add(time.Second))
		from, err := readProxyHeader(server)
		switch {
		case tt.peer:
			if err != nil || from != server.RemoteAddr() {
				t.Errorf("%s: %v %v, want the peer itself", name, from, err)
			}
		case tt.want == "":
			if err == nil {
				t.Errorf("%s: read %v, want a refusal", name, from)
			}
		case err != nil || from.String() != tt.want:
			t.Errorf("%s: %v %v, want %s", name, from, err, tt.want)
		}
		if err == nil {
			rest := make([]byte, 4)
			if _, err := server.Read(rest); err != nil || string(rest) != "rest" {
				t.Errorf("%s: the header's reader took %q of what followed", name, rest)
			}
		}
		_ = client.Close()
		_ = server.Close()
	}
}
