package resource

import (
	"bytes"
	"compress/flate"
	"fmt"
	"github.com/lixenwraith/vif/internal/parameter"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/fsm"
)

// writeScenario lays down a two-file scenario under dir and returns its root.
func writeScenario(t *testing.T, dir, regionBody string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "description = \"A portable test scenario.\"\n[regions.main]\nfile = \"main.toml\"\n"
	for name, body := range map[string]string{"scenario.toml": entry, "main.toml": regionBody} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const scenarioRegion = "[states.MainStart]\nparent = \"Root\"\n"

// TestDigestFollowsContentNotLocation is the rule the digest exists for: two roots
// holding the same bytes are the same scenario however they are named or reached,
// and one changed byte is a different one. It is what a join is refused on.
func TestDigestFollowsContentNotLocation(t *testing.T) {
	base := t.TempDir()
	here := writeScenario(t, filepath.Join(base, "one", "scenario", "main"), scenarioRegion)
	there := writeScenario(t, filepath.Join(base, "two", "scenario", "renamed"), scenarioRegion)
	edited := writeScenario(t, filepath.Join(base, "three", "scenario", "main"),
		scenarioRegion+"\n[states.MainOther]\nparent = \"Root\"\n")

	a, b, c := read(t, here), read(t, there), read(t, edited)
	if a.Digest() != b.Digest() {
		t.Errorf("same bytes under different names digest differently:\n  %s\n  %s", a.Digest(), b.Digest())
	}
	if a.Name != "main" || b.Name != "renamed" {
		t.Errorf("names = %q and %q; want the containing directory", a.Name, b.Name)
	}
	if a.Digest() == c.Digest() {
		t.Error("an edited scenario kept its digest")
	}
}

// TestCanonicalFormRoundTrips covers what a transfer will depend on: the bytes a
// digest is taken over rebuild the same scenario, and a corrupted body is refused
// rather than loaded as a shorter one.
func TestCanonicalFormRoundTrips(t *testing.T) {
	src := read(t, writeScenario(t, filepath.Join(t.TempDir(), "scenario", "td"), scenarioRegion))

	back, err := UnmarshalScenario(src.Name, src.Marshal())
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Digest() != src.Digest() || back.Entry() != src.Entry() || back.Files() != src.Files() ||
		back.Description != "A portable test scenario." {
		t.Fatalf("round trip lost identity: %+v", back)
	}
	body, err := fs.ReadFile(back.FS(), "main.toml")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(body), "MainStart") {
		t.Errorf("round-tripped region body = %q", body)
	}

	truncated := src.Marshal()
	if _, err := UnmarshalScenario(src.Name, truncated[:len(truncated)-4]); err == nil {
		t.Error("a truncated body was accepted")
	}
	if _, err := UnmarshalScenario(src.Name, append(src.Marshal(), 0)); err == nil {
		t.Error("a body with trailing bytes was accepted")
	}
}

// TestMacroFilesAreScenarioFiles: a library the regions expand against is part of
// what the digest covers and a transfer carries. Were it not, two peers holding
// different libraries would agree on a digest and simulate different graphs.
func TestMacroFilesAreScenarioFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scenario", "lib")
	writeScenario(t, dir, "[states]\nMainStart = { macro = \"Wait\", next = \"MainStart\" }\n")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("scenario.toml", "macro_files = [\"macros.toml\"]\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n")
	library := "[macros.Wait]\nparams = { next = \"string\" }\nbody = { transitions = [{ trigger = \"Tick\", target = \"${next}\" }] }\n"
	write("macros.toml", library)

	before := read(t, dir)
	if before.Files() != 3 {
		t.Fatalf("scenario holds %d files, want the entry, the region and the library", before.Files())
	}
	merged, err := fsm.ResolveScenario(before.FS(), before.Entry())
	if err != nil {
		t.Fatalf("resolve from the held copy: %v", err)
	}
	var doc fsm.ScenarioDoc
	if err := toml.Decode(merged, &doc); err != nil || doc.States["MainStart"].Transitions[0].Target != "MainStart" {
		t.Fatalf("expanded state = %+v, %v", doc.States["MainStart"], err)
	}

	write("macros.toml", strings.Replace(library, "Tick", "EventGameResetRequest", 1))
	if read(t, dir).Digest() == before.Digest() {
		t.Error("an edited macro file kept the scenario's digest")
	}
}

func read(t *testing.T, dir string) Scenario {
	t.Helper()
	s, err := LoadScenario(Options{Scenario: dir})
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	return s
}

