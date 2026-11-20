package game

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/mode"
)

// TestARunsFirstAskHandsTheDrawerItsTexture: the app builds its drawer from Atlas
// before a run opens, so the run's first AtlasChanged hands over the texture its
// frames are encoded for, a smaller strike's too, and then nothing until a zoom
// crosses to another.
func TestARunsFirstAskHandsTheDrawerItsTexture(t *testing.T) {
	v := newView(false, 1, 0) // 12 px cells, which the smallest strike draws
	got := v.AtlasChanged()
	if !bytes.Equal(got, v.Atlas()) || bytes.Equal(got, Atlas()) || v.AtlasChanged() != nil {
		t.Fatalf("the first ask handed %d bytes, the run's texture being %d and the app's %d", len(got), len(v.Atlas()), len(Atlas()))
	}
}

// menuListener keeps what the run handed the app to draw
type menuListener struct{ menus []string }

func (l *menuListener) Exited(string)    {}
func (l *menuListener) Menu(json string) { l.menus = append(l.menus, json) }
func (l *menuListener) Subnets() string  { return "" }
func (l *menuListener) Listening(bool)   {}

// TestTheMenuCrossesTheBindingAsJSONAndOps: a snapshot reaches the app's listener as
// its JSON, a closed menu as the empty string; an op the app sends reaches the run's
// channel with its fields, and with the queue full it is dropped rather than waited on.
func TestTheMenuCrossesTheBindingAsJSONAndOps(t *testing.T) {
	v := newView(false, 1, 0)
	l := &menuListener{}
	v.listener = l
	v.Menu([]byte(`{"title":"Menu"}`))
	v.Menu(nil)
	if want := []string{`{"title":"Menu"}`, ""}; !slices.Equal(l.menus, want) {
		t.Fatalf("the app was handed %q, want %q", l.menus, want)
	}
	mu.Lock()
	run = v
	mu.Unlock()
	defer func() {
		mu.Lock()
		run = nil
		mu.Unlock()
	}()
	Menu("set", "settings/#Controls/auto", "main")
	select {
	case op := <-v.MenuOps():
		if op != (mode.MenuOp{Op: "set", ID: "settings/#Controls/auto", Arg: "main"}) {
			t.Fatalf("the op crossed as %+v", op)
		}
	default:
		t.Fatal("the op did not reach the run")
	}
	for range eventQueue + 1 {
		Menu("close", "", "")
	}
	if len(v.menuOps) != eventQueue {
		t.Fatalf("%d ops queued past the queue's %d", len(v.menuOps), eventQueue)
	}
}

// TestTheHostsAreWrittenToTheRunsMenuContract: MenuDialog.java and the page's
// vif.js name the node kinds, the ops and the field inputs the run's snapshot
// and hooks use, each list in the run's order, so no host drifts from the run.
func TestTheHostsAreWrittenToTheRunsMenuContract(t *testing.T) {
	for _, host := range []struct {
		path, declaration string
	}{
		{"../app/src/main/java/com/lixen/vif/MenuDialog.java", `static final String\[\] %s = \{([^}]*)\};`},
		{"../../web/vif.js", `const MENU_%s = \[([^\]]*)\];`},
	} {
		src, err := os.ReadFile(host.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			name string
			want []string
		}{{"KINDS", mode.MenuKinds[:]}, {"OPS", mode.MenuOps}, {"INPUTS", mode.MenuInputs}} {
			m := regexp.MustCompile(fmt.Sprintf(host.declaration, c.name)).FindSubmatch(src)
			if m == nil {
				t.Fatalf("%s declares no %s", host.path, c.name)
			}
			var got []string
			for _, q := range regexp.MustCompile(`["']([^"']*)["']`).FindAllSubmatch(m[1], -1) {
				got = append(got, string(q[1]))
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("%s is %q in %s, %q in the run", c.name, got, host.path, c.want)
			}
		}
	}
}
