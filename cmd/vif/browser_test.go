//go:build js && wasm

package main

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/paths"
)

// TestBrowserBringsTheKeptSettingsUpToDateOnce: the settings document a page kept
// from an earlier visit is read brought up to date, naming the changes, and the
// start keeps the upgrade, which names none after; nothing kept, or a document that
// does not read, is the embedded one until the first change.
func TestBrowserBringsTheKeptSettingsUpToDateOnce(t *testing.T) {
	was := settingsStore
	t.Cleanup(func() { settingsStore = was })
	var s paths.Settings
	store, notes := keptSettings([]byte("[play]\nmouse = false # mine\neffects = 1\n"), &s)
	if s.Play.Mouse || len(notes) == 0 || notes[0] != `dropped unknown key "play.effects"` {
		t.Fatalf("read %+v, notes %q", s.Play, notes)
	}
	settingsStore = store
	if said := upgradeOwn(false, true); !strings.HasPrefix(said, "Settings brought up to date: "+notes[0]) {
		t.Fatalf("the start said %q", said)
	}
	doc, err := store.Load()
	if _, again, _ := paths.UpgradeSettings(doc); err != nil || len(again) > 0 || !strings.Contains(string(doc), "mouse = false # mine") {
		t.Fatalf("kept %q, %v:\n%s", again, err, doc)
	}
	for _, kept := range []string{"", "[play\n"} {
		var s paths.Settings
		store, notes := keptSettings([]byte(kept), &s)
		if doc, err := store.Load(); err != nil || len(notes) > 0 || s != (paths.Settings{}) || string(doc) != string(asset.DefaultSettings) {
			t.Errorf("kept %q: %+v, notes %q, %v", kept, s, notes, err)
		}
	}
}
