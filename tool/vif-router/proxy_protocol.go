package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
)

// A PROXY protocol header is what an edge that ended TLS writes before the dialer's
// first byte, so the address that keys the budgets is the player's and not the
// edge's: version 1, the text line nginx writes, or version 2, the binary header
// HAProxy writes. With -route-proxy-protocol a connection without one is closed,
// since the edge is then the only way in.
const (
	proxyV1Max = 107  // the longest version 1 line, its line end included
	proxyV2Max = 1024 // the address block and what follows it, well past both families
)

var proxyV2Signature = [12]byte{'\r', '\n', '\r', '\n', 0, '\r', '\n', 'Q', 'U', 'I', 'T', '\n'}

// readProxyHeader reads one header off conn and returns the source it names; a
// LOCAL connection, the edge's own health check, names the peer itself.
func readProxyHeader(conn net.Conn) (net.Addr, error) {
	var head [12]byte
	if _, err := io.ReadFull(conn, head[:6]); err != nil {
		return nil, err
	}
	if string(head[:6]) == "PROXY " {
		return readProxyV1(conn, head[:6])
	}
	if _, err := io.ReadFull(conn, head[6:]); err != nil {
		return nil, err
	}
	if head != proxyV2Signature {
		return nil, errors.New("no PROXY protocol header")
	}
	return readProxyV2(conn)
}

// readProxyV1 reads the rest of "PROXY TCP4|TCP6 src dst sport dport\r\n", byte by
// byte, so nothing of the dialer's own is taken with it.
func readProxyV1(conn net.Conn, prefix []byte) (net.Addr, error) {
	line := slices.Clone(prefix)
	var b [1]byte
	for len(line) < proxyV1Max && (len(line) == 0 || line[len(line)-1] != '\n') {
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return nil, err
		}
		line = append(line, b[0])
	}
	text, ok := strings.CutSuffix(string(line), "\r\n")
	if !ok {
		return nil, errors.New("PROXY protocol header: no line end within its bound")
	}
	f := strings.Split(text, " ")
	if len(f) != 6 || (f[1] != "TCP4" && f[1] != "TCP6") {
		return nil, fmt.Errorf("PROXY protocol header: %q", text)
	}
	ip := net.ParseIP(f[2])
	port, err := strconv.Atoi(f[4])
	if ip == nil || err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("PROXY protocol header: source %s:%s", f[2], f[4])
	}
	return &net.TCPAddr{IP: ip, Port: port}, nil
}

// readProxyV2 reads what follows the signature: version and command, family and
// protocol, the address block's length, then the block itself.
func readProxyV2(conn net.Conn) (net.Addr, error) {
	var rest [4]byte
	if _, err := io.ReadFull(conn, rest[:]); err != nil {
		return nil, err
	}
	version, command, family := rest[0]>>4, rest[0]&0x0f, rest[1]>>4
	n := int(binary.BigEndian.Uint16(rest[2:]))
	if version != 2 || n > proxyV2Max {
		return nil, fmt.Errorf("PROXY protocol header: version %d, %d bytes", version, n)
	}
	block := make([]byte, n)
	if _, err := io.ReadFull(conn, block); err != nil {
		return nil, err
	}
	switch {
	case command == 0:
		return conn.RemoteAddr(), nil
	case command != 1:
		return nil, fmt.Errorf("PROXY protocol header: command %d", command)
	case family == 1 && n >= 12:
		return &net.TCPAddr{IP: slices.Clone(block[0:4]), Port: int(binary.BigEndian.Uint16(block[8:10]))}, nil
	case family == 2 && n >= 36:
		return &net.TCPAddr{IP: slices.Clone(block[0:16]), Port: int(binary.BigEndian.Uint16(block[32:34]))}, nil
	}
	return nil, fmt.Errorf("PROXY protocol header: family %d", family)
}
