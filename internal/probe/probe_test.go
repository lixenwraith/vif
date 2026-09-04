package probe

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/status"
)

func serve(t *testing.T, snap func() Snapshot, reg *status.Registry) string {
	t.Helper()
	s, err := New("127.0.0.1:0", snap, reg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return "http://" + s.Addr()
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// TestTheProbesAnswerSeparately is the distinction the endpoint exists to make.
// A run that is not live should be restarted; one that is merely not ready should
// stop being sent participants, and answering both with one code would conflate
// "this pod is broken" with "this match is full".
func TestTheProbesAnswerSeparately(t *testing.T) {
	t.Parallel()
	var state atomic.Pointer[Snapshot]
	state.Store(&Snapshot{Live: true, Ready: true})
	base := serve(t, func() Snapshot { return *state.Load() }, nil)

	if code, _ := get(t, base+"/healthz"); code != http.StatusOK {
		t.Fatalf("live run answered /healthz with %d", code)
	}
	if code, _ := get(t, base+"/readyz"); code != http.StatusOK {
		t.Fatalf("ready run answered /readyz with %d", code)
	}

	// Full, but healthy: the Service must stop routing, the orchestrator must not
	// restart.
	state.Store(&Snapshot{Live: true, Ready: false, Reason: "session at capacity"})
	if code, _ := get(t, base+"/healthz"); code != http.StatusOK {
		t.Fatalf("a full but live run answered /healthz with %d, want 200", code)
	}
	code, body := get(t, base+"/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("a full run answered /readyz with %d, want 503", code)
	}
	if !strings.Contains(body, "session at capacity") {
		t.Fatalf("the body did not carry the reason: %q", body)
	}

	// Stalled: not live, and therefore not ready whatever it thinks of its roster.
	state.Store(&Snapshot{Live: false, Ready: true, Reason: "tick stalled"})
	if code, _ := get(t, base+"/healthz"); code != http.StatusServiceUnavailable {
		t.Fatalf("a stalled run answered /healthz with %d, want 503", code)
	}
	if code, _ := get(t, base+"/readyz"); code != http.StatusServiceUnavailable {
		t.Fatal("a run that is not live reported itself ready")
	}
}

// TestMetricsRenderTheRegistry covers the exposition. It is a rendering of state
// that already exists, so the test that matters is that every kind of cell reaches
// the output and that the names it produces are ones a scrape will accept.
func TestMetricsRenderTheRegistry(t *testing.T) {
	t.Parallel()
	reg := status.NewRegistry()
	reg.Ints.Get("network.peers").Store(3)
	reg.Bools.Get("drain.paused").Store(true)
	reg.Floats.Get("snapshot.cadence.ticks").Set(4.5)
	reg.Strings.Get("fsm.main.state").Store("MainEscalate")

	base := serve(t, func() Snapshot { return Snapshot{Live: true, Ready: true} }, reg)
	code, body := get(t, base+"/metrics")
	if code != http.StatusOK {
		t.Fatalf("/metrics answered %d", code)
	}

	for _, want := range []string{
		"vif_network_peers 3",
		"vif_drain_paused 1",
		"vif_snapshot_cadence_ticks 4.5",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("exposition is missing %q:\n%s", want, body)
		}
	}
	// Strings are states rather than numbers; their exposition is a label set this
	// does not model, so they are omitted rather than rendered as something false.
	if strings.Contains(body, "MainEscalate") {
		t.Fatal("a string cell reached the exposition")
	}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		name, _, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("line is not a sample: %q", line)
		}
		if !validMetricName(name) {
			t.Fatalf("%q is not a name a scrape accepts", name)
		}
	}
}

// TestMetricsSurviveAnAbsentRegistry: a run with nothing to report is not a run
// that is broken, so the endpoint answers empty rather than failing.
func TestMetricsSurviveAnAbsentRegistry(t *testing.T) {
	t.Parallel()
	base := serve(t, func() Snapshot { return Snapshot{Live: true} }, nil)
	if code, body := get(t, base+"/metrics"); code != http.StatusOK || body != "" {
		t.Fatalf("absent registry answered %d %q", code, body)
	}
}

func validMetricName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r == ':',
			r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
