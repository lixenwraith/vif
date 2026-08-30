package filter

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
)

func match(f Filter, d logfile.Domain) bool {
	var c Ctx
	c.Reset(0, logfile.Meta{Dom: d})
	return f.Match(&c)
}

func TestDomainCyclesThroughThreeStates(t *testing.T) {
	f := NewDomain()
	want := []logfile.Domain{logfile.DomShared, logfile.DomPlayer, logfile.DomNone}
	for i, w := range want {
		f.Cycle()
		if f.State != w {
			t.Fatalf("cycle %d: state = %v, want %v", i+1, f.State, w)
		}
	}
}

func TestDomainBothAdmitsEverything(t *testing.T) {
	f := NewDomain()
	if f.Active() {
		t.Error("a fresh domain filter constrains something")
	}
	for _, d := range []logfile.Domain{logfile.DomNone, logfile.DomShared, logfile.DomPlayer} {
		if !match(f, d) {
			t.Errorf("both rejected %v", d)
		}
	}
	if f.Label() != "" {
		t.Errorf("Label() = %q, want empty while unconstrained", f.Label())
	}
}

func TestDomainSelectsExactlyOneScope(t *testing.T) {
	f := NewDomain()
	f.Set(logfile.DomPlayer)

	if !match(f, logfile.DomPlayer) {
		t.Error("player rejected its own domain")
	}
	if match(f, logfile.DomShared) {
		t.Error("player admitted a shared record")
	}
	// A record with no domain is not player state; a journal capture asked for
	// one scope should not smuggle the anchor back in.
	if match(f, logfile.DomNone) {
		t.Error("player admitted a record carrying no domain")
	}
	if got := f.Label(); got != "dom:player" {
		t.Errorf("Label() = %q, want dom:player", got)
	}
}

func TestDomainIsIndexOnly(t *testing.T) {
	// The scan resolves the domain, so selecting one must never read a line.
	if n := NewDomain().Needs(); n != 0 {
		t.Errorf("Needs() = %v, want 0", n)
	}
}

func TestDomainSpecParses(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want logfile.Domain
	}{
		{"both", logfile.DomNone},
		{"", logfile.DomNone},
		{"shared", logfile.DomShared},
		{" Player ", logfile.DomPlayer},
	} {
		f, err := New("dom", tc.arg)
		if err != nil {
			t.Fatalf("New(dom, %q): %v", tc.arg, err)
		}
		if got := f.(*Domain).State; got != tc.want {
			t.Errorf("New(dom, %q) state = %v, want %v", tc.arg, got, tc.want)
		}
	}
	if _, err := New("dom", "world"); err == nil {
		t.Error("New(dom, world) accepted an unknown scope")
	}
}

func TestDomainStacksWithTheRest(t *testing.T) {
	// The stack orders index-only predicates first; the domain filter must not
	// push a line read in front of the level check.
	var s Stack
	s.Add(NewLevel())
	s.Add(NewDomain())
	s.Add(NewFields(t))
	if s.Needs() != NeedFields {
		t.Fatalf("stack needs = %v, want NeedFields", s.Needs())
	}
}

// NewFields builds a fields filter for the stack-ordering test.
func NewFields(t *testing.T) Filter {
	t.Helper()
	f, err := New("fields", "spawn")
	if err != nil {
		t.Fatalf("New(fields): %v", err)
	}
	return f
}
