package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSessionAllocator struct {
	created   session
	sessions  []session
	createErr error
	listErr   error
	readyErr  error
}

func (f *fakeSessionAllocator) createSession(context.Context) (session, error) {
	return f.created, f.createErr
}

func (f *fakeSessionAllocator) listSessions(context.Context) ([]session, error) {
	return f.sessions, f.listErr
}

func (f *fakeSessionAllocator) ready(context.Context) error { return f.readyErr }

func testServer(allocator sessionAllocator) *apiServer {
	return newAPIServer(allocator, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPostSession(t *testing.T) {
	backend := &fakeSessionAllocator{created: session{ID: "abc", Port: 31700, Routable: true}}
	request := httptest.NewRequest(http.MethodPost, "/vif/api/sessions", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	testServer(backend).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"id":"abc"`) || !strings.Contains(response.Body.String(), `"port":31700`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestPostSessionRejectsFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/vif/api/sessions", strings.NewReader(`{"image":"arbitrary"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	testServer(&fakeSessionAllocator{}).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPostSessionReportsFleetFull(t *testing.T) {
	backend := &fakeSessionAllocator{createErr: errFleetFull}
	request := httptest.NewRequest(http.MethodPost, "/vif/api/sessions", nil)
	response := httptest.NewRecorder()

	testServer(backend).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, headers = %v", response.Code, response.Header())
	}
	if !strings.Contains(response.Body.String(), `"code":"fleet_full"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestPostSessionReportsCancellation(t *testing.T) {
	backend := &fakeSessionAllocator{createErr: context.Canceled}
	request := httptest.NewRequest(http.MethodPost, "/vif/api/sessions", nil)
	response := httptest.NewRecorder()

	testServer(backend).ServeHTTP(response, request)

	if response.Code != http.StatusRequestTimeout || !strings.Contains(response.Body.String(), `"code":"request_canceled"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestGetSessionsAlwaysReturnsArray(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/vif/api/sessions", nil)
	response := httptest.NewRecorder()

	testServer(&fakeSessionAllocator{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"sessions":[]}` {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestLogEndpointIsExplicitlyDeferred(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/vif/api/logs", nil)
	response := httptest.NewRecorder()

	testServer(&fakeSessionAllocator{}).ServeHTTP(response, request)

	if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "log_stream_not_configured") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
