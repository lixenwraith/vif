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
	requested sessionRequest
	bounds    fleetLimits
	podIP     string
	createErr error
	listErr   error
	readyErr  error
	routeErr  error
}

func (f *fakeSessionAllocator) createSession(_ context.Context, req sessionRequest) (session, error) {
	f.requested = req
	return f.created, f.createErr
}

func (f *fakeSessionAllocator) limits() fleetLimits { return f.bounds }

func (f *fakeSessionAllocator) listSessions(context.Context) ([]session, error) {
	return f.sessions, f.listErr
}

func (f *fakeSessionAllocator) ready(context.Context) error { return f.readyErr }

func (f *fakeSessionAllocator) routeSession(context.Context, string) (string, error) {
	return f.podIP, f.routeErr
}

func testServer(allocator sessionAllocator) *apiServer {
	return newAPIServer(allocator, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, allocatorConfig{})
}

// testWebServer publishes the browser route, which an ordinary test server does
// not: the two halves of the fleet are refusable independently.
func testWebServer(allocator sessionAllocator) *apiServer {
	return newAPIServer(allocator, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		allocatorConfig{WebOrigin: "https://site.example", WebMaxPerSession: 2})
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

	testServer(&fakeSessionAllocator{
		bounds: fleetLimits{PlayersMax: 4, LogLevels: []string{"info"},
			Scenarios: []string{"main"}},
	}).ServeHTTP(response, request)

	want := `{"sessions":[],"limits":{"players_max":4,"log_levels":["info"],"scenarios":["main"]}}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

// TestPostSessionCarriesTheChoicesItNames pins the one thing a caller may select. A
// field the allocator dropped would be a session silently unlike the one the page
// offered, which is worse than a refusal.
func TestPostSessionCarriesTheChoicesItNames(t *testing.T) {
	backend := &fakeSessionAllocator{created: session{ID: "abc", Port: 31700}}
	request := httptest.NewRequest(http.MethodPost, "/vif/api/sessions",
		strings.NewReader(`{"players":2,"log_level":"debug"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	testServer(backend).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if backend.requested != (sessionRequest{Players: 2, LogLevel: "debug"}) {
		t.Fatalf("the allocator was asked for %+v", backend.requested)
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

func TestTheBrowserRouteRefusesEverythingButAnUpgradeFromItsOrigin(t *testing.T) {
	const live = "/vif/ws/0123456789abcdef"
	upgrade := func(r *http.Request) *http.Request {
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", "keep-alive, Upgrade")
		r.Header.Set("Origin", "https://site.example")
		return r
	}
	for name, tc := range map[string]struct {
		request *http.Request
		want    int
	}{
		"a path that is not a session identifier": {
			upgrade(httptest.NewRequest(http.MethodGet, "/vif/ws/0123456789ABCDEF/extra", nil)), http.StatusNotFound},
		"an ordinary GET": {
			httptest.NewRequest(http.MethodGet, live, nil), http.StatusBadRequest},
		"another origin": {
			func() *http.Request {
				r := upgrade(httptest.NewRequest(http.MethodGet, live, nil))
				r.Header.Set("Origin", "https://elsewhere.example")
				return r
			}(), http.StatusForbidden},
		"a method the route does not answer": {
			upgrade(httptest.NewRequest(http.MethodPost, live, nil)), http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		testWebServer(&fakeSessionAllocator{podIP: "10.42.0.7"}).ServeHTTP(response, tc.request)
		if response.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, response.Code, tc.want)
		}
	}

	// A deployment that publishes no route answers the same path with its absence
	// rather than with a refusal a caller would retry.
	response := httptest.NewRecorder()
	testServer(&fakeSessionAllocator{}).ServeHTTP(response, upgrade(httptest.NewRequest(http.MethodGet, live, nil)))
	if response.Code != http.StatusNotImplemented {
		t.Errorf("unconfigured route status = %d", response.Code)
	}
}
