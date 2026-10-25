package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/network"
)

const maxCreateBody = 1024

// listFresh is how long one reading of the fleet answers every list request. Each
// costs the API server four lists and every running pod a probe, which every open
// lobby page polling every few seconds would otherwise pay; listTimeout bounds it.
const (
	listFresh   = 2 * time.Second
	listTimeout = 10 * time.Second
)

type sessionAllocator interface {
	createSession(context.Context, sessionRequest) (session, error)
	listSessions(context.Context) ([]session, error)
	routeSession(context.Context, string) (string, error)
	limits() fleetLimits
	ready(context.Context) error
}

type apiServer struct {
	allocator sessionAllocator
	log       *slog.Logger
	logProxy  *httputil.ReverseProxy
	ws        *wsRouter
	mux       *http.ServeMux

	clientHeader string
	unattributed atomic.Bool // warned once that the edge sent no address
	creates      *network.AdmissionLimiter
	list         fleetList
}

// fleetList is the latest reading of the fleet and the one replacing it, which
// every request arriving meanwhile waits on rather than starting its own.
type fleetList struct {
	mu   sync.Mutex
	at   time.Time
	last *fleetRead
	read *fleetRead
	gen  uint64 // a create's, so a reading begun before it is not kept
}

type fleetRead struct {
	done     chan struct{}
	gen      uint64
	sessions []session
	limits   fleetLimits
	err      error
}

type sessionsResponse struct {
	Sessions []session   `json:"sessions"`
	Limits   fleetLimits `json:"limits"`
}

type errorResponse struct {
	Error apiErrorResponse `json:"error"`
}

type apiErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newAPIServer(allocator sessionAllocator, logger *slog.Logger, logStreamURL *url.URL, cfg allocatorConfig,
	held *holds, joins *network.AdmissionLimiter) *apiServer {
	server := &apiServer{allocator: allocator, log: logger, mux: http.NewServeMux(),
		clientHeader: cfg.ClientAddressHeader}
	if logStreamURL != nil {
		server.logProxy = newLogStreamProxy(logStreamURL, logger)
	}
	if cfg.WebOrigin != "" {
		server.ws = &wsRouter{origin: cfg.WebOrigin, route: newTCPRouter(allocator, held, joins, logger)}
	}
	// A session nobody joins expires after -first-join, so this bounds how many of
	// the fleet's unclaimed sessions one address can hold.
	if window, err := time.ParseDuration(cfg.Workload.FirstJoin); err == nil && cfg.ClientCreates > 0 {
		server.creates = network.NewAdmissionLimiterOf(window, cfg.ClientCreates)
	}
	server.mux.HandleFunc("/healthz", server.handleHealth)
	server.mux.HandleFunc("/readyz", server.handleReady)
	server.mux.HandleFunc("/vif/api/sessions", server.handleSessions)
	server.mux.HandleFunc("/vif/api/logs", server.handleLogs)
	server.mux.HandleFunc(wsRoutePrefix, server.handleSessionSocket)
	return server
}

func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.mux.ServeHTTP(w, r)
}

func (s *apiServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, "ok\n")
	}
}

func (s *apiServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.allocator.ready(ctx); err != nil {
		s.log.Warn("readiness failed", "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes_unavailable", "Kubernetes API is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, "ok\n")
	}
}

func (s *apiServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions, limits, err := s.fleet(r.Context())
		if err != nil {
			s.log.Error("list sessions", "error", err)
			writeAPIError(w, http.StatusBadGateway, "kubernetes_error", "Could not read sessions")
			return
		}
		writeJSON(w, http.StatusOK, sessionsResponse{Sessions: sessions, Limits: limits})
	case http.MethodPost:
		request, err := parseCreateRequest(r)
		if err != nil {
			var bodyErr *requestBodyError
			if errors.As(err, &bodyErr) {
				writeAPIError(w, bodyErr.status, bodyErr.code, bodyErr.message)
				return
			}
			writeAPIError(w, http.StatusBadRequest, "invalid_request",
				"Request body must be a JSON object naming only players, bots, log_level, scenario and size")
			return
		}
		from := s.clientAddr(r)
		if s.creates != nil && s.creates.Admit(from) != nil {
			wait := s.creates.Wait(from)
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
			writeAPIError(w, http.StatusTooManyRequests, "create_budget",
				"This address has created as many sessions as it may for now; join one or try again later")
			return
		}
		created, err := s.allocator.createSession(r.Context(), request)
		if err != nil {
			// Charged first, so one address has one create under way; a refused one
			// started nothing and is given back, so a full fleet's retries cost none.
			if s.creates != nil && (errors.Is(err, errRequestRefused) || errors.Is(err, errFleetFull)) {
				s.creates.Refund(from)
			}
			s.writeCreateError(w, err)
			return
		}
		s.forgetFleet()
		s.log.Info("session created", "session", created.ID, "port", created.Port,
			"players", request.Players, "bots", request.Bots, "log_level", request.LogLevel,
			"scenario", request.Scenario, "size", request.Size)
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

// fleet is the last reading, aged to now, while it is fresh, and otherwise the
// reading under way, which the first request to need one starts.
func (s *apiServer) fleet(ctx context.Context) ([]session, fleetLimits, error) {
	l := &s.list
	l.mu.Lock()
	if age := time.Since(l.at); l.last != nil && age < listFresh {
		last := l.last
		l.mu.Unlock()
		return aged(last.sessions, age), last.limits, nil
	}
	read := l.read
	if read == nil {
		read = &fleetRead{done: make(chan struct{}), gen: l.gen}
		l.read = read
		go s.readFleet(read)
	}
	l.mu.Unlock()
	select {
	case <-read.done:
		return aged(read.sessions, 0), read.limits, read.err
	case <-ctx.Done():
		return nil, fleetLimits{}, ctx.Err()
	}
}

// readFleet reads for every request waiting on read, detached from each of them:
// the one that started it leaving must not cancel it for the rest.
func (s *apiServer) readFleet(read *fleetRead) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	read.sessions, read.err = s.allocator.listSessions(ctx)
	cancel()
	read.limits = s.allocator.limits()
	l := &s.list
	l.mu.Lock()
	if l.read == read {
		l.read = nil
	}
	if read.err == nil && read.gen == l.gen {
		l.last, l.at = read, time.Now()
	}
	l.mu.Unlock()
	close(read.done)
}

// forgetFleet drops a reading a create made stale, and keeps any under way from
// being kept, so the next list a page reads carries the session it was handed.
func (s *apiServer) forgetFleet() {
	s.list.mu.Lock()
	s.list.gen++
	s.list.last, s.list.read = nil, nil
	s.list.mu.Unlock()
}

// aged is a reading as old as it is, each expiry counted down by its age: a page
// pins expires_in to its own clock as it receives it.
func aged(sessions []session, age time.Duration) []session {
	out := make([]session, len(sessions))
	copy(out, sessions)
	for i := range out {
		if left, err := time.ParseDuration(out[i].State.ExpiresIn); err == nil && age > 0 {
			out[i].State.ExpiresIn = max(left-age, 0).Round(time.Second).String()
		}
	}
	return out
}

// clientAddr is the player's address as the edge wrote it, nil when no header is
// configured or the request carries no single address. Nil charges no budget: keyed
// on the edge's own address, it would be one budget for every player.
func (s *apiServer) clientAddr(r *http.Request) net.Addr {
	if s.clientHeader == "" {
		return nil
	}
	if values := r.Header.Values(s.clientHeader); len(values) == 1 {
		if ip, err := netip.ParseAddr(strings.TrimSpace(values[0])); err == nil {
			return net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip.Unmap(), 0))
		}
	}
	if !s.unattributed.Swap(true) {
		s.log.Warn("a request carried no client address; the edge's limits alone bound it",
			"header", s.clientHeader)
	}
	return nil
}

