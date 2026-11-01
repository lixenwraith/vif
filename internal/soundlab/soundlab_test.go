//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/pkg/audio"
)

func newTestSession(t *testing.T, out *bytes.Buffer) *Session {
	t.Helper()
	s, err := NewSession(Options{Backend: "null", SfxVolume: 0.5, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if s.startErr != nil {
		t.Fatalf("null backend must start: %v", s.startErr)
	}
	// the searches sized for a test: enough to run every path, not to be good
	s.fit = fitOptions{notes: 2, generations: 1, pool: 4, seed: 1}
	s.compressOpt = compressOptions{generations: 3, pool: 6, seed: 1}
	return s
}

func writeWAV(t *testing.T, file string, buf []float64) {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.WriteWAV(f, buf); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func runOrFatal(t *testing.T, s *Session, out *bytes.Buffer, script string) {
	t.Helper()
	if err := RunScript(s, strings.NewReader(script)); err != nil {
		t.Fatalf("script: %v\noutput:\n%s", err, out.String())
	}
}

func teardown(t *testing.T, s *Session) {
	t.Helper()
	s.Close()
	// ResetRegistries is only defined once the mix goroutine has returned;
	// Stopped is the happens-before edge that makes it safe. Stop's own wait is
	// bounded for a wedged backend, and a loaded tick can outlast it.
	waitFor(t, s.eng.Stopped, "mixer did not stop; registries cannot be reset")
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

// TestEditOpensWhatIsSelected: e opens a pattern in the editor's grid and a sound
// in the inspector, neither as new; space in the grid takes a hit off and puts one
// on, and a clone lands in the inspector with its tracks open.
func TestEditOpensWhatIsSelected(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "new pattern bp1\nnew sound bs1")
	a := newTUIApp(s, NewLogBuffer(100))
	key := func(k terminal.Key, r rune) { a.editorKey(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r}) }

	a.kind = kindSound
	a.cursorTo("bs1")
	a.editSel()
	if a.focus != focusInspector || a.fresh != "" {
		t.Fatalf("e on a sound: focus %d, fresh %q; want the inspector, editing", a.focus, a.fresh)
	}
	a.focus, a.kind = focusMain, kindPattern
	a.cursorTo("bp1")
	a.editSel()
	if a.view != viewEdit || a.focus != focusGrid || a.ed.name != "bp1" || a.fresh != "" {
		t.Fatalf("e on a pattern: view %d, focus %d; want its grid, editing", a.view, a.focus)
	}
	key(terminal.KeySpace, 0) // the seed's hit at step 0
	if d, _ := s.pats.get("bp1"); eventAt(d, 0, 0) != -1 {
		t.Fatal("space on a hit must take it off")
	}
	key(terminal.KeyRune, 'l')
	key(terminal.KeySpace, 0)
	if d, _ := s.pats.get("bp1"); eventAt(d, 0, 1) < 0 || d.Track[0].Event[eventAt(d, 0, 1)].Vel != gridDefaultVel {
		t.Fatalf("space on an empty step must put a hit there at %g", gridDefaultVel)
	}
	key(terminal.KeyEscape, 0)

	a.promptClone()
	a.editField.SetValue("bp2")
	a.promptKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter})
	if !s.pats.has("bp2") || a.selName() != "bp2" || a.focus != focusInspector || !a.exp.IsExpanded("bp2.track") {
		t.Fatalf("a clone must land in the inspector, its tracks open: at %q, focus %d", a.selName(), a.focus)
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
	if len(sdefs.Sound) != 1 || sdefs.Sound[0].Name != "ws1" {
		t.Fatalf("want exactly ws1, got %d sound(s)", len(sdefs.Sound))
	}
}

// TestVoicesAreAuthoredAsData is the rule that a tonal voice is an entry of the
// sound document: made, edited, validated, applied and heard like a sound, saved
// with the sounds and loaded back, a new name an instrument a pattern takes.
func TestVoicesAreAuthoredAsData(t *testing.T) {
	dir := t.TempDir()
	sndFile := filepath.Join(dir, "sounds.toml")
	var out bytes.Buffer
	s := newTestSession(t, &out)
	runOrFatal(t, s, &out, fmt.Sprintf(`
builtin voice
ls voice
new voice glass
set glass.wave modal
set glass.partials 16
set glass.decay_time 1.5
set glass.unison 9                   # set checks syntax alone; the bound holds at validate
! validate glass
unset glass.unison
validate glass
new voice kick                       # a drum's name is not a voice's: validate says so
! validate kick
del kick
set lead.wave square                 # retune a shipped voice in place
apply all
music start
note 60 0.8 2 glass
new pattern gp
set gp.track.0.instr glass
set gp.track.0.event.0.deg 2
validate gp
apply gp
music stop
save sound %s
stat
`, sndFile))
	if !strings.Contains(out.String(), "auto-applied") && !strings.Contains(out.String(), "applied voice \"glass\"") {
		t.Fatalf("glass was never applied:\n%s", out.String())
	}
	if id, ok := audio.InstrumentByName("glass"); !ok || id < audio.InstrumentCount {
		t.Fatalf("glass is not an instrument after apply: %d %v", id, ok)
	}
	if audio.VoiceDefByName("lead").Wave != "square" {
		t.Fatal("the retuned lead did not reach the registry")
	}
	teardown(t, s)

	var out2 bytes.Buffer
	s2 := newTestSession(t, &out2)
	runOrFatal(t, s2, &out2, fmt.Sprintf(`
load sound %s
show glass
ls voice glass
`, sndFile))
	teardown(t, s2)
	if !strings.Contains(out2.String(), "wave = \"modal\"") || !strings.Contains(out2.String(), "voice(s) loaded") {
		t.Fatalf("the saved voice did not come back:\n%s", out2.String())
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
// may be asserted directly. A loaded run drops ticks, so the deadline is wide.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
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

	a := newTUIApp(s, NewLogBuffer(50))
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

	a := newTUIApp(s, NewLogBuffer(50))
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

// TestTakeLandsOnTheKeysDegrees: take writes the notes played onto the degrees of
// the key that sound them, a chord's notes kept apart on their step, and a note
// outside the key moves to the nearest degree, the lower on a tie.
func TestTakeLandsOnTheKeysDegrees(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	// E2 phrygian is E F G A B C D; the notes are E3, G3 and B3 together, F#3
	runOrFatal(t, s, &out, "key 40 phrygian\nnew pattern tk\ndel tk.track.0\ntake tk piano 0:52 4:55 4:59 8:54")
	d, _ := s.pats.get("tk")
	if len(d.Track) != 1 || d.Track[0].Instr != "piano" {
		t.Fatalf("want one piano track:\n%s", out.String())
	}
	type at struct{ pos, deg, oct int }
	var got []at
	for _, e := range d.Track[0].Event {
		got = append(got, at{e.Pos, e.Deg, e.Oct})
	}
	if want := []at{{0, 0, 1}, {4, 2, 1}, {4, 4, 1}, {8, 1, 1}}; !slices.Equal(got, want) {
		t.Fatalf("take events %v, want %v (F#3 on F)", got, want)
	}
}

// TestTooSmallTakesNoKeys: on a screen too small for the views a key does nothing
// but quit, and the ticker asks for no frame.
func TestTooSmallTakesNoKeys(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	a := newTUIApp(s, NewLogBuffer(50))
	a.w, a.h, a.dirty = minW-1, minH, false
	lines := len(a.log.lines)
	for _, r := range "ne: m" {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r})
	}
	a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
	if a.view != viewBrowse || a.mode != modeNormal || len(a.log.lines) != lines || a.dirty {
		t.Fatalf("a hidden view acted: view %d mode %d dirty %v log %q", a.view, a.mode, a.dirty, a.log.lines)
	}
	a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyCtrlQ})
	if !a.quit {
		t.Fatal("^Q must still quit")
	}
}

