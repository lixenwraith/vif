//go:build js && wasm

package main

import (
	"slices"
	"strings"
	"syscall/js"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/paths"
)

// TestBrowserSpeakerPostsSoundAlone: the speaker posts a buffer that sounds, as the
// page's audio, and nothing else; a page that plays no sound has no speaker
func TestBrowserSpeakerPostsSoundAlone(t *testing.T) {
	if speaker() != nil {
		t.Fatal("a speaker without a page that plays sound")
	}
	var posted []int
	post := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == kindAudio {
			posted = append(posted, args[1].Get("data").Get("length").Int())
		}
		return nil
	})
	defer post.Release()
	js.Global().Set("terminalPage", map[string]any{"features": []any{kindAudio}, "post": post})
	defer js.Global().Delete("terminalPage")
	s := speaker()
	if s == nil {
		t.Fatal("no speaker on a page that plays sound")
	}
	for _, p := range [][]byte{make([]byte, 8), {0, 0, 1, 0}} {
		if n, err := s.Write(p); n != len(p) || err != nil {
			t.Fatalf("Write(%v) = %d, %v", p, n, err)
		}
	}
	if len(posted) != 1 || posted[0] != 4 {
		t.Fatalf("posted %v, want the 4-byte buffer alone", posted)
	}
}

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
	if said := upgradeOwn(true); !strings.HasPrefix(said, "Settings brought up to date: "+notes[0]) {
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

// TestBrowserKeepsWhatABotLearnedAsThePagesDocuments: a population is a document
// the page keeps under its name. The page names those it held as the run began;
// one the run stores or removes since is read from the run, and told to the page.
func TestBrowserKeepsWhatABotLearnedAsThePagesDocuments(t *testing.T) {
	posted := map[string]string{}
	post := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "keep" {
			posted[args[1].Get("name").String()] = args[1].Get("text").String()
		}
		return nil
	})
	defer post.Release()
	js.Global().Set("terminalPage", map[string]any{"post": post,
		"kept": map[string]any{docBots: "default.fighter.walk\nmine.cave.raid", docBot + "default.fighter.walk": "walked", docBot + "mine.cave.raid": "raided"}})
	defer js.Global().Delete("terminalPage")
	p := &keptPopulations{kept: map[string][]byte{}}
	read := func(name string) string {
		doc, err := p.Open(name).Load()
		if err != nil {
			t.Fatal(err)
		}
		return string(doc)
	}
	if got := p.Names(); !slices.Equal(got, []string{"default.fighter.walk", "mine.cave.raid"}) || read("mine.cave.raid") != "raided" {
		t.Fatalf("the page's populations read as %v, one %q", got, read("mine.cave.raid"))
	}
	if err := p.Open("mine.cave.kraken").Store([]byte("fought")); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove("mine.cave.raid"); err != nil {
		t.Fatal(err)
	}
	if got := p.Names(); !slices.Equal(got, []string{"default.fighter.walk", "mine.cave.kraken"}) || read("mine.cave.kraken") != "fought" || read("mine.cave.raid") != "" {
		t.Fatalf("after a store and a remove the run reads %v, %q and %q", got, read("mine.cave.kraken"), read("mine.cave.raid"))
	}
	if text, told := posted[docBot+"mine.cave.raid"]; posted[docBot+"mine.cave.kraken"] != "fought" || !told || text != "" {
		t.Fatalf("the page was told to keep %v: want the new population, and the removed one empty", posted)
	}
}
