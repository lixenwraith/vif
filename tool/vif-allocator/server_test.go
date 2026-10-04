package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/network"
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
	return newAPIServer(allocator, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, allocatorConfig{}, nil, nil)
}

// testWebServer publishes the browser route, which an ordinary test server does
// not: the two halves of the fleet are refusable independently.
func testWebServer(allocator sessionAllocator) *apiServer {
	return newAPIServer(allocator, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		allocatorConfig{WebOrigin: "https://site.example"}, newHolds(2), nil)
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

// TestGetSessionsNamesScenarioDescriptionsBesideTheList keeps the list a list of
// names: descriptions are an object beside it, absent when there are none above.
func TestGetSessionsNamesScenarioDescriptionsBesideTheList(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/vif/api/sessions", nil)
	response := httptest.NewRecorder()

	testServer(&fakeSessionAllocator{
		bounds: fleetLimits{PlayersMax: 4, LogLevels: []string{"info"},
			Scenarios: []string{"main", "td"}, Descriptions: map[string]string{"td": "Hold the line."}},
	}).ServeHTTP(response, request)

	want := `"scenarios":["main","td"],"scenario_descriptions":{"td":"Hold the line."}}`
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
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

// TestATerminalIsHandedTheLinkItAskedFor holds vif's own client to this handler: a
// created session comes back as the front door's link, and a refusal as its message.
func TestATerminalIsHandedTheLinkItAskedFor(t *testing.T) {
	backend := &fakeSessionAllocator{created: session{ID: "abc", JoinTarget: "vif://site.example:7777/abc"}}
	site := httptest.NewServer(testServer(backend))
	defer site.Close()
	link, err := network.RequestSession(site.URL, 2, "main")
	if err != nil || link != backend.created.JoinTarget {
		t.Fatalf("RequestSession = %q, %v; want %q", link, err, backend.created.JoinTarget)
	}
	if backend.requested != (sessionRequest{Players: 2, Scenario: "main"}) {
		t.Fatalf("the allocator was asked for %+v", backend.requested)
	}
	backend.createErr = errFleetFull
	if _, err := network.RequestSession(site.URL, 0, ""); err == nil || err.Error() != "All session ports are allocated" {
		t.Fatalf("a refusal returned %v", err)
	}
}

// TestSessionCreationIsBudgetedPerAddress: one address holds at most its budget of
// the fleet's unclaimed sessions, another is unaffected, and a request the edge did
// not attribute is bounded by the edge alone.
func TestSessionCreationIsBudgetedPerAddress(t *testing.T) {
	api := newAPIServer(&fakeSessionAllocator{created: session{ID: "abc", Port: 31700}},
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil, allocatorConfig{
			Workload: workloadConfig{FirstJoin: "90s"}, ClientAddressHeader: "X-Real-IP", ClientCreates: 1,
		}, nil, nil)
	for i, tc := range []struct {
		player string
		want   int
	}{
		{"192.0.2.1", http.StatusCreated},
		{"192.0.2.1", http.StatusTooManyRequests},
		{"192.0.2.2", http.StatusCreated},
		{"", http.StatusCreated},
	} {
		request := httptest.NewRequest(http.MethodPost, "/vif/api/sessions", nil)
		if tc.player != "" {
			request.Header.Set("X-Real-IP", tc.player)
		}
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Errorf("request %d from %q answered %d, want %d", i+1, tc.player, response.Code, tc.want)
		}
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

// TestTheBrowserRouteRefusesEverythingButAnUpgradeFromItsOrigin: each refusal is a
// status answered before any 101, the allocator's own handshake checks and pod dial
// included.
func TestTheBrowserRouteRefusesEverythingButAnUpgradeFromItsOrigin(t *testing.T) {
	const live = "/vif/ws/" + routedID
	upgrade := func(method, path string, edits ...string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", "keep-alive, Upgrade")
		r.Header.Set("Origin", "https://site.example")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		for i := 0; i+1 < len(edits); i += 2 {
			r.Header.Set(edits[i], edits[i+1])
		}
		return r
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, refusingPort, _ := net.SplitHostPort(closed.Addr().String())
	_ = closed.Close()

	for name, tc := range map[string]struct {
		request  *http.Request
		routeErr error
		held     int
		want     int
	}{
		"a path that is not a session identifier": {
			request: upgrade(http.MethodGet, "/vif/ws/0123456789ABCDEF/extra"), want: http.StatusNotFound},
		"an ordinary GET": {request: httptest.NewRequest(http.MethodGet, live, nil), want: http.StatusBadRequest},
		"another origin": {
			request: upgrade(http.MethodGet, live, "Origin", "https://elsewhere.example"), want: http.StatusForbidden},
		"a method the route does not answer": {
			request: upgrade(http.MethodPost, live), want: http.StatusMethodNotAllowed},
		"an unknown session": {
			request: upgrade(http.MethodGet, live), routeErr: errSessionUnknown, want: http.StatusNotFound},
		"a session not accepting players": {request: upgrade(http.MethodGet, live),
			routeErr: fmt.Errorf("%w: occupied", errSessionRefusing), want: http.StatusConflict},
		"a session with no reachable pod": {request: upgrade(http.MethodGet, live),
			routeErr: errSessionUnroutable, want: http.StatusServiceUnavailable},
		"a session at -route-max": {request: upgrade(http.MethodGet, live), held: 2,
			want: http.StatusServiceUnavailable},
		"another WebSocket version": {
			request: upgrade(http.MethodGet, live, "Sec-WebSocket-Version", "8"), want: http.StatusUpgradeRequired},
		"a key that is not 16 bytes": {
			request: upgrade(http.MethodGet, live, "Sec-WebSocket-Key", "AAAA"), want: http.StatusBadRequest},
		"a pod that refuses the dial": {request: upgrade(http.MethodGet, live), want: http.StatusBadGateway},
	} {
		server := testWebServer(&fakeSessionAllocator{podIP: "127.0.0.1", routeErr: tc.routeErr})
		server.ws.game = refusingPort
		for range tc.held {
			server.ws.holds.hold(routedID)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, tc.request)
		if response.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, response.Code, tc.want)
		}
	}

	// A deployment that publishes no route answers the same path with its absence
	// rather than with a refusal a caller would retry.
	response := httptest.NewRecorder()
	testServer(&fakeSessionAllocator{}).ServeHTTP(response, upgrade(http.MethodGet, live))
	if response.Code != http.StatusNotImplemented {
		t.Errorf("unconfigured route status = %d", response.Code)
	}
}