// TestRollPlacesANoteOnItsPitch: the roll puts a note at the cursor's pitch, and
// . moves it a degree up, into the next octave past the scale's last degree.
func TestRollPlacesANoteOnItsPitch(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "new pattern rl\nset rl.track.0.instr lead\ndel rl.track.0.event.0")
	a := newTUIApp(s, NewLogBuffer(100))
	a.openEditor("rl", focusGrid)
	key := func(r rune) { a.editorKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}) }
	key('r') // an empty track opens on the second octave's root
	for range 6 {
		key('k')
	}
	key(' ')
	key('.')
	d, _ := s.pats.get("rl")
	if evs := d.Track[0].Event; len(evs) != 1 || evs[0].Pos != 0 || evs[0].Deg != 0 || evs[0].Oct != 3 {
		t.Fatalf("want one note at step 0, degree 0 of octave 3, got %+v", evs)
	}
}

// TestUnknownSettingsAreSkipped: a vif.toml another vif build wrote, with a table
// this one does not know, still gives soundlab its mixer period.
func TestUnknownSettingsAreSkipped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, t.TempDir())
	doc := "[later]\nwindow = true\n[audio]\nbuffer_ms = 20\n"
	if err := os.WriteFile(filepath.Join(dir, paths.SettingsFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := ResolveFiles(dir, "", "")
	if err != nil || f.skipped == nil || f.buffer != 20*time.Millisecond {
		t.Fatalf("resolve = %+v, %v; want buffer 20ms with the unknown table named", f, err)
	}
}

// TestAudioFilesFollowVif is the rule that soundlab reads the override vif reads, and
// that an untitled save lands where the next vif run reads first: an installed system
// root is shadowed from the user root, never written.
func TestAudioFilesFollowVif(t *testing.T) {
	base := t.TempDir()
	user, sys, dir := filepath.Join(base, "user"), filepath.Join(base, "sys"), filepath.Join(base, "dir")
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv(paths.SystemRootsEnv, sys)
	installed := filepath.Join(sys, paths.AudioDirName, paths.MusicConfigFile)
	for _, d := range []string{filepath.Dir(installed), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pat := "[[pattern]]\nname = \"installed\"\nsteps = 16\n[[pattern.track]]\ninstr = \"kick\"\nevent = [{ pos = 0, vel = 1.0 }]\n"
	if err := os.WriteFile(installed, []byte(pat), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, root := range []string{"", dir} {
		f, err := ResolveFiles(root, "", "")
		if err != nil {
			t.Fatal(err)
		}
		vif, err := resource.Audio(resource.Options{Dir: root})
		if err != nil || f.music != vif.MusicPath || f.sounds != vif.SoundPath {
			t.Fatalf("root %q: soundlab reads %+v, vif %+v (%v)", root, f, vif, err)
		}
	}

	f, _ := ResolveFiles("", "", "")
	var out bytes.Buffer
	s, err := NewSession(Options{Backend: "null", SfxVolume: 0.5, Out: &out, Files: f})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown(t, s)
	runOrFatal(t, s, &out, "save pattern\n")
	vif, _ := resource.Audio(resource.Options{})
	if want := filepath.Join(user, paths.AppDirName, paths.AudioDirName, paths.MusicConfigFile); vif.MusicPath != want {
		t.Errorf("after save vif reads %q, want the user root's %q", vif.MusicPath, want)
	}
	if data, _ := os.ReadFile(vif.MusicPath); !strings.Contains(string(data), "installed") {
		t.Errorf("the saved override lost the installed pattern:\n%s", data)
	}
}

// frameTerm takes the frames a test lays out and shows them nowhere
type frameTerm struct{ terminal.Terminal }

func (frameTerm) Flush([]terminal.Cell, int, int) {}

// TestTabWalksThePanesOnScreen: Tab moves the keys through the panes on screen in
// every view, the editor's tabs among them, and Esc brings them back from a side
// pane; a pane takes only its own keys, so a piano key on the mixer plays nothing.
func TestTabWalksThePanesOnScreen(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	a := newTUIApp(s, NewLogBuffer(50))
	a.term, a.w, a.h = frameTerm{}, 100, 40
	key := func(k terminal.Key, r rune) {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r})
		a.render()
	}
	a.render()
	var walk []focusArea
	for range 3 {
		key(terminal.KeyTab, 0)
		walk = append(walk, a.focus)
	}
	if want := []focusArea{focusMixer, focusLog, focusMain}; !slices.Equal(walk, want) {
		t.Fatalf("browse walked %v, want %v", walk, want)
	}
	key(terminal.KeyRune, 'n')
	key(terminal.KeyEnter, 0)
	walk = walk[:0]
	for _, k := range []terminal.Key{terminal.KeyTab, terminal.KeyTab, terminal.KeyTab} {
		key(k, 0)
		walk = append(walk, a.focus)
	}
	if want := []focusArea{focusGrid, focusMixer, focusKeys}; a.view != viewEdit || !slices.Equal(walk, want) {
		t.Fatalf("the editor walked %v, want %v", walk, want)
	}
	key(terminal.KeyBacktab, 0)
	key(terminal.KeyRune, 'z')
	if a.focus != focusMixer || len(a.keys.lit) != 0 {
		t.Fatalf("on the mixer: focus %d, %d key(s) lit; want none", a.focus, len(a.keys.lit))
	}
	key(terminal.KeyEscape, 0)
	key(terminal.KeyRune, 'z')
	if a.view != viewEdit || a.focus != focusKeys || len(a.keys.lit) != 1 {
		t.Fatalf("after Esc: view %d, focus %d, %d key(s) lit; want the keys playing", a.view, a.focus, len(a.keys.lit))
	}
}

// TestMixerStaysWhereItIsLeft: rows the mixer cannot show scroll under its cursor,
// and stay as they are once the keys move to another pane.
func TestMixerStaysWhereItIsLeft(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "new pattern one\nnew pattern big\n"+strings.Repeat("mix one big\n", 11)+"apply big\nslot 0 big")
	id := audio.PatternIDByName("big")
	waitFor(t, func() bool { return s.eng.SlotPattern(0) == id }, "big must reach slot 0")
	a := newTUIApp(s, NewLogBuffer(50))
	a.term, a.w, a.h = frameTerm{}, 100, 24
	key := func(k terminal.Key, r rune) {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r})
		a.render()
	}
	a.render()
	key(terminal.KeyTab, 0)
	key(terminal.KeyRune, 'G')
	top := a.mixTop
	if a.mixCur != (mixRow{3, -1}) || top == (mixRow{0, -1}) {
		t.Fatalf("G on the mixer: cursor %v, first row %v; want the last slot, scrolled", a.mixCur, top)
	}
	key(terminal.KeyTab, 0)
	if a.focus != focusLog || a.mixTop != top {
		t.Fatalf("leaving the mixer moved it from %v to %v", top, a.mixTop)
	}
}

