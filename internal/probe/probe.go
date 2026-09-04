// Package probe serves a run's liveness, readiness and metrics over HTTP.
//
// It exists because a supervised process has to be able to answer three questions
// from outside itself: is it alive, may it be sent work, and what is it doing. A
// terminal game answers all three by being looked at; a dedicated host has nobody
// looking, and its status bar, its periodic summary and its metric registry are
// all inside a process nothing can reach.
//
// The server is deliberately small and stdlib-only. It holds no state of its own:
// a Snapshot function supplies the run's answer to the first two questions and a
// status registry supplies the third, so the run decides what "ready" means and
// this only decides how to say it.
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
// Live and Ready are separate because they fail for different reasons and want
// different responses: a run that is not live should be restarted, and one that is
// merely not ready should stop being sent participants. Reason is for the person
// reading the probe's body, never for the orchestrator, which reads only the code.
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
	mux.HandleFunc("/healthz", s.handleLive)
	mux.HandleFunc("/readyz", s.handleReady)
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

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	snap := s.snapshot()
	s.writeSnapshot(w, snap, snap.Live)
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	snap := s.snapshot()
	// Readiness implies liveness: a run that is not live is not ready either,
	// whatever it thinks of its own roster.
	s.writeSnapshot(w, snap, snap.Live && snap.Ready)
}

// writeSnapshot answers one probe. The body is for a person; the code is the
// answer.
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
