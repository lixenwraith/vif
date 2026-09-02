package journal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

type scriptEmission struct {
	typeID  event.EventType
	payload any
	domain  core.Domain
}

type scriptFake struct {
	position event.Stamp
	intents  []input.Intent
	events   []scriptEmission
}

func (f *scriptFake) Position() event.Stamp { return f.position }
func (f *scriptFake) Tick(n int)            { f.position.Tick += uint64(n) }
func (f *scriptFake) Inject(intents ...*input.Intent) bool {
	for _, intent := range intents {
		f.intents = append(f.intents, *intent)
	}
	return true
}
func (f *scriptFake) Emit(et event.EventType, payload any, domain core.Domain) {
	f.events = append(f.events, scriptEmission{typeID: et, payload: payload, domain: domain})
}

func TestAuthoredScriptRunsActionsAtNamedTicks(t *testing.T) {
	script, err := ParseScript([]byte(`
schema = 1
ticks = 3
width = 120
height = 40

[[action]]
tick = 3
event = "HeatSetRequest"
payload = "entity = 1\nvalue = 99"

[[action]]
tick = 0
text = "ab"

[[action]]
tick = 1
intent = "motion_right"
count = 2

[[action]]
tick = 2
command = ":god"
`))
	if err != nil {
		t.Fatalf("ParseScript() error = %v", err)
	}
	if script.Width != 120 || script.Height != 40 {
		t.Fatalf("geometry = %dx%d, want 120x40", script.Width, script.Height)
	}

	target := &scriptFake{}
	driver, err := NewScriptDriver(target, script)
	if err != nil {
		t.Fatalf("NewScriptDriver() error = %v", err)
	}
	if err := driver.RunAll(); err != nil {
		t.Fatalf("RunAll() error = %v", err)
	}
	stats := driver.Stats()
	if stats.Executed != 4 || stats.Ticks != 3 || stats.End.Tick != 3 {
		t.Fatalf("stats = %+v, want 4 actions and tick 3", stats)
	}

	if len(target.intents) != 8 {
		t.Fatalf("intent count = %d, want 8 (text 2, motion 1, command 5)", len(target.intents))
	}
	if got := target.intents[2]; got.Type != input.IntentMotion || got.Motion != input.MotionRight || got.Count != 2 {
		t.Fatalf("motion intent = %+v", got)
	}
	if len(target.events) != 1 || target.events[0].typeID != event.EventHeatSetRequest ||
		target.events[0].domain != core.DomainPlayer {
		t.Fatalf("event = %+v, want player HeatSetRequest", target.events)
	}
	p, ok := target.events[0].payload.(*event.HeatSetRequestPayload)
	if !ok || p.Entity != 1 || p.Value != 99 {
		t.Fatalf("event payload = %#v", target.events[0].payload)
	}
}

func TestAuthoredScriptRequiresStampedEventDomain(t *testing.T) {
	_, err := ParseScript([]byte(`
schema = 1
ticks = 1
[[action]]
tick = 0
event = "MaterializeRequest"
`))
	if err == nil || !strings.Contains(err.Error(), "requires domain") {
		t.Fatalf("ParseScript() error = %v, want stamped-domain error", err)
	}
}

func TestAuthoredScriptRejectsUnknownFields(t *testing.T) {
	_, err := ParseScript([]byte(`
schema = 1
ticks = 1
[[action]]
tick = 0
intnet = "motion_right"
`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "intnet"`) {
		t.Fatalf("ParseScript() error = %v, want unknown-field error", err)
	}
}

// TestCheckedInScriptsCompile parses every script the repository ships.
//
// It used to name the Phase 3 pair, which meant each later phase's pair went in
// unchecked: a script is only read by an operator running a two-terminal
// diagnostic, so a typo in one is discovered at the moment it is least welcome.
// Globbing the directory is what makes a new pair covered by existing it.
func TestCheckedInScriptsCompile(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "script", "*.toml"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no checked-in scripts found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			script, err := LoadScript(path)
			if err != nil {
				t.Fatalf("LoadScript() error = %v", err)
			}
			if script.Ticks <= 0 {
				t.Fatalf("ticks = %d; a script declares a hard budget", script.Ticks)
			}
			if _, err := NewScriptDriver(&scriptFake{}, script); err != nil {
				t.Fatalf("NewScriptDriver() error = %v", err)
			}
		})
	}
}

func TestAuthoredScriptReportsAnUnreachedRun(t *testing.T) {
	script, err := ParseScript([]byte(`
schema = 1
ticks = 1
[[action]]
run = 1
tick = 0
intent = "motion_right"
`))
	if err != nil {
		t.Fatalf("ParseScript() error = %v", err)
	}
	driver, err := NewScriptDriver(&scriptFake{}, script)
	if err != nil {
		t.Fatalf("NewScriptDriver() error = %v", err)
	}
	err = driver.RunAll()
	if err == nil || !strings.Contains(err.Error(), "before action") {
		t.Fatalf("RunAll() error = %v, want unreached-action error", err)
	}
}
