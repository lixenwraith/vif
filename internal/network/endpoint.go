package network

import (
	"bytes"
	"cmp"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
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

// Hostless reports a bind address naming a port and no host, every interface: it
// says where a run listens, never where to dial it.
func (e Endpoint) Hostless() bool {
	host := ""
	if e.Scheme == SchemeWebSocket {
		if u, err := url.Parse(e.Addr); err == nil {
			host = u.Hostname()
		}
	} else if h, _, err := net.SplitHostPort(e.Addr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return host == "" || ip != nil && ip.IsUnspecified()
}

// Doors reads the addresses a host binds, comma-separated, each tcp, ws or wss and
// none naming a session: one session answers at every door, so a tcp door beside a
// ws or wss one takes terminals and pages at once.
func Doors(list string) ([]Endpoint, error) {
	var doors []Endpoint
	for _, target := range strings.Split(list, ",") {
		e, err := ParseEndpoint(strings.TrimSpace(target))
		if err == nil && e.Name != "" {
			err = errors.New("a bound address names no session; a host answers to -name")
		}
		if err == nil {
			err = e.Listenable()
		}
		if err != nil {
			return nil, err
		}
		doors = append(doors, e)
	}
	return doors, nil
}

// DoorList is the list Doors read, each door as it is bound.
func DoorList(doors []Endpoint) string {
	addrs := make([]string, len(doors))
	for i, d := range doors {
		addrs[i] = d.Addr
	}
	return strings.Join(addrs, ",")
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
// an empty keyFile reads the key from the certificate's file.
func LoadTLS(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, cmp.Or(keyFile, certFile))
	if err != nil {
		return nil, fmt.Errorf("certificate %s: %w", certFile, err)
	}
	return serverTLS(cert), nil
}

// SelfSignedTLS makes the certificate a wss:// host serves for this run alone, with
// no authority behind it: a P-256 key and the names given, this machine's loopback
// among them. A joiner trusts it by its fingerprint (TrustTLS), a browser by being
// told to; what authenticates the host without either is a later handshake bound to
// this certificate.
func SelfSignedTLS(hosts []string) (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	now := time.Now() // [wall] a certificate's validity is the world's clock
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "vif"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range append([]string{"localhost", "127.0.0.1", "::1"}, hosts...) {
		switch ip := net.ParseIP(h); {
		case h == "":
		case ip != nil && !slices.ContainsFunc(template.IPAddresses, ip.Equal):
			template.IPAddresses = append(template.IPAddresses, ip)
		case ip == nil && !slices.Contains(template.DNSNames, h):
			template.DNSNames = append(template.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return serverTLS(tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}), nil
}

// selfSignedValidity outlasts any run; a browser accepts a private certificate of
// any length.
const selfSignedValidity = 365 * 24 * time.Hour

// serverTLS is what a wss:// door speaks: TLS 1.2 or later, and HTTP/1.1 alone, the
// only protocol the upgrade is made over.
func serverTLS(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	}
}

// Fingerprint names a door's certificate as a joiner pins it: sha256: and the hex
// digest of its leaf; empty for a configuration serving none.
func Fingerprint(cfg *tls.Config) string {
	if cfg == nil || len(cfg.Certificates) == 0 || len(cfg.Certificates[0].Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(cfg.Certificates[0].Certificate[0])
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TrustTLS is what a wss:// dial trusts a host's certificate by, in place of the
// system's roots: sha256:<hex> pins the certificate itself, how a self-signed door is
// joined, and anything else is a PEM file of the issuers to trust.
func TrustTLS(spec string) (*tls.Config, error) {
	if digest, ok := strings.CutPrefix(spec, "sha256:"); ok {
		want, err := hex.DecodeString(digest)
		if err != nil || len(want) != sha256.Size {
			return nil, fmt.Errorf("%q: a fingerprint is sha256: and 64 hex digits", spec)
		}
		pin := [sha256.Size]byte(want)
		return &tls.Config{
			InsecureSkipVerify: true, // the pin below is the verification; no authority is asked
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 || sha256.Sum256(raw[0]) != pin {
					return plainError{"the host's certificate is not the one -ca pins", errors.New("certificate pin mismatch")}
				}
				return nil
			},
		}, nil
	}
	pemBytes, err := os.ReadFile(spec)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("%s holds no certificate", spec)
	}
	return &tls.Config{RootCAs: pool}, nil
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
	case isErrno(err, sysErrno.addrInUse):
		text = "the port is in use; choose another"
	case isErrno(err, sysErrno.addrNotAvail):
		text = "not an address of this machine; bind :port or one of its own"
	case errors.As(err, &dns) && dns.IsNotFound:
		text = "host name not found"
	case isErrno(err, sysErrno.connRefused):
		text = "nothing is listening there; is the host running on that port?"
	case isErrno(err, sysErrno.unreachable):
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

// isErrno reports whether err is one of the system's codes for a failure.
func isErrno(err error, codes []error) bool {
	return slices.ContainsFunc(codes, func(code error) bool { return errors.Is(err, code) })
}

// wsAddr names a WebSocket endpoint in the syntax -join reads.
type wsAddr string

func (a wsAddr) Network() string { return "websocket" }
func (a wsAddr) String() string  { return string(a) }

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
	client := &http.Client{Timeout: parameter.NetworkSessionRequestTimeout}
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
