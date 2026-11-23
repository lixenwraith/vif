package main

import (
	"net/http"
	"regexp"
	"slices"
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
// the WebSocket and places its payload with the front door's own placement, so the
// two routes share a budget, a ceiling and their refusals. origin is the site's,
// whose page signs in by its cookie; listed are the others the route answers.
type wsRouter struct {
	origin string
	listed []string
	route  *tcpRouter
}

// holds bounds the connections one session carries through this router, over
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

// handleSessionSocket answers before the upgrade only what HTTP must: the route,
// the upgrade and its origin. After the 101 the stream is placed as the front door
// places one, so a refusal is the join reply either client reads rather than a
// status a page is never shown.
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
	origin := r.Header.Get("Origin")
	if origin != s.ws.origin && !slices.Contains(s.ws.listed, origin) {
		writeAPIError(w, http.StatusForbidden, "origin_refused",
			"This session route does not answer this origin")
		return
	}
	handshake, err := websocket.Check(w, r)
	if err != nil {
		refused := err.(*websocket.RequestError)
		writeAPIError(w, refused.Status, "bad_handshake", capitalize(refused.Reason))
		return
	}
	client, err := handshake.Upgrade(w)
	if err != nil {
		s.log.Warn("session socket upgrade failed", "session", id, "error", err)
		return
	}
	// The site's page signs in by the cookie the identity service set on the site's
	// origin, which rides the upgrade. A listed origin's upgrade may carry it too, and
	// is not that page: it is placed as a dial that shows no sign-in.
	signIn := ""
	if origin == s.ws.origin {
		signIn = signInOf(r)
	}
	s.ws.route.route(client, s.clientAddr(r), signIn)
}