// TestNewPatternPlaysAlongAndGoes: n makes an empty pattern, new to the editor and
// playing in a silent slot beside what plays; a take goes into it as a track and is
// applied, and ^D twice deletes it and takes it out of its slot.
func TestNewPatternPlaysAlongAndGoes(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "bpm 999\nnew pattern bg\nslot 0 bg\nmusic start")
	bg := audio.PatternIDByName("bg")
	waitFor(t, func() bool { return s.eng.SlotPattern(0) == bg }, "bg must play in slot 0")
	a := newTUIApp(s, NewLogBuffer(100))
	a.promptNew(-1)
	a.promptKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter})
	d, ok := s.pats.get("new_1")
	if !ok || len(d.Track) != 0 || a.view != viewEdit || a.focus != focusKeys || a.fresh != "new_1" {
		t.Fatalf("n: want new_1 empty and new in the editor's keys:\n%s", strings.Join(a.log.lines, "\n"))
	}
	id := audio.PatternIDByName("new_1")
	waitFor(t, func() bool { return s.eng.SlotPattern(1) == id && s.eng.SlotPattern(0) == bg }, "new_1 must play in slot 1, beside bg")

	a.keys.rec = true
	for _, n := range []takeNote{{pos: 0, midi: 52}, {pos: 4, midi: 55}, {pos: 8, midi: 59}} {
		n.vel, n.dur, n.instr = 0.8, 2, audio.InstrPiano
		a.keys.take = append(a.keys.take, n)
	}
	a.editorKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyCtrlR})
	if d, _ := s.pats.get("new_1"); len(d.Track) != 1 || len(d.Track[0].Event) != 3 || s.pats.isDirty("new_1") || len(a.keys.take) != 0 {
		t.Fatalf("^R: want the take in new_1 as one applied track, the keys clean:\n%s", strings.Join(a.log.lines, "\n"))
	}

	for range 2 {
		a.editorKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyCtrlD})
	}
	if s.pats.has("new_1") || a.view != viewBrowse {
		t.Fatal("^D twice must delete new_1 and close the editor")
	}
	waitFor(t, func() bool { return s.eng.SlotPattern(1) == audio.PatternSilence }, "slot 1 must fall silent")
}

