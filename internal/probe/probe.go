// Package probe serves a run's health and metrics over HTTP.
//
// It exists because a supervised process has to be able to answer two questions
// from outside itself: should it still be running, and what is it doing. A terminal
// game answers both by being looked at; a dedicated host has nobody looking, and
// its status bar, its periodic summary and its metric registry are all inside a
// process nothing can reach.
//
// There is one health path, not a liveness one and a readiness one. They were
// separate while the fleet routed players through a Service that had to stop
// selecting a full pod; an allocated session is one pod behind one endpoint an
// allocator hands out directly, so the routing decision moved to the allocator and
// the admission decision was always the application's own — a dial to a session
// that cannot take it is refused by the handshake, not by a load balancer. What is
// left is one code that says whether to restart this process, and a body that says
// everything else, including whether a dial would currently be admitted.
//
// The server is deliberately small and stdlib-only. It holds no state of its own:
// a Snapshot function supplies the run's answer and a status registry supplies the
// metrics, so the run decides what its words mean and this only decides how to say
// them.
package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/status"
)

// shutdownGrace bounds how long Close waits for in-flight probe requests. A probe
// is a read of values already in memory, so anything still running past this is
// not a slow answer but a stuck one, and a shutdown may not wait on it.
const shutdownGrace = time.Second

// readHeaderTimeout bounds the pre-request phase. The endpoint is reachable by
// whatever can route to the pod, and a connection that opens and never completes
// its headers is the cheapest way to hold one.
const readHeaderTimeout = 5 * time.Second

// Snapshot is what a run reports about itself.
//
// Live is the only field the HTTP code carries: it is "should this process still
// be running", and nothing else is a restart. Ready is "would a dial be admitted
// right now", which an allocator reads from the body when it is choosing where to
// send a player — a full or draining session is not broken, and answering it with
// a failure code would say it was. Reason is for whoever reads the body.
type Snapshot struct {
	Live   bool
	Ready  bool
	Reason string

	// Detail is optional context rendered under the reason — the tick, the roster,
	// the address. Ordered by key so two reads of one state read the same.
	Detail map[string]string
}

// Server is the probe endpoint. The zero value is not usable; call New.
type Server struct {
	addr     string
	snapshot func() Snapshot
	registry *status.Registry

	http    *http.Server
	ln      net.Listener
	started atomic.Bool
}

// New builds a probe server. snapshot is required; registry may be nil, in which
// case /metrics reports nothing rather than failing — a run without a registry is
// one with nothing to report, not one that is broken.
func New(addr string, snapshot func() Snapshot, registry *status.Registry) (*Server, error) {
	if addr == "" {
		return nil, errors.New("probe: no bind address")
	}
	if snapshot == nil {
		return nil, errors.New("probe: no snapshot source")
	}
	s := &Server{addr: addr, snapshot: snapshot, registry: registry}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	return s, nil
}

// Start binds and serves. It binds synchronously so a port already in use is a
// startup error rather than a probe that silently never answers.
func (s *Server) Start() error {
	if !s.started.CompareAndSwap(false, true) {
		return nil
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.started.Store(false)
		return fmt.Errorf("probe listen %s: %w", s.addr, err)
	}
	s.ln = ln
	go func() {
		// ErrServerClosed is Close doing its job; anything else has already
		// stopped the listener, and the probe going quiet is what a supervisor
		// will see either way.
		_ = s.http.Serve(ln)
	}()
	return nil
}

// Addr is the bound address, empty before Start. It resolves a :0 port, which is
// what a test binds.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close stops serving. Safe on a server that never started.
func (s *Server) Close() error {
	if s == nil || !s.started.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// handleHealth answers the one health question. The code is the verdict a
// supervisor acts on; the body is everything a person or an allocator wants.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	snap := s.snapshot()
	s.writeSnapshot(w, snap, snap.Live)
}

// writeSnapshot renders one answer. The body is for a reader; the code is for the
// supervisor.
func (s *Server) writeSnapshot(w http.ResponseWriter, snap Snapshot, ok bool) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if ok {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	var b strings.Builder
	b.WriteString("live=")
	b.WriteString(strconv.FormatBool(snap.Live))
	b.WriteString(" ready=")
	b.WriteString(strconv.FormatBool(snap.Ready))
	if snap.Reason != "" {
		b.WriteString(" reason=")
		b.WriteString(snap.Reason)
	}
	b.WriteByte('\n')
	for _, k := range sortedKeys(snap.Detail) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(snap.Detail[k])
		b.WriteByte('\n')
	}
	_, _ = w.Write([]byte(b.String()))
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
