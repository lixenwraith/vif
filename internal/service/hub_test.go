package service

import (
	"strings"
	"testing"
)

// stubService is a Service with no side effects, used to pin Hub ordering
type stubService struct {
	name string
	deps []string
	log  *[]string
}

func (s *stubService) Name() string           { return s.name }
func (s *stubService) Dependencies() []string { return s.deps }
func (s *stubService) Init() error            { *s.log = append(*s.log, "init:"+s.name); return nil }
func (s *stubService) Start() error           { *s.log = append(*s.log, "start:"+s.name); return nil }
func (s *stubService) Stop() error            { *s.log = append(*s.log, "stop:"+s.name); return nil }

// newStubHub registers the named services in the given order; deps maps a name
// to the names it depends on
func newStubHub(t *testing.T, log *[]string, order []string, deps map[string][]string) *Hub {
	t.Helper()
	h := NewHub()
	for _, n := range order {
		if err := h.Register(&stubService{name: n, deps: deps[n], log: log}); err != nil {
			t.Fatalf("register %s: %v", n, err)
		}
	}
	return h
}

// TestHubInitOrderIsTopological asserts dependencies initialize before dependents
// and that registration order does not reach the result.
func TestHubInitOrderIsTopological(t *testing.T) {
	deps := map[string][]string{
		"render":  {"terminal", "content"},
		"audio":   {"content"},
		"network": nil,
	}
	want := "init:content init:network init:terminal init:audio init:render"

	// Registration order must not matter; both permutations resolve identically
	for _, order := range [][]string{
		{"render", "audio", "network", "terminal", "content"},
		{"content", "terminal", "network", "audio", "render"},
	} {
		var log []string
		h := newStubHub(t, &log, order, deps)
		if err := h.InitAll(); err != nil {
			t.Fatalf("InitAll: %v", err)
		}
		if got := strings.Join(log, " "); got != want {
			t.Errorf("registration %v:\n got %s\nwant %s", order, got, want)
		}
	}
}

// TestHubInitOrderIsDeterministic asserts repeated resolution of the same set
// yields one order; Go randomizes map iteration, so an unsorted walk diverges here.
func TestHubInitOrderIsDeterministic(t *testing.T) {
	deps := map[string][]string{
		"c": {"a"}, "d": {"a"}, "e": {"b"}, "f": {"b"},
	}
	order := []string{"f", "e", "d", "c", "b", "a"}

	var first string
	for range 32 {
		var log []string
		h := newStubHub(t, &log, order, deps)
		if err := h.InitAll(); err != nil {
			t.Fatalf("InitAll: %v", err)
		}
		got := strings.Join(log, " ")
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("order varies between runs:\n %s\n %s", first, got)
		}
	}
	if want := "init:a init:b init:c init:d init:e init:f"; first != want {
		t.Errorf("got %s\nwant %s", first, want)
	}
}

// TestHubStartFollowsInitOrder asserts StartAll reuses the resolved order
func TestHubStartFollowsInitOrder(t *testing.T) {
	var log []string
	h := newStubHub(t, &log, []string{"b", "a"}, map[string][]string{"b": {"a"}})
	if err := h.InitAll(); err != nil {
		t.Fatalf("InitAll: %v", err)
	}
	log = log[:0]
	if err := h.StartAll(); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if got := strings.Join(log, " "); got != "start:a start:b" {
		t.Errorf("got %s", got)
	}
}

// TestHubStopReversesInitOrder asserts teardown unwinds initialization
func TestHubStopReversesInitOrder(t *testing.T) {
	var log []string
	h := newStubHub(t, &log, []string{"c", "b", "a"},
		map[string][]string{"c": {"b"}, "b": {"a"}})
	if err := h.InitAll(); err != nil {
		t.Fatalf("InitAll: %v", err)
	}
	log = log[:0]
	h.StopAll()
	if got := strings.Join(log, " "); got != "stop:c stop:b stop:a" {
		t.Errorf("got %s", got)
	}
}

// TestHubUnregisteredDependency asserts the error names both endpoints
func TestHubUnregisteredDependency(t *testing.T) {
	var log []string
	h := newStubHub(t, &log, []string{"a"}, map[string][]string{"a": {"ghost"}})
	err := h.InitAll()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"a", "ghost"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if len(log) != 0 {
		t.Errorf("services initialized despite the error: %v", log)
	}
}

// TestHubCircularDependency asserts a cycle is reported, not deadlocked or partially run
func TestHubCircularDependency(t *testing.T) {
	var log []string
	h := newStubHub(t, &log, []string{"a", "b", "c"},
		map[string][]string{"a": {"c"}, "b": {"a"}, "c": {"b"}})
	err := h.InitAll()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "circular") && !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not report a cycle", err)
	}
	if len(log) != 0 {
		t.Errorf("services initialized despite the cycle: %v", log)
	}
}
