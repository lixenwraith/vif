package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/audio"
)

func newTestSession(t *testing.T, out *bytes.Buffer) *Session {
	t.Helper()
	s, err := NewSession("null", 0.5, out)
	if err != nil {
		t.Fatal(err)
	}
	if s.startErr != nil {
		t.Fatalf("null backend must start: %v", s.startErr)
	}
	return s
}

func runOrFatal(t *testing.T, s *Session, out *bytes.Buffer, script string) {
	t.Helper()
	if err := runScript(s, strings.NewReader(script)); err != nil {
		t.Fatalf("script: %v\noutput:\n%s", err, out.String())
	}
}

func teardown(t *testing.T, s *Session) {
	t.Helper()
	s.Close()
	// ResetRegistries is only defined once the mix goroutine has returned;
	// Stopped is the happens-before edge that makes it safe.
	if !s.eng.Stopped() {
		t.Fatal("mixer did not stop; registries cannot be reset")
	}
	audio.ResetRegistries()
}

// TestEndToEnd is the deliverable-4 script: author from nothing, validate,
// apply, audition headless, export, save; then a fresh engine reloads and the
// re-render must be byte-identical — RenderPreview seeds its rng from the
// sound name, so audio-identical means exact bytes, not a tolerance.
func TestEndToEnd(t *testing.T) {
	dir := t.TempDir()
	wavA := filepath.Join(dir, "a.wav")
	wavB := filepath.Join(dir, "b.wav")
	sndFile := filepath.Join(dir, "sounds.toml")
	patFile := filepath.Join(dir, "patterns.toml")

	var out1 bytes.Buffer
	s1 := newTestSession(t, &out1)

	script1 := fmt.Sprintf(`
# --- author a sound from nothing ---
new sound blip
! validate blip                      # empty spec must fail
set blip.duration 0.2
add blip.layer
set blip.layer.0.source.kind osc
set blip.layer.0.source.wave sine
set blip.layer.0.source.freq 440
add blip.layer.0.chain
set blip.layer.0.chain.0.kind ar
set blip.layer.0.chain.0.attack 0.005
set blip.layer.0.chain.0.release 0.05
! set blip.layer.0.source.nope 1     # unknown key must fail with sibling hint
validate blip
apply blip
play blip 0.8
hit kick                             # registry fallback path
export blip %s
# --- author a pattern ---
new pattern p1
set p1.steps 16
add p1.track
set p1.track.0.instr kick
add p1.track.0.event
set p1.track.0.event.0.pos 0
set p1.track.0.event.0.vel 0.9
validate p1
apply p1
set p1.track.0.event.0.vel 0.8       # dirty again
slot 0 p1                            # must auto-apply
music start
bpm 150
wait 20
where
music stop
save sound %s
save pattern %s
stat
`, wavA, sndFile, patFile)
	runOrFatal(t, s1, &out1, script1)
	if !strings.Contains(out1.String(), "auto-applied dirty pattern") {
		t.Fatalf("slot did not auto-apply:\n%s", out1.String())
	}
	teardown(t, s1)

	var out2 bytes.Buffer
	s2 := newTestSession(t, &out2)
	script2 := fmt.Sprintf(`
load sound %s
load pattern %s
validate all
show p1.track.0.event.0.vel
export blip %s
`, sndFile, patFile, wavB)
	runOrFatal(t, s2, &out2, script2)
	teardown(t, s2)

	if !strings.Contains(out2.String(), "0.8") {
		t.Fatalf("edited velocity lost on round-trip:\n%s", out2.String())
	}
	a, err := os.ReadFile(wavA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wavB)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) <= 44 {
		t.Fatalf("export produced a header-only WAV (%d bytes)", len(a))
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("reloaded render diverges: %d vs %d bytes", len(a), len(b))
	}
}

// TestPathDiagnostics pins the failure mode the reflection walk was chosen
// for: an unknown key names its siblings.
func TestPathDiagnostics(t *testing.T) {
	d := &audio.SoundDef{Name: "x"}
	_, err := resolve(reflect.ValueOf(d), []string{"nope"}, false)
	if err == nil {
		t.Fatal("want error for unknown key")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"nope"`) || !strings.Contains(msg, "duration") {
		t.Fatalf("want sibling hint listing known keys, got: %v", err)
	}
}

func TestUnset(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new sound u1
set u1.duration 0.1
add u1.layer
set u1.layer.0.source.kind osc
set u1.layer.0.source.freq 100
set u1.layer.0.source.vibrato.rate 5   # create-on-write through a nil pointer
show u1.layer.0.source.vibrato.rate
unset u1.layer.0.source.vibrato
show u1.layer.0.source.vibrato
validate u1
`)
	o := out.String()
	if !strings.Contains(o, "= 5") || !strings.Contains(o, "<unset>") {
		t.Fatalf("unset round-trip failed:\n%s", o)
	}
}

func TestModifiedFlag(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new sound mf
set mf.duration 0.1
add mf.layer
set mf.layer.0.source.kind osc
set mf.layer.0.source.freq 200
`)
	if !s.sounds.modified {
		t.Fatal("edits must set modified")
	}
	runOrFatal(t, s, &out, "save sound "+filepath.Join(dir, "mf.toml"))
	if s.sounds.modified {
		t.Fatal("save must clear modified")
	}
}