// TestMixerMutesTheCursorsSlot: space and m on the mixer mute the slot under its
// cursor and let it sound again, through the command a script runs, which where
// reports.
func TestMixerMutesTheCursorsSlot(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	a := newTUIApp(s, NewLogBuffer(50))
	a.term, a.w, a.h = frameTerm{}, 100, 24
	key := func(k terminal.Key, r rune) {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r})
		a.render()
	}
	a.render()
	key(terminal.KeyTab, 0)
	key(terminal.KeyDown, 0)
	key(terminal.KeyRune, 'm')
	if !s.eng.IsSlotMuted(1) || s.eng.IsSlotMuted(0) {
		t.Fatal("m on slot 1's row must mute slot 1 alone")
	}
	key(terminal.KeySpace, ' ')
	if s.eng.IsSlotMuted(1) {
		t.Fatal("space must let slot 1 sound again")
	}
	runOrFatal(t, s, &out, "mute slot 3 on\nwhere")
	if !strings.Contains(out.String(), "slot3=- (muted)") {
		t.Fatalf("where must name the muted slot:\n%s", out.String())
	}
}

// TestRipMakesPatternsOfARecording is the verb's contract: a recording's beat and
// melody come into the document as dirty patterns under the given name, and the
// transport takes the tempo and key heard.
// thumpLoop is eight seconds at 120 bpm: a low thump on every beat and a tone on
// every bar, a recording a rip hears a tempo, a kick and a note in
func thumpLoop() []float64 {
	const rate, bpm = audio.AudioSampleRate, 120
	beat := rate * 60 / bpm
	buf := make([]float64, 16*beat)
	for i := range buf {
		p := i % beat
		if p < rate/10 {
			buf[i] += 0.8 * math.Sin(2*math.Pi*55*float64(i)/rate) * math.Exp(-float64(p)/(rate/40))
		}
		if i%(4*beat) < beat {
			buf[i] += 0.3 * math.Sin(2*math.Pi*440*float64(i)/rate)
		}
	}
	return buf
}

