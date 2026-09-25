package prof

import (
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
)

// TestProfilerCardsShowOnlyWhileOn covers the off-cost promise from the viewer's
// side: off, a timed call records nothing and the prof and proc cards stay
// hidden; on, a closed window ranks the module first; off again hides them.
func TestProfilerCardsShowOnlyWhileOn(t *testing.T) {
	reg := status.NewRegistry()
	p := New(reg)
	drain := p.Timer(KindSystem, "drain")
	reg.Freeze()

	visible := func() []string {
		var names []string
		for _, v := range reg.VisibleViews() {
			if strings.HasPrefix(v.Name(), "prof") || strings.HasPrefix(v.Name(), "proc") {
				names = append(names, v.Name())
			}
		}
		return names
	}

	if span := p.Begin(drain); span.start != 0 || drain.calls.Load() != 0 {
		t.Fatal("an off profiler timed a call")
	}
	if got := visible(); len(got) != 0 {
		t.Fatalf("off profiler shows cards %v", got)
	}

	p.SetOn(true)
	p.Begin(drain).End()
	p.start.Store(now() - int64(parameter.ProfWindow))
	p.Publish()
	if got := visible(); !slices.Contains(got, "prof.top") || !slices.Contains(got, "proc") {
		t.Fatalf("on profiler shows cards %v", got)
	}
	if top := p.top[0].Load(); !strings.HasPrefix(top, "sys drain ") {
		t.Fatalf("top module = %q", top)
	}

	p.SetOn(false)
	if got := visible(); len(got) != 0 || p.Report() != nil {
		t.Fatalf("stopped profiler still shows cards %v", got)
	}
}