// TestATransferCarriesBytesByLengthNotByScanning is why the whole container is
// deflated as one stream rather than one per file: the framing is length-prefixed,
// so a file holding the container's own magic, a NUL or an unbalanced quote is
// taken by its declared length and cannot run into its neighbour. It also bounds
// the inflation, so a few compressed bytes cannot be made to allocate a megabyte.
func TestATransferCarriesBytesByLengthNotByScanning(t *testing.T) {
	adversarial := scenarioRegion +
		"\n# VIFSCEN\x00\x00\x01 unbalanced \" and a lone ' in a comment\n" +
		"[states.MainOther]\nparent = \"Root\"\n"
	src := read(t, writeScenario(t, filepath.Join(t.TempDir(), "scenario", "main"), scenarioRegion))
	// TOML refuses a NUL, so it cannot load from disk; a peer's body can still carry one.
	src.files[slices.IndexFunc(src.files, func(f scenarioFile) bool { return f.name == "main.toml" })].data = []byte(adversarial)
	src.digest = digestOf(src.Marshal())

	body, err := src.MarshalCompressed()
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	back, err := UnmarshalScenarioCompressed(src.Name, body)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}
	if back.Digest() != src.Digest() {
		t.Fatalf("round trip changed the digest")
	}
	region, err := fs.ReadFile(back.FS(), "main.toml")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(region) != adversarial {
		t.Errorf("region came back as %q", region)
	}
	entry, err := fs.ReadFile(back.FS(), "scenario.toml")
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if strings.Contains(string(entry), "MainOther") {
		t.Error("one file's bytes reached the next")
	}

	var bomb bytes.Buffer
	w, _ := flate.NewWriter(&bomb, flate.BestCompression)
	_, _ = w.Write(make([]byte, parameter.ScenarioMaxBytes*4))
	_ = w.Close()
	if _, err := UnmarshalScenarioCompressed("bomb", bomb.Bytes()); err == nil {
		t.Fatalf("a %d-byte body inflating to %d was accepted", bomb.Len(), parameter.ScenarioMaxBytes*4)
	}
}

// TestAMapPlacesCursorsUnlessItSaysNot: [map] cursors = false takes the cursors off
// the map, true or no key leaves them on it, and anything but a boolean is refused.
func TestAMapPlacesCursorsUnlessItSaysNot(t *testing.T) {
	for _, c := range []struct {
		table   string
		offMap  bool
		refused bool
	}{
		{"", false, false},
		{"[map]\ncursors = true\n", false, false},
		{"[map]\ncursors = false\n", true, false},
		{"[map]\ncursors = 0\n", false, true},
	} {
		entry := "[regions.main]\nfile = \"main.toml\"\n" + c.table
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		s, err := ReadScenario("cursors", fsys, "scenario.toml")
		if c.refused {
			if err == nil {
				t.Errorf("%q was taken", c.table)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.table, err)
		}
		if s.CursorsOffMap != c.offMap {
			t.Errorf("%q: cursors off the map %t, want %t", c.table, s.CursorsOffMap, c.offMap)
		}
	}
}

// TestAScenarioDeclaresItsPlayerRange: min_players and max_players default to one
// and a full roster, min may be 0, and a range nobody could join or that is upside
// down or past the roster is refused.
func TestAScenarioDeclaresItsPlayerRange(t *testing.T) {
	for _, c := range []struct {
		preamble string
		min, max int
		refused  bool
	}{
		{"", 1, parameter.MaxPlayers, false},
		{"min_players = 0\nmax_players = 2\n", 0, 2, false},
		{"min_players = 3\n", 3, parameter.MaxPlayers, false},
		{"max_players = 0\n", 0, 0, true},
		{"min_players = 3\nmax_players = 2\n", 0, 0, true},
		{fmt.Sprintf("max_players = %d\n", parameter.MaxPlayers+1), 0, 0, true},
		{"min_players = \"1\"\n", 0, 0, true},
	} {
		entry := c.preamble + "[regions.main]\nfile = \"main.toml\"\n"
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		s, err := ReadScenario("players", fsys, "scenario.toml")
		switch {
		case c.refused && err == nil:
			t.Errorf("%q was taken", c.preamble)
		case !c.refused && err != nil:
			t.Errorf("%q: %v", c.preamble, err)
		case !c.refused && (s.MinPlayers != c.min || s.MaxPlayers != c.max):
			t.Errorf("%q: players %d..%d, want %d..%d", c.preamble, s.MinPlayers, s.MaxPlayers, c.min, c.max)
		}
	}
}