func TestRipMakesPatternsOfARecording(t *testing.T) {
	const bpm = 120
	wav := filepath.Join(t.TempDir(), "loop.wav")
	writeWAV(t, wav, thumpLoop())
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, fmt.Sprintf(`
! rip %s loop                        # not a recording
rip %s loop
! rip %s loop                        # the names are taken
ls pattern loop*
validate all
`, filepath.Join(t.TempDir(), "missing.wav"), wav, wav))
	if !s.pats.has("loop_beat_1") || !s.pats.isDirty("loop_beat_1") {
		t.Fatalf("no dirty loop_beat_1 in the document:\n%s", out.String())
	}
	if s.bpm < bpm-3 || s.bpm > bpm+3 {
		t.Errorf("tempo %d, want about %d", s.bpm, bpm)
	}
	if !strings.Contains(out.String(), "ripped") {
		t.Errorf("no report:\n%s", out.String())
	}
}

// TestTuneSearchesTheRipParameters is the verb's contract on a search too small
// to be good: it renders a corpus from the registered patterns, scores the
// current parameters, runs the generations asked, and leaves the session with
// parameters that load back, written when a candidate beat the start.
func TestTuneSearchesTheRipParameters(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	file := filepath.Join(t.TempDir(), "rip.toml")
	if err := s.tune(tuneOptions{loops: 2, bars: 2, generations: 1, pool: 4, seed: 1}, file); err != nil {
		t.Fatalf("tune: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "generation 0") || !strings.Contains(out.String(), "current parameters score") {
		t.Fatalf("no search reported:\n%s", out.String())
	}
	if audio.ValidateRipParams(s.rip) != nil {
		t.Errorf("the session listens with parameters out of range: %+v", s.rip)
	}
	if data, err := os.ReadFile(file); err == nil {
		if _, err := audio.LoadRipParams(data); err != nil {
			t.Errorf("the written parameters do not load: %v", err)
		}
	} else if !strings.Contains(out.String(), "they stay") {
		t.Errorf("neither written nor kept:\n%s", out.String())
	}
	runOrFatal(t, s, &out, `
! tune 0
! tune 1 2 3
`)
}

// TestRenderAndCorpusWriteWhatTheEngineHears: render writes the slots' music to a
// WAV the rip reads back at the transport's tempo, and corpus writes loops with
// their frames and labelled events in the layout the trainer reads.
func TestRenderAndCorpusWriteWhatTheEngineHears(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "slots.wav")
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, fmt.Sprintf(`
! render %s                           # nothing slotted
builtin pattern
bpm 128
slot 0 beat_basic
render %s 4
corpus %s 2 2
`, wav, wav, filepath.Join(dir, "corpus")))
	f, err := os.Open(wav)
	if err != nil {
		t.Fatal(err)
	}
	r, err := audio.RipWAV(f, s.rip)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if d := r.BPM - 128; d < -2 || d > 2 {
		t.Errorf("the rendered slots rip at %d bpm, want 128", r.BPM)
	}
	for _, name := range []string{"corpus.json", "loop_000.wav", "loop_000.mel", "loop_001.json"} {
		if _, err := os.Stat(filepath.Join(dir, "corpus", name)); err != nil {
			t.Errorf("corpus lacks %s", name)
		}
	}
	lab, err := os.ReadFile(filepath.Join(dir, "corpus", "loop_000.json"))
	if err != nil || !strings.Contains(string(lab), "\"events\"") || !strings.Contains(string(lab), "\"voice\"") {
		t.Errorf("loop_000.json: %v\n%.200s", err, lab)
	}
}

