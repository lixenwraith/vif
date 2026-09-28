package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lixenwraith/vif/pkg/websocket"
)

const (
	// wsRoutePrefix has no version segment: this endpoint's scope is one session,
	// and a session identifier is not a thing that gains a second shape.
	wsRoutePrefix = "/vif/ws/"
	// wsBridgePort is the sidecar's private port. It is published by no Service and
	// forwarded by no firewall; the node's allocator is the only thing that dials it.
	wsBridgePort = "7779"
	// The bridge answers its handshake without reading the game, so this bounds a
	// pod that accepted the connection and then said nothing.
	wsHandshakeTimeout = 10 * time.Second
)

// A session identifier is 16 hex characters. Matching here rather than trusting the
// site keeps each route refusable on its own, and keeps it out of a label selector.
var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

type upstreamKey struct{}

// wsRouter carries one browser connection to the session it names. Without a bridge
// it terminates the WebSocket and splices its payload to the pod's game port, as
// the front door does; with one it proxies the upgrade to the pod's sidecar.
type wsRouter struct {
	origin string
	holds  *holds
	proxy  *httputil.ReverseProxy // nil: this allocator terminates the socket
	game   string                 // the pod port dialled; a test points it at a stand-in
}

func newWSRouter(origin string, bridge bool, held *holds, logger *slog.Logger) *wsRouter {
	router := &wsRouter{origin: origin, holds: held, game: gamePort}
	if !bridge {
		return router
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	// One connection per player, held for the match. Pooling would only risk
	// handing the next player a connection the last session had not finished
	// tearing down.
	transport.DisableKeepAlives = true
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = wsHandshakeTimeout

	router.proxy = &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			upstream, _ := request.In.Context().Value(upstreamKey{}).(*url.URL)
			request.SetURL(upstream)
			request.Out.Host = upstream.Host
			// The bridge routes nothing: it is this session's own listener, so the
			// path it is asked for is meaningless and the origin it would check is
			// this allocator's business, already done.
			request.Out.URL.Path = "/"
			request.Out.URL.RawQuery = ""
			request.Out.Header.Del("Origin")
			request.Out.Header.Del("Cookie")
			request.Out.Header.Del("Authorization")
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			logger.Warn("session socket upstream failed", "error", err)
			writeAPIError(w, http.StatusBadGateway, "session_unreachable",
				"The session did not accept the connection")
		},
	}
	return router
}

// holds bounds the connections one session carries through this allocator, over
// the front door and the browser route alike. The pod's per-address limiter cannot:
// every proxied participant reaches it from the node or from its own loopback.
type holds struct {
	max int

	mu sync.Mutex
	n  map[string]int
}

func newHolds(max int) *holds { return &holds{max: max, n: make(map[string]int)} }

func (h *holds) hold(id string) (func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n[id] >= h.max {
		return nil, false
	}
	h.n[id]++
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.n[id] <= 1 {
			delete(h.n, id)
			return
		}
		h.n[id]--
	}, true
}

// handleSessionSocket validates the request, resolves the session, then stops
// looking at the bytes. Everything refusable is refused before the upgrade,
// because after it there is no status code left to send.
func (s *apiServer) handleSessionSocket(w http.ResponseWriter, r *http.Request) {
	if s.ws == nil {
		writeAPIError(w, http.StatusNotImplemented, "web_route_not_configured",
			"This deployment publishes no browser session route")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, wsRoutePrefix)
	if !sessionIDPattern.MatchString(id) {
		writeAPIError(w, http.StatusNotFound, "unknown_session", "No such session")
		return
	}
	if !websocket.IsUpgrade(r) {
		writeAPIError(w, http.StatusBadRequest, "not_an_upgrade",
			"This route answers a WebSocket upgrade and nothing else")
		return
	}
	// A browser sends Origin on every WebSocket handshake and cannot forge it.
	// Nothing else admits a caller here, so this is the whole of same-origin.
	if r.Header.Get("Origin") != s.ws.origin {
		writeAPIError(w, http.StatusForbidden, "origin_refused",
			"This session route answers one origin")
		return
	}

	lookup, cancel := context.WithTimeout(r.Context(), wsHandshakeTimeout)
	podIP, err := s.allocator.routeSession(lookup, id)
	cancel()
	if err != nil {
		s.writeRouteError(w, id, err)
		return
	}

	release, ok := s.ws.holds.hold(id)
	if !ok {
		w.Header().Set("Retry-After", "5")
		writeAPIError(w, http.StatusServiceUnavailable, "session_busy",
			"This session is carrying as many connections as it accepts")
		return
	}
	defer release()
	if s.ws.proxy == nil {
		s.terminateSessionSocket(w, r, id, podIP)
		return
	}

	// The server's finite request deadlines belong to the finite API. They are set
	// on the connection this is about to hijack, so a match would end at whichever
	// expired first.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Time{})

	upstream := &url.URL{Scheme: "http", Host: net.JoinHostPort(podIP, wsBridgePort)}
	s.log.Info("session socket opened", "session", id)
	s.ws.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), upstreamKey{}, upstream)))
}

// terminateSessionSocket answers the upgrade here and splices it to the pod, which
// reads the browser's own route frame and refuses a name not its own. The pod is
// dialled before the 101, so an unreachable one is refused as the proxy refused it.
func (s *apiServer) terminateSessionSocket(w http.ResponseWriter, r *http.Request, id, podIP string) {
	handshake, err := websocket.Check(w, r)
	if err != nil {
		refused := err.(*websocket.RequestError)
		writeAPIError(w, refused.Status, "bad_handshake", capitalize(refused.Reason))
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(podIP, s.ws.game), routeTimeout)
	if err != nil {
		s.log.Warn("session socket dial failed", "session", id, "error", err)
		writeAPIError(w, http.StatusBadGateway, "session_unreachable",
			"The session did not accept the connection")
		return
	}
	client, err := handshake.Upgrade(w)
	if err != nil {
		_ = upstream.Close()
		s.log.Warn("session socket upgrade failed", "session", id, "error", err)
		return
	}
	s.log.Info("session socket opened", "session", id)
	splice(client, upstream)
}

func (s *apiServer) writeRouteError(w http.ResponseWriter, id string, err error) {
	switch {
	case errors.Is(err, errSessionUnknown):
		writeAPIError(w, http.StatusNotFound, "unknown_session", "No such session")
	case errors.Is(err, errSessionRefusing):
		writeAPIError(w, http.StatusConflict, "session_not_accepting",
			"The session is full or ending and is not accepting players")
	case errors.Is(err, errSessionUnroutable):
		w.Header().Set("Retry-After", "5")
		writeAPIError(w, http.StatusServiceUnavailable, "session_unreachable",
			"The session has no reachable pod")
	default:
		s.log.Error("route session", "session", id, "error", err)
		writeAPIError(w, http.StatusBadGateway, "kubernetes_error", "Could not resolve the session")
	}
}
