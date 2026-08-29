package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
)

// loadSystemsConfig writes a minimal machine config carrying the given
// [systems] and region tables, then loads it
func loadSystemsConfig(t *testing.T, body string) *fsm.Machine[*engine.World] {
	t.Helper()
	event.EnsureRegistry()

	path := filepath.Join(t.TempDir(), "game.toml")
	config := body + `
[states.Root]
transitions = []
`
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := fsm.NewMachine[*engine.World]()
	manifest.RegisterFSMComponents(m)
	if err := fsm.LoadConfigFromPath(m, path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return m
}

// TestCheckSystemsAcceptsIntactSet asserts a config disabling nothing required passes
func TestCheckSystemsAcceptsIntactSet(t *testing.T) {
	m := loadSystemsConfig(t, `
[systems]
disabled_systems = ["glyph","nugget","gold","audio","music"]

[regions.main]
initial = "Root"
`)
	if err := checkSystems(m, io.Discard); err != nil {
		t.Fatalf("checkSystems rejected an intact set: %v", err)
	}
}

// TestCheckSystemsRejectsDisabledRequirement asserts a global disable that
// strands a dependent is named, with both endpoints in the message
func TestCheckSystemsRejectsDisabledRequirement(t *testing.T) {
	m := loadSystemsConfig(t, `
[systems]
disabled_systems = ["death"]

[regions.main]
initial = "Root"
`)
	err := checkSystems(m, io.Discard)
	if err == nil {
		t.Fatal("checkSystems accepted a config disabling a required system")
	}
	for _, want := range []string{"[systems]", "combat requires death", "timer requires death"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message does not report %q:\n%v", want, err)
		}
	}
}

// TestCheckSystemsRejectsRegionDisabledRequirement asserts a per-region disable
// is caught and attributed to its region
func TestCheckSystemsRejectsRegionDisabledRequirement(t *testing.T) {
	m := loadSystemsConfig(t, `
[regions.main]
initial = "Root"
disabled_systems = ["composite"]
`)
	err := checkSystems(m, io.Discard)
	if err == nil {
		t.Fatal("checkSystems accepted a region disabling a required system")
	}
	if !strings.Contains(err.Error(), "region main: wall requires composite") {
		t.Errorf("message does not attribute the failure to its region:\n%v", err)
	}
}

// TestCheckSystemsAcceptsDependentDisabledToo asserts disabling both ends is
// legal: the dependency is only required by systems that remain enabled
func TestCheckSystemsAcceptsDependentDisabledToo(t *testing.T) {
	m := loadSystemsConfig(t, `
[regions.main]
initial = "Root"
disabled_systems = ["cursor","camera","energy","heat","ping","shield","boost","weapon","typing","splash","motion_marker","missile","network"]
`)
	if err := checkSystems(m, io.Discard); err != nil {
		t.Fatalf("checkSystems rejected a self-consistent disable set: %v", err)
	}
}

// TestCheckSystemsRejectsUnknownName asserts name validation still runs first,
// and that the message names the entry it rejected
func TestCheckSystemsRejectsUnknownName(t *testing.T) {
	m := loadSystemsConfig(t, `
[regions.main]
initial = "Root"
disabled_systems = ["timekeeper"]
`)
	err := checkSystems(m, io.Discard)
	if err == nil {
		t.Fatal("checkSystems accepted an unknown system name")
	}
	for _, want := range []string{"unknown system names", "region main: timekeeper"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message does not report %q:\n%v", want, err)
		}
	}
}
