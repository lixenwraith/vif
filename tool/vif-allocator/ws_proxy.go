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
// front door keeps the route refusable on its own.
var wsSessionPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

type upstreamKey struct{}

// wsRouter proxies one browser connection to the session it names. It terminates
// nothing: the sidecar in the pod owns the WebSocket, and these bytes are copied
// between two connections that have already agreed on what they are.
type wsRouter struct {
	origin string
	max    int
	proxy  *httputil.ReverseProxy

	mu    sync.Mutex
	holds map[string]int
}

func newWSRouter(origin string, max int, logger *slog.Logger) *wsRouter {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	// One connection per player, held for the match. Pooling would only risk
	// handing the next player a connection the last session had not finished
	// tearing down.
	transport.DisableKeepAlives = true
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = wsHandshakeTimeout

	router := &wsRouter{origin: origin, max: max, holds: make(map[string]int)}
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

// hold bounds the browser connections one session may carry. The game's own
// per-address admission limiter cannot do this here: every browser player reaches
// the pod from the same loopback address, so the per-player bound is this one.
func (r *wsRouter) hold(id string) (func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holds[id] >= r.max {
		return nil, false
	}
	r.holds[id]++
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.holds[id] <= 1 {
			delete(r.holds, id)
			return
		}
		r.holds[id]--
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
	if !wsSessionPattern.MatchString(id) {
		writeAPIError(w, http.StatusNotFound, "unknown_session", "No such session")
		return
	}
	if !isUpgradeRequest(r) {
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

	release, ok := s.ws.hold(id)
	if !ok {
		w.Header().Set("Retry-After", "5")
		writeAPIError(w, http.StatusServiceUnavailable, "session_busy",
			"This session is carrying as many browser players as it accepts")
		return
	}
	defer release()

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

// isUpgradeRequest reads the two hop-by-hop headers that make a request an
// upgrade. Connection is a comma list, so a token match is the only correct read.
func isUpgradeRequest(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, token := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
			return true
		}
	}
	return false
}
