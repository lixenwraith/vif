package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
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
# --- author a sound over the seed ---
new sound blip                       # seeded minimal valid spec
validate blip                        # seed must be valid
set blip.layer.0.source.kind wrong
! validate blip                      # unknown kind must fail
set blip.layer.0.source.kind osc
set blip.duration 0.2
set blip.layer.0.source.wave sine
set blip.layer.0.source.freq 440
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
set p1.track.0.instr kick
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
new sound u1                           # seeded: layer.0 already exists
set u1.duration 0.1
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

func TestNewSeedsValid(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new sound ns1
validate ns1
play ns1
new pattern np1
validate np1
apply np1
slot 0 np1
`)
}

// TestNoPhantomModified pins bug 1: the TUI seeds builtins through
// replaceAll at startup, and replaceAll used to latch modified — every TUI
// session claimed unsaved changes before the first keystroke. Seeding and
// the entire play path must be state-neutral on both axes.
func TestNoPhantomModified(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	runOrFatal(t, s, &out, `
builtin sound
builtin pattern
`)
	if s.sounds.modified || s.pats.modified {
		t.Fatalf("seeding alone latched modified (sounds=%v patterns=%v)",
			s.sounds.modified, s.pats.modified)
	}

	runOrFatal(t, s, &out, `
music start
bpm 150
slot 0 beat_driving
wait 30
note 52
hit kick
music stop
`)
	if s.sounds.modified || s.pats.modified {
		t.Fatalf("play path latched modified (sounds=%v patterns=%v)\n%s",
			s.sounds.modified, s.pats.modified, out.String())
	}
	if d := s.sounds.dirtyNames(); len(d) != 0 {
		t.Fatalf("phantom dirty sounds: %v", d)
	}
	if d := s.pats.dirtyNames(); len(d) != 0 {
		t.Fatalf("phantom dirty patterns: %v", d)
	}
	if strings.Contains(out.String(), "auto-applied") {
		t.Fatalf("play path applied something:\n%s", out.String())
	}
}

// TestRevertContract pins bug 2. Contract: revert is registry-level. (1) An
// un-applied edit reverts to the last-applied content. (2) Revert after
// apply is the identity — apply moved the edits into the registry — and must
// say so rather than claim a restore. (3) modified stays latched either way:
// it tracks disk divergence and only save/replaceAll re-anchor (see doc[T]).
func TestRevertContract(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	runOrFatal(t, s, &out, `
new pattern rp
apply rp
set rp.track.0.event.0.vel 0.4
revert rp
show rp.track.0.event.0.vel
`)
	o := out.String()
	i := strings.Index(o, `reverted pattern "rp"`)
	if i < 0 {
		t.Fatalf("real revert not reported:\n%s", o)
	}
	if !strings.Contains(o[i:], "= 0.9") { // new seeds vel 0.9; apply snapshotted it
		t.Fatalf("revert did not restore the applied content:\n%s", o)
	}
	if d := s.pats.dirtyNames(); len(d) != 0 {
		t.Fatalf("revert must clear dirty: %v", d)
	}
	if !s.pats.modified {
		t.Fatal("modified must stay latched after revert (disk was never written)")
	}

	out.Reset()
	runOrFatal(t, s, &out, `
set rp.track.0.event.0.vel 0.4
apply rp
revert rp
show rp.track.0.event.0.vel
`)
	o = out.String()
	if strings.Contains(o, "reverted pattern") {
		t.Fatalf("revert-after-apply must not claim a restore:\n%s", o)
	}
	if !strings.Contains(o, "matches registry") {
		t.Fatalf("no-op revert not reported:\n%s", o)
	}
	if !strings.Contains(o, "= 0.4") {
		t.Fatalf("apply-then-revert must keep the applied content:\n%s", o)
	}

	// Sound branch shares the mechanism; pin the shape once.
	out.Reset()
	runOrFatal(t, s, &out, `
new sound rs
apply rs
set rs.duration 0.35
revert rs
show rs.duration
`)
	if o = out.String(); !strings.Contains(o, `reverted sound "rs"`) || !strings.Contains(o, "= 0.2") {
		t.Fatalf("sound revert failed:\n%s", o)
	}
}

// TestTUIPromptAndBeat drives the new browser keys with no terminal: prompt
// commits, cursor landing, clone→inspector focus, and B straight after n on
// the new-seeded pattern (item 6). Every mutation still flows through
// Execute, so this also re-asserts script parity for the key macros.
func TestTUIPromptAndBeat(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	a := &tuiApp{
		s: s, log: newLogBuffer(100),
		exp: tui.NewTreeExpansion(), tree: tui.NewTreeState(10),
		cmdField: tui.NewTextFieldState(""), editField: tui.NewTextFieldState(""),
		kind: kindPattern,
	}
	enter := terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter}
	space := terminal.Event{Type: terminal.EventKey, Key: terminal.KeySpace}
	right := terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'l'}
	esc := terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEscape}

	a.promptNew()
	if a.mode != modePrompt {
		t.Fatal("n must arm the prompt")
	}
	a.editField.SetValue("bp1")
	a.promptKey(enter)
	if !s.pats.has("bp1") {
		t.Fatalf("prompt commit did not create the pattern:\n%s", out.String())
	}
	if a.selName() != "bp1" {
		t.Fatalf("cursor must land on the new entry, at %q", a.selName())
	}

	a.enterBeat() // item 6: fresh `new` seed opens cleanly
	if a.mode != modeBeat || a.beat == nil {
		t.Fatal("B on a new-seeded pattern must enter beat mode")
	}
	a.beatKey(space) // seed has an event at pos 0: toggle deletes it
	if d, _ := s.pats.get("bp1"); eventAt(d, 0, 0) != -1 {
		t.Fatal("toggle on an occupied cell must delete the event")
	}
	a.beatKey(right)
	a.beatKey(space) // empty cell: add pos=1 at the default velocity
	d, _ := s.pats.get("bp1")
	ei := eventAt(d, 0, 1)
	if ei < 0 || d.Track[0].Event[ei].Vel != beatDefaultVel {
		t.Fatalf("toggle on an empty cell must add pos=1 vel=%g, got %+v",
			beatDefaultVel, d.Track[0].Event)
	}
	a.beatKey(esc)

	a.promptClone()
	a.editField.SetValue("bp2")
	a.promptKey(enter)
	if !s.pats.has("bp2") || a.selName() != "bp2" {
		t.Fatalf("clone commit failed, cursor at %q", a.selName())
	}
	if a.focus != focusInspector || !a.exp.IsExpanded("bp2.track") {
		t.Fatal("clone must focus the inspector with the track list pre-expanded")
	}
	if !s.pats.modified {
		t.Fatal("new/cp/grid edits must latch modified")
	}
}

// TestWriteSingleEntry pins the export contract: exactly one entry lands in
// the file, it round-trips through the loader, and neither dirty nor
// modified move — write is not a baseline event.
func TestWriteSingleEntry(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	pf := filepath.Join(dir, "one_pat.toml")
	sf := filepath.Join(dir, "one_snd.toml")
	runOrFatal(t, s, &out, fmt.Sprintf(`
new pattern wp1
new pattern wp2
new sound ws1
write wp1 %s
write ws1 %s
`, pf, sf))

	if !s.pats.modified || !s.sounds.modified {
		t.Fatal("write must not clear modified — save owns the baseline")
	}
	if !s.pats.isDirty("wp1") || !s.sounds.isDirty("ws1") {
		t.Fatal("write must not clear dirty — the registry axis is untouched")
	}

	data, err := os.ReadFile(pf)
	if err != nil {
		t.Fatal(err)
	}
	pdefs, err := audio.LoadPatternDefs(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pdefs) != 1 || pdefs[0].Name != "wp1" {
		t.Fatalf("want exactly wp1, got %d pattern(s)", len(pdefs))
	}

	if data, err = os.ReadFile(sf); err != nil {
		t.Fatal(err)
	}
	sdefs, err := audio.LoadSoundsTOML(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sdefs) != 1 || sdefs[0].Name != "ws1" {
		t.Fatalf("want exactly ws1, got %d sound(s)", len(sdefs))
	}
}

// TestMixVerb pins mix's contract: append-only into dst, src untouched,
// dst-only dirty, same-kind enforced, result validates.
func TestMixVerb(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	runOrFatal(t, s, &out, `
new pattern base
new pattern part
set part.track.0.instr snare
set part.track.0.event.0.pos 8
mix part base
validate base
new sound sa
new sound sb
mix sb sa
validate sa
! mix sa part                 # cross-kind must fail
! mix base base               # self-mix is an accident, not a feature
`)
	d, _ := s.pats.get("base")
	if len(d.Track) != 2 {
		t.Fatalf("want 2 tracks in dst, got %d", len(d.Track))
	}
	if p, _ := s.pats.get("part"); len(p.Track) != 1 {
		t.Fatal("mix must not mutate src")
	}
	if sd, _ := s.sounds.get("sa"); len(sd.Layer) != 2 {
		t.Fatalf("want 2 layers in dst, got %d", len(sd.Layer))
	}
}

// waitFor polls an engine-side condition. Transport and slot commands travel
// the mixer's queue and take effect on its tick — asserting engine state
// synchronously after an exec races that tick (reliably so under -race).
// Registry state is the exception: RegisterPattern is mutex-synchronous and
// may be asserted directly.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// TestAutoSlotAssign pins the space-on-pattern flow: unassigned → auto-apply
// + lowest empty slot + transport start; assigned → transport toggle.
func TestAutoSlotAssign(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	a := &tuiApp{
		s: s, log: newLogBuffer(50),
		exp: tui.NewTreeExpansion(), tree: tui.NewTreeState(10),
		cmdField: tui.NewTextFieldState(""), editField: tui.NewTextFieldState(""),
		kind: kindPattern,
	}
	runOrFatal(t, s, &out, "new pattern ap")
	a.cursorTo("ap")

	a.playPattern()
	id := audio.PatternIDByName("ap")
	if id == audio.PatternSilence {
		t.Fatal("space must auto-apply the dirty pattern (registration is synchronous)")
	}
	waitFor(t, func() bool { return s.eng.SlotPattern(0) == id },
		"assigned pattern must publish into slot 0")
	waitFor(t, func() bool { return s.eng.IsMusicPlaying() },
		"auto-assign must start the transport")

	a.playPattern() // now sounding: space is play/pause
	waitFor(t, func() bool { return !s.eng.IsMusicPlaying() },
		"second space must pause the transport (stop applies on the mixer tick)")
}

// TestDeleteKey pins browser 'd': first press arms and deletes nothing,
// second same-name press deletes, and an intervening non-'d' key (simulated
// via the handle()-level withdrawal contract) disarms.
func TestDeleteKey(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)

	a := &tuiApp{
		s: s, log: newLogBuffer(50),
		exp: tui.NewTreeExpansion(), tree: tui.NewTreeState(10),
		cmdField: tui.NewTextFieldState(""), editField: tui.NewTextFieldState(""),
		kind: kindPattern,
	}
	runOrFatal(t, s, &out, "new pattern dp")
	a.cursorTo("dp")

	a.deleteSel()
	if !s.pats.has("dp") {
		t.Fatal("first d must arm, not delete")
	}
	a.delArmed = "" // what handle() does on any intervening non-'d' key
	a.deleteSel()
	if !s.pats.has("dp") {
		t.Fatal("withdrawn arm must not carry into the next press")
	}
	a.deleteSel()
	if s.pats.has("dp") {
		t.Fatal("second same-name d must delete")
	}
}
