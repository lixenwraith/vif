package network

import (
	"bytes"
	"cmp"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"syscall"
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

// Secure reports a wss:// endpoint: a WebSocket under TLS, which a page served over
// https can open and a host serves only with a certificate.
func (e Endpoint) Secure() bool {
	return e.Scheme == SchemeWebSocket && strings.HasPrefix(e.Addr, "wss://")
}

// Shown is how a message names a dialled address: a WebSocket route by its host,
// since the rest of its URL is a session key nobody reads, any other as given.
func Shown(target string) string {
	if e, err := ParseEndpoint(target); err == nil && e.Scheme == SchemeWebSocket {
		if u, err := url.Parse(e.Addr); err == nil && u.Host != "" {
			return u.Host
		}
	}
	return target
}

// Listenable reports why vif cannot serve e, nil when it can. A WebSocket host is
// ws or wss on host:port and answers every path, since the session is named in-band;
// whether a wss one has its certificate is Listen's to say.
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
	case strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.User != nil:
		return errors.New("a WebSocket host binds ws(s)://host:port and answers on every path")
	}
	_, _, err = net.SplitHostPort(u.Host)
	return err
}

// LoadTLS reads the certificate chain a wss:// host serves and its key, PEM files;
// an empty keyFile reads the key from the certificate's file. The configuration
// speaks HTTP/1.1 alone, the only protocol the upgrade is made over.
func LoadTLS(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, cmp.Or(keyFile, certFile))
	if err != nil {
		return nil, fmt.Errorf("certificate %s: %w", certFile, err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	}, nil
}

// plainError is what a player is told about a network failure in place of the
// system's words; the cause stays reachable to errors.Is and errors.As.
type plainError struct {
	text  string
	cause error
}

func (e plainError) Error() string { return e.text }
func (e plainError) Unwrap() error { return e.cause }

// plainNetError names what a player can do about a bind or a dial that failed. An
// error it does not know passes through as it is.
func plainNetError(err error) error {
	var (
		op     *net.OpError
		dns    *net.DNSError
		ne     net.Error
		verify *tls.CertificateVerificationError
		plain  plainError
		port   = -1
		text   string
	)
	if errors.As(err, &op) {
		if a, ok := op.Addr.(*net.TCPAddr); ok {
			port = a.Port
		}
	}
	switch {
	case err == nil:
		return nil
	case errors.As(err, &plain):
		return err // already in the player's words
	case errors.Is(err, os.ErrPermission) && port >= 0 && port < 1024:
		text = "ports below 1024 need root; use one above, e.g. :7777"
	case errors.Is(err, syscall.EADDRINUSE):
		text = "the port is in use; choose another"
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		text = "not an address of this machine; bind :port or one of its own"
	case errors.As(err, &dns) && dns.IsNotFound:
		text = "host name not found"
	case errors.Is(err, syscall.ECONNREFUSED):
		text = "nothing is listening there; is the host running on that port?"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		text = "unreachable from this network"
	case errors.As(err, &ne) && ne.Timeout():
		text = "no answer; check the address and the host's firewall or port forward"
	case errors.As(err, &verify):
		text = "the host's certificate is not one this machine trusts"
	default:
		return err
	}
	return plainError{text, err}
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
		siteSession
		Error struct{ Message string }
	}
	decoded := json.NewDecoder(io.LimitReader(resp.Body, siteReplyBytes)).Decode(&reply) == nil
	switch {
	case resp.StatusCode != http.StatusCreated && decoded && reply.Error.Message != "":
		return "", errors.New(reply.Error.Message)
	case resp.StatusCode != http.StatusCreated:
		return "", fmt.Errorf("%s answered %s", site, resp.Status)
	}
	if links := sessionLinks(reply.JoinTarget, reply.WSURL); len(links) > 0 {
		return links[0], nil
	}
	return "", fmt.Errorf("%s created a session this build has no route to", site)
}

// sessionLinks are the routes to a site's session this build can take, best first:
// natively the front door, then the WebSocket route for a network that blocks it.
func sessionLinks(joinTarget, wsURL string) []string {
	links := []string{wsURL}
	if buildDialsTCP {
		links = []string{joinTarget, wsURL}
	}
	return slices.DeleteFunc(links, func(link string) bool { return link == "" })
}

// siteSession is a session as its site describes it: the links each client takes.
type siteSession struct {
	ID         string `json:"id"`
	JoinTarget string `json:"join_target"`
	WSURL      string `json:"ws_url"`
}

// siteReplyBytes bounds how much of a site's answer is read.
const siteReplyBytes = 64 << 10

// listedLinks asks the site that issued addr, a link to the session named name, for
// the links this build can take to that session. The first site that lists its
// sessions answers for them; with none answering, there are no links and no error.
func listedLinks(addr, name string, timeout time.Duration) ([]string, error) {
	client := &http.Client{Timeout: timeout}
	for _, site := range linkSites(addr) {
		resp, err := client.Get(site + "/vif/api/sessions")
		if err != nil {
			continue
		}
		var reply struct{ Sessions []siteSession }
		listed := resp.StatusCode == http.StatusOK &&
			json.NewDecoder(io.LimitReader(resp.Body, siteReplyBytes)).Decode(&reply) == nil
		_ = resp.Body.Close()
		if !listed {
			continue
		}
		for _, s := range reply.Sessions {
			if s.ID != name {
				continue
			}
			if links := sessionLinks(s.JoinTarget, s.WSURL); len(links) > 0 {
				return links, nil
			}
			return nil, fmt.Errorf("%s lists session %s with no route this build has", site, name)
		}
		return nil, fmt.Errorf("%s lists no session %s", site, name)
	}
	return nil, nil
}

// linkSites are the origins that may have issued a link: a WebSocket route's own,
// then its host's over https, which is also where a front door's site answers.
func linkSites(addr string) []string {
	var sites []string
	host, _, err := net.SplitHostPort(addr)
	if u, uerr := url.Parse(addr); uerr == nil && (u.Scheme == "ws" || u.Scheme == "wss") {
		sites = append(sites, strings.Replace(u.Scheme, "ws", "http", 1)+"://"+u.Host)
		host, err = u.Hostname(), nil
	}
	if err != nil || host == "" {
		return sites
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if site := "https://" + host; !slices.Contains(sites, site) {
		sites = append(sites, site)
	}
	return sites
}