func (s *apiServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
		return
	}
	if s.logProxy == nil {
		writeAPIError(w, http.StatusNotImplemented, "log_stream_not_configured", "The LogWisp stream is not configured")
		return
	}

	// Successful responses must retain LogWisp's cache and stream headers rather
	// than the router's finite-API defaults.
	w.Header().Del("Cache-Control")
	w.Header().Del("X-Content-Type-Options")

	// A HEAD names the route, and this route is only a body. Upstream refuses one
	// because the client it would register never reads, so the probe is answered
	// here without opening a stream. A GET is what reports availability.
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		return
	}

	// The server keeps a finite WriteTimeout for create/list/health responses.
	// Clear it only for this long-lived SSE response; request cancellation and
	// router shutdown still cancel the upstream request.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	s.logProxy.ServeHTTP(w, r)
}

func (s *apiServer) writeCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRequestRefused):
		// The bound is what the caller needs, and it is the same bound the session
		// list advertises, so stating it tells an anonymous caller nothing new.
		writeAPIError(w, http.StatusBadRequest, "invalid_request", capitalize(unwrapRefusal(err)))
	case errors.Is(err, errFleetFull):
		w.Header().Set("Retry-After", "10")
		writeAPIError(w, http.StatusServiceUnavailable, "fleet_full", "All session ports are allocated")
	case errors.Is(err, errSessionNotReady):
		s.log.Error("session readiness failed", "error", err)
		writeAPIError(w, http.StatusGatewayTimeout, "session_not_ready", "The session did not become ready and was removed")
	case errors.Is(err, context.Canceled):
		s.log.Info("session request canceled")
		writeAPIError(w, http.StatusRequestTimeout, "request_canceled", "The request was canceled and the session was removed")
	default:
		s.log.Error("create session", "error", err)
		writeAPIError(w, http.StatusBadGateway, "kubernetes_error", "Could not create the session")
	}
}

type requestBodyError struct {
	status  int
	code    string
	message string
}

func (e *requestBodyError) Error() string { return e.message }

// parseCreateRequest reads the optional session choices. An empty body and `{}` are
// both the deployment default, which is what every caller sent before there was
// anything to choose; an unknown field is refused rather than ignored, so a caller
// misspelling one is told instead of silently served a default.
func parseCreateRequest(r *http.Request) (sessionRequest, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxCreateBody+1))
	if err != nil {
		return sessionRequest{}, err
	}
	if len(data) > maxCreateBody {
		return sessionRequest{}, &requestBodyError{status: http.StatusRequestEntityTooLarge, code: "request_too_large", message: "Request body is too large"}
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return sessionRequest{}, nil
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return sessionRequest{}, &requestBodyError{status: http.StatusUnsupportedMediaType, code: "content_type", message: "Content-Type must be application/json"}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request sessionRequest
	if err := decoder.Decode(&request); err != nil {
		return sessionRequest{}, fmt.Errorf("body is not a session request: %w", err)
	}
	if decoder.More() {
		return sessionRequest{}, fmt.Errorf("body carries more than one JSON value")
	}
	return request, nil
}

// unwrapRefusal drops the sentinel prefix so the reason reaches the caller without
// the wrapper it was matched on.
func unwrapRefusal(err error) string {
	return strings.TrimPrefix(err.Error(), errRequestRefused.Error()+": ")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: apiErrorResponse{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
