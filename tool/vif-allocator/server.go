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
	"net/http"
	"time"
)

const maxCreateBody = 1024

type sessionAllocator interface {
	createSession(context.Context) (session, error)
	listSessions(context.Context) ([]session, error)
	ready(context.Context) error
}

type apiServer struct {
	allocator sessionAllocator
	log       *slog.Logger
	mux       *http.ServeMux
}

type sessionsResponse struct {
	Sessions []session `json:"sessions"`
}

type errorResponse struct {
	Error apiErrorResponse `json:"error"`
}

type apiErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newAPIServer(allocator sessionAllocator, logger *slog.Logger) *apiServer {
	server := &apiServer{allocator: allocator, log: logger, mux: http.NewServeMux()}
	server.mux.HandleFunc("/healthz", server.handleHealth)
	server.mux.HandleFunc("/readyz", server.handleReady)
	server.mux.HandleFunc("/vif/api/sessions", server.handleSessions)
	server.mux.HandleFunc("/vif/api/logs", server.handleLogs)
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
		sessions, err := s.allocator.listSessions(r.Context())
		if err != nil {
			s.log.Error("list sessions", "error", err)
			writeAPIError(w, http.StatusBadGateway, "kubernetes_error", "Could not read sessions")
			return
		}
		if sessions == nil {
			sessions = []session{}
		}
		writeJSON(w, http.StatusOK, sessionsResponse{Sessions: sessions})
	case http.MethodPost:
		if err := validateCreateBody(r); err != nil {
			var bodyErr *requestBodyError
			if errors.As(err, &bodyErr) {
				writeAPIError(w, bodyErr.status, bodyErr.code, bodyErr.message)
				return
			}
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "Request body must be an empty JSON object")
			return
		}
		created, err := s.allocator.createSession(r.Context())
		if err != nil {
			s.writeCreateError(w, err)
			return
		}
		s.log.Info("session created", "session", created.ID, "port", created.Port)
		writeJSON(w, http.StatusCreated, created)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (s *apiServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeAPIError(w, http.StatusNotImplemented, "log_stream_not_configured", "The LogWisp stream is not configured")
}

func (s *apiServer) writeCreateError(w http.ResponseWriter, err error) {
	switch {
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

func validateCreateBody(r *http.Request) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxCreateBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxCreateBody {
		return &requestBodyError{status: http.StatusRequestEntityTooLarge, code: "request_too_large", message: "Request body is too large"}
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &requestBodyError{status: http.StatusUnsupportedMediaType, code: "content_type", message: "Content-Type must be application/json"}
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil || len(object) != 0 {
		return fmt.Errorf("body is not an empty JSON object")
	}
	return nil
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
