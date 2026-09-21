package resource

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScenario lays down a two-file scenario under dir and returns its root.
func writeScenario(t *testing.T, dir, regionBody string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "[regions.main]\nfile = \"main.toml\"\n"
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
	if back.Digest() != src.Digest() || back.Entry() != src.Entry() || back.Files() != src.Files() {
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

func read(t *testing.T, dir string) Scenario {
	t.Helper()
	s, err := LoadScenario(Options{Scenario: dir})
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	return s
}
