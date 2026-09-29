package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Scheme is how a session's bytes travel. TCP carries native play at both ends; a
// WebSocket carries the same stream for a browser or a network that blocks the TCP
// port, from the deployment's route or a host's own ws:// listener. A site names no
// session yet: its allocator is asked for one, and the link it answers is dialled.
type Scheme uint8

const (
	SchemeTCP Scheme = iota
	SchemeWebSocket
	SchemeSite
)

func (t Scheme) String() string {
	return [...]string{SchemeTCP: "tcp", SchemeWebSocket: "ws", SchemeSite: "site"}[t]
}

// Endpoint is a session address: how it is reached, where, and the session named
// on it. Every flag and command that takes an address reads it through ParseEndpoint.
type Endpoint struct {
	Scheme Scheme
	Addr   string // host:port; a WebSocket route's whole URL; a site's origin
	Name   string
}

// linkScheme prefixes the link a player is handed, so one string is both a thing to
// click and a thing to paste after -join. It is TCP.
const linkScheme = "vif://"

// ParseEndpoint reads [tcp://|vif://]host:port[/name] or a ws(s):// URL, whose last
// path segment is the name as a vif:// path is; one ending in '/' names none. An
// http(s):// URL is its site, whatever page it names. A bare host:port is TCP, and
// any other scheme is refused rather than read as a host.
func ParseEndpoint(target string) (Endpoint, error) {
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		u, err := url.Parse(target)
		if err != nil {
			return Endpoint{}, err
		}
		if u.Host == "" || u.User != nil {
			return Endpoint{}, fmt.Errorf("%q: a site is scheme://host[:port]", target)
		}
		return Endpoint{Scheme: SchemeSite, Addr: u.Scheme + "://" + u.Host}, nil
	}
	if strings.HasPrefix(target, "ws://") || strings.HasPrefix(target, "wss://") {
		u, err := url.Parse(target)
		if err != nil {
			return Endpoint{}, err
		}
		name := u.Path[strings.LastIndex(u.Path, "/")+1:]
		return Endpoint{Scheme: SchemeWebSocket, Addr: target, Name: name}, nil
	}
	e, rest := Endpoint{Scheme: SchemeTCP}, target
	if scheme, after, ok := strings.Cut(rest, "://"); ok {
		if scheme+"://" != linkScheme && scheme != "tcp" {
			return Endpoint{}, fmt.Errorf("%q: unknown scheme %q; use tcp, vif, ws(s) or http(s)", target, scheme)
		}
		rest = after
	}
	e.Addr, e.Name, _ = strings.Cut(rest, "/")
	if _, _, err := net.SplitHostPort(e.Addr); err != nil {
		return Endpoint{}, fmt.Errorf("%q: %w", target, err)
	}
	return e, nil
}

// Listenable reports why vif cannot serve e, nil when it can. A WebSocket host is
// plain ws on host:port and answers every path, since the session is named in-band.
func (e Endpoint) Listenable() error {
	switch e.Scheme {
	case SchemeTCP:
		return nil
	case SchemeSite:
		return errors.New("a site is asked for a session, not bound")
	}
	u, err := url.Parse(e.Addr)
	switch {
	case err != nil:
		return err
	case u.Scheme != "ws":
		return errors.New("vif serves plain ws; wss is a TLS edge's to terminate")
	case strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.User != nil:
		return errors.New("a WebSocket host binds ws://host:port and answers on every path")
	}
	_, _, err = net.SplitHostPort(u.Host)
	return err
}

// wsAddr names a WebSocket endpoint in the syntax -join reads.
type wsAddr string

func (a wsAddr) Network() string { return "websocket" }
func (a wsAddr) String() string  { return string(a) }

// sessionRequestTimeout outlasts the allocator's own bound: it answers once the new
// session's pod is ready, not when the request arrives.
const sessionRequestTimeout = 2 * time.Minute

// RequestSession asks site's allocator for a new session and returns the link this
// build joins it by. players and scenario choose from what the site advertises,
// zero for its defaults; a refusal, an unauthorised one included, is its message.
func RequestSession(site string, players int, scenario string) (string, error) {
	body, err := json.Marshal(struct {
		Players  int    `json:"players,omitempty"`
		Scenario string `json:"scenario,omitempty"`
	}{players, scenario})
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: sessionRequestTimeout}
	resp, err := client.Post(site+"/vif/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var reply struct {
		JoinTarget string `json:"join_target"`
		WSURL      string `json:"ws_url"`
		Error      struct{ Message string }
	}
	decoded := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply) == nil
	switch {
	case resp.StatusCode != http.StatusCreated && decoded && reply.Error.Message != "":
		return "", errors.New(reply.Error.Message)
	case resp.StatusCode != http.StatusCreated:
		return "", fmt.Errorf("%s answered %s", site, resp.Status)
	}
	if link := sessionLink(reply.JoinTarget, reply.WSURL); link != "" {
		return link, nil
	}
	return "", fmt.Errorf("%s created a session this build has no route to", site)
}
