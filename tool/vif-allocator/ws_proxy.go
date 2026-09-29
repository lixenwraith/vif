package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/lixenwraith/vif/pkg/websocket"
)

// wsRoutePrefix has no version segment: this endpoint's scope is one session, and a
// session identifier is not a thing that gains a second shape.
const wsRoutePrefix = "/vif/ws/"

// A session identifier is 16 hex characters. Matching here rather than trusting the
// site keeps each route refusable on its own, and keeps it out of a label selector.
var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// wsRouter carries one browser connection to the session it names: it terminates
// the WebSocket and splices its payload to the pod's game port, as the front door
// does with a native dial.
type wsRouter struct {
	origin string
	holds  *holds
	game   string // the pod port dialled; a test points it at a stand-in
}

func newWSRouter(origin string, held *holds) *wsRouter {
	return &wsRouter{origin: origin, holds: held, game: gamePort}
}

// holds bounds the connections one session carries through this allocator, over
// the front door and the browser route alike. The pod's per-address limiter cannot:
// every proxied participant reaches it from the node.
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

// handleSessionSocket refuses everything it can before the upgrade, because after it
// there is no status code left to send, then splices the socket to the pod. The pod
// reads the browser's own route frame and refuses a name not its own.
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

	lookup, cancel := context.WithTimeout(r.Context(), routeTimeout)
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

	handshake, err := websocket.Check(w, r)
	if err != nil {
		refused := err.(*websocket.RequestError)
		writeAPIError(w, refused.Status, "bad_handshake", capitalize(refused.Reason))
		return
	}
	// Dialled before the 101, so an unreachable pod is still a status.
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