// TestRipFitsVoicesAndCompressesToASong is the second stage's rule: a rip of the
// engine's own render fits a voice to each register it heard, compress matches
// its bars to a dictionary of one-bar patterns and a song that plays them,
// scored by ripping the song back, the song renders, saves with the patterns and
// loads back, and play conducts it through the live sequencer to its end.
func TestRipFitsVoicesAndCompressesToASong(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	var slots [audio.MusicSlots]audio.PatternID
	for _, p := range audio.RegisteredPatterns() {
		switch p.Role {
		case audio.RoleRhythm, audio.RoleMelody, audio.RoleBass:
			if slots[p.Role-1] == audio.PatternSilence {
				slots[p.Role-1] = p.ID
			}
		}
	}
	buf, err := s.eng.RenderMusic(slots, 120, 4, audio.DefaultRootNote, audio.ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	wav, song, file := filepath.Join(dir, "loop.wav"), filepath.Join(dir, "song.wav"), filepath.Join(dir, "patterns.toml")
	writeWAV(t, wav, buf)
	runOrFatal(t, s, &out, fmt.Sprintf(`
rip %s loop
! compress nothing                   # nothing ripped under that name
compress loop 20
ls song
song loop
! song loop_x                        # no such song
song loop %s
save pattern %s
`, wav, song, file))
	if names, _ := s.voices.names("loop_*"); len(names) == 0 || !strings.Contains(out.String(), "voice:") {
		t.Fatalf("no voice fitted:\n%s", out.String())
	}
	d, ok := s.songs.get("loop")
	if !ok || len(d.Bars) != 4 || d.BPM != s.bpm || !s.songs.isDirty("loop") {
		t.Fatalf("song %+v after compress:\n%s", d, out.String())
	}
	if !strings.Contains(out.String(), "rips back at") {
		t.Errorf("no score of the song against the rip:\n%s", out.String())
	}
	// a song's bars are edited by path like anything in a document
	saved := d.Clone()
	runOrFatal(t, s, &out, "unset loop.bars.0.0\nshow loop\n")
	if d.Bars[0][0] != "" || !s.songs.isDirty("loop") {
		t.Errorf("unset left bar 1's beat as %q", d.Bars[0][0])
	}
	dict := 0
	for _, n := range s.pats.order {
		if dictionaryName("loop", n) {
			dict++
		}
	}
	if dict == 0 || dict > 12 {
		t.Errorf("%d dictionary pattern(s) for 4 bars of 3 parts:\n%s", dict, out.String())
	}
	if fi, err := os.Stat(song); err != nil || fi.Size() < 1000 {
		t.Errorf("the song did not render: %v", err)
	}
	runOrFatal(t, s, &out, "fit loop 1\n")
	if !strings.Contains(out.String(), "registered; the patterns naming it play it") {
		t.Errorf("fit replaced no voice:\n%s", out.String())
	}
	// a short song conducted to its end, the way a ticking front end drives it
	short := &audio.SongDef{Name: "short", BPM: 200, Root: audio.DefaultRootNote, Scale: "phrygian",
		Bars: [][]string{{patName(slots[0]), "", "", ""}, {"", patName(slots[1]), "", ""}}}
	s.songs.put(short, true)
	s.ticked = true
	runOrFatal(t, s, &out, "song short play\n")
	if s.song == nil {
		t.Fatalf("play returned with no song conducted:\n%s", out.String())
	}
	for deadline := time.Now().Add(15 * time.Second); s.song != nil; {
		if time.Now().After(deadline) {
			t.Fatalf("the song did not end:\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
		s.conduct()
	}
	// the second bar cleared the beat and placed the melody
	if !strings.Contains(out.String(), "short done") || s.slots[1] != slots[1] || s.slots[0] != audio.PatternSilence {
		t.Errorf("after the song: slots %v\n%s", s.slots, out.String())
	}
	teardown(t, s)

	// the document carries the song to the next session
	var out2 bytes.Buffer
	s2 := newTestSession(t, &out2)
	defer teardown(t, s2)
	runOrFatal(t, s2, &out2, "load pattern "+file+"\nsong loop\nls song\n")
	if d2, ok := s2.songs.get("loop"); !ok || !reflect.DeepEqual(d2.Bars, saved.Bars) || !strings.Contains(out2.String(), "song(s)") {
		t.Fatalf("the song did not load back:\n%s", out2.String())
	}
}

// TestSplitArgsKeepsAQuotedPath: a path with a space travels in quotes
func TestSplitArgsKeepsAQuotedPath(t *testing.T) {
	got := splitArgs(`load pattern "my songs/a b.toml"  x`)
	if !slices.Equal(got, []string{"load", "pattern", "my songs/a b.toml", "x"}) {
		t.Errorf("split %q", got)
	}
	if got := splitArgs("set a.desc it's \"fine\""); !slices.Equal(got, []string{"set", "a.desc", "it's", "fine"}) {
		t.Errorf("split %q", got)
	}
}

// TestFilesOverlayLoadsRipsAndSavesHere: ^O lists a directory, its directories
// first and its files newest first; enter on a pattern document loads it by its
// tables and closes the overlay on the patterns; enter on a recording asks what
// to rip it as and rips it; s asks a name and saves the tab's document into the
// directory, which then shows it first.
func TestFilesOverlayLoadsRipsAndSavesHere(t *testing.T) {
	dir := t.TempDir()
	pat := "[[pattern]]\nname = \"from_file\"\nsteps = 16\n[[pattern.track]]\ninstr = \"kick\"\nevent = [{ pos = 0, vel = 1.0 }]\n"
	if err := os.WriteFile(filepath.Join(dir, "pats.toml"), []byte(pat), 0o644); err != nil {
		t.Fatal(err)
	}
	writeWAV(t, filepath.Join(dir, "loop.wav"), thumpLoop())
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	a := newTUIApp(s, NewLogBuffer(100))
	a.term, a.w, a.h = frameTerm{}, 100, 40
	key := func(k terminal.Key, r rune) {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r})
		a.render()
	}
	names := func() []string {
		var ns []string
		for _, r := range a.files.rows {
			ns = append(ns, r.name)
		}
		return ns
	}
	a.files.dir = dir // opens where it last was, so here rather than the working directory
	key(terminal.KeyCtrlO, 0)
	if !a.files.open || a.files.dir != dir {
		t.Fatalf("^O: open %v in %q", a.files.open, a.files.dir)
	}
	if ns := names(); len(ns) != 4 || ns[0] != ".." || ns[1] != "sub" {
		t.Fatalf("rows %v, want the parent, the directory, then the files", ns)
	}
	a.files.cur = slices.Index(names(), "pats.toml")
	key(terminal.KeyEnter, 0)
	if a.files.open || !s.pats.has("from_file") || a.kind != kindPattern {
		t.Fatalf("enter on a pattern document: open %v, loaded %v, tab %d", a.files.open, s.pats.has("from_file"), a.kind)
	}
	key(terminal.KeyCtrlO, 0)
	a.files.cur = slices.Index(names(), "loop.wav")
	key(terminal.KeyEnter, 0)
	if a.mode != modePrompt || a.editField.Value() != "loop" {
		t.Fatalf("enter on a recording: mode %d, name %q", a.mode, a.editField.Value())
	}
	key(terminal.KeyEnter, 0)
	if a.files.open || !s.pats.has("loop_beat_1") {
		t.Fatalf("the recording was not ripped: open %v\n%s", a.files.open, out.String())
	}
	key(terminal.KeyCtrlO, 0)
	key(terminal.KeyRune, 's')
	if a.mode != modePrompt {
		t.Fatal("s asked no name")
	}
	a.editField.SetValue("saved.toml")
	key(terminal.KeyEnter, 0)
	if _, err := os.Stat(filepath.Join(dir, "saved.toml")); err != nil {
		t.Fatalf("save here: %v\n%s", err, out.String())
	}
	if ns := names(); !a.files.open || ns[2] != "saved.toml" {
		t.Errorf("after the save the overlay shows %v, want the saved file first", ns)
	}
	key(terminal.KeyEscape, 0)
	if a.files.open {
		t.Error("esc left the overlay open")
	}
	if kind, err := sniffDocument(filepath.Join(dir, "saved.toml")); kind != "pattern" || err != nil {
		t.Errorf("the saved document sniffs as %q, %v", kind, err)
	}
}
