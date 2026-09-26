package network

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Scheme is how a session's bytes travel. TCP carries native play at both ends;
// a WebSocket route is a browser's way to the same session, served by the
// deployment's bridge to the TCP port. UDP is named so an address can say it.
type Scheme uint8

const (
	SchemeTCP Scheme = iota
	SchemeWebSocket
	SchemeUDP
)

func (t Scheme) String() string {
	return [...]string{SchemeTCP: "tcp", SchemeWebSocket: "ws", SchemeUDP: "udp"}[t]
}

// Endpoint is a session address: how it is reached, where, and the session named
// on it. Every flag and command that takes an address reads it through ParseEndpoint.
type Endpoint struct {
	Scheme Scheme
	Addr   string // host:port; a WebSocket route's whole URL
	Name   string
}

// linkScheme prefixes the link a player is handed, so one string is both a thing to
// click and a thing to paste after -join. It is TCP.
const linkScheme = "vif://"

// ParseEndpoint reads [tcp://|vif://]host:port[/name] or a ws(s):// URL, whose path
// is the front door's rather than a session name. A bare host:port is TCP.
func ParseEndpoint(target string) (Endpoint, error) {
	if strings.HasPrefix(target, "ws://") || strings.HasPrefix(target, "wss://") {
		if _, err := url.Parse(target); err != nil {
			return Endpoint{}, err
		}
		return Endpoint{Scheme: SchemeWebSocket, Addr: target}, nil
	}
	e, rest := Endpoint{Scheme: SchemeTCP}, target
	switch {
	case strings.HasPrefix(rest, "udp://"):
		return Endpoint{}, fmt.Errorf("%q: udp is not available yet; tcp is the default", target)
	case strings.HasPrefix(rest, "tcp://"):
		rest = strings.TrimPrefix(rest, "tcp://")
	default:
		rest = strings.TrimPrefix(rest, linkScheme)
	}
	e.Addr, e.Name, _ = strings.Cut(rest, "/")
	if _, _, err := net.SplitHostPort(e.Addr); err != nil {
		return Endpoint{}, fmt.Errorf("%q: %w", target, err)
	}
	return e, nil
}

// Listenable reports why vif cannot serve e, nil when it can: a WebSocket route is
// the deployment's bridge in front of a TCP port, not something vif binds.
func (e Endpoint) Listenable() error {
	if e.Scheme == SchemeWebSocket {
		return errors.New("a WebSocket route is served by the deployment's bridge; host on tcp")
	}
	return nil
}
