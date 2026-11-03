//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
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
	s.refineOpt = refineOptions{generations: 1, pool: 4, seed: 1, steps: 2}
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

// TestListElementsInsertCopyAndMove: over any list a path reaches, add inserts
// a zero element at an index, dup a copy sharing nothing with its original, and
// mv moves an element, the rest shifting; a song's bars are listed after each,
// numbered as the paths that address them.
func TestListElementsInsertCopyAndMove(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	s.songs.put(&audio.SongDef{Name: "tune", BPM: 120, Scale: "major", Bars: [][]string{{"a"}, {"b"}, {"c"}}}, true)
	runOrFatal(t, s, &out, `
add tune.bars 1
dup tune.bars.3 0
mv tune.bars.1 3
set tune.bars.0.0 d
new pattern p
take p lead 0:60
dup p.track.1
set p.track.2.instr pad
set p.track.2.event.0.pos 8
mv p.track.2 0
! add tune.bars 6
! mv tune.bars.0 5
! dup tune.bars.5
! mv tune.bars.0 x
`)
	d, _ := s.songs.get("tune")
	if got := fmt.Sprint(d.Bars); got != "[[d] [] [b] [a] [c]]" || !strings.Contains(out.String(), "  3: a  -  -  -") {
		t.Fatalf("add at 1, dup of bar 3 at 0, mv of bar 1 to 3, then the copy's slot set: got %s\n%s", got, out.String())
	}
	p, _ := s.pats.get("p")
	var got []string
	for _, tr := range p.Track {
		got = append(got, fmt.Sprintf("%s@%d", tr.Instr, tr.Event[0].Pos))
	}
	if strings.Join(got, " ") != "pad@8 kick@0 lead@0" {
		t.Fatalf("dup of the lead edited as a pad and moved to 0: want pad@8 kick@0 lead@0, got %v", got)
	}
}

// TestGridJKMoveTheTrack: J and K move the cursor's track down and up through
// mv, the cursor with it, and stop at the ends.
func TestGridJKMoveTheTrack(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "new pattern g\ntake g lead 0:60\ntake g pad 0:48")
	a := newTUIApp(s, NewLogBuffer(50))
	a.openEditor("g", focusGrid)
	for _, r := range "JJJK" {
		a.editorKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r})
	}
	d, _ := s.pats.get("g")
	var got []string
	for _, tr := range d.Track {
		got = append(got, tr.Instr)
	}
	if strings.Join(got, " ") != "lead kick pad" || a.ed.trk != 1 {
		t.Fatalf("J J J K from the kick: want lead kick pad, the cursor on the kick at 1; got %v at %d:\n%s", got, a.ed.trk, strings.Join(a.log.lines, "\n"))
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

// TestUndoStepsBackOverEdits: undo [n] steps back over the last n commands that
// changed a document, whatever their verb; a command that changed none is no
// edit, and a script run inside one line undoes with it.
func TestUndoStepsBackOverEdits(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "two.slab"), []byte("new pattern r1\nnew pattern r2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runOrFatal(t, s, &out, fmt.Sprintf(`
new pattern a
set a.steps 32
take a lead 0:60
apply a
validate a
ls pattern
undo 2
run %s
undo
! undo 0
`, filepath.Join(dir, "two.slab")))
	if got := patternSteps(s, "a"); got != "16: kick 0" || s.has("r1") || s.has("r2") {
		t.Fatalf("want a as new, r1 and r2 gone, got %q:\n%s", got, out.String())
	}
	for _, want := range []string{"undid take: a back to 1 track", "undid set: a back to 1 track", "undid run: r1 removed, r2 removed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("want %q said:\n%s", want, out.String())
		}
	}
	runOrFatal(t, s, &out, "undo 5\n! undo")
	if s.has("a") || !strings.Contains(out.String(), "the history held 1 edit") {
		t.Fatalf("undo 5 over one edit: want a gone and the shortfall said:\n%s", out.String())
	}
}

// TestUndoRestoresWhatTheEditChanged: an undo puts back the entries one edit
// changed as they were, a new one removed and a deleted one back, with their
// marks and order, a mark an apply since made stale left dirty; a command that
// wrote many entries undoes as one, and the registry stays as it is.
func TestUndoRestoresWhatTheEditChanged(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	file := filepath.Join(t.TempDir(), "p.toml")
	runOrFatal(t, s, &out, fmt.Sprintf(`
new pattern a
new pattern src
new pattern dst
take src lead 0:60
apply all
mix src dst
undo
del src
mv a z
undo 2
save pattern %s
builtin pattern
undo
set a.steps 32
apply a
undo
`, file))
	if !strings.Contains(out.String(), "undid mix: dst back to 1 track") {
		t.Fatalf("want the mix's undo said:\n%s", out.String())
	}
	if got := strings.Join(s.pats.order, " "); got != "a src dst" || s.pats.src != file || patternSteps(s, "src") != "16: kick 0 lead 0" {
		t.Fatalf("want a, src and dst in order from %s, src whole, got %q from %s", file, got, s.pats.src)
	}
	if !strings.Contains(out.String(), "undid builtin: ") || !strings.Contains(out.String(), " patterns removed") {
		t.Fatalf("want builtin undone at once, counted:\n%s", out.String())
	}
	if s.pats.isDirty("src") || s.pats.isDirty("dst") || !s.pats.isDirty("a") || patternSteps(s, "a") != "16: kick 0" {
		t.Fatalf("want src and dst clean as applied, a at 16 steps and dirty: the registry holds its 32; dirty %v", s.pats.dirtyNames())
	}
	if p := audio.GetPattern(audio.PatternIDByName("a")); p == nil || p.Def().Steps != 32 {
		t.Fatal("undo must leave the registry as the apply left it")
	}
	for range undoDepth + 1 {
		runOrFatal(t, s, &out, "set a.steps 16")
	}
	out.Reset()
	runOrFatal(t, s, &out, "undo 1000")
	if !strings.Contains(out.String(), fmt.Sprintf("the history held %d edits", undoDepth)) {
		t.Fatalf("want the history bounded at %d:\n%s", undoDepth, out.String())
	}
}

// TestUndoKeyTakesBackAKeysEdit: u in the browser and in the editor's grid undoes
// the last edit, every command one key ran at once
func TestUndoKeyTakesBackAKeysEdit(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "new pattern g")
	a := newTUIApp(s, NewLogBuffer(50))
	a.w, a.h = 100, 24
	key := func(r rune) { a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}) }
	a.openEditor("g", focusGrid)
	a.ed.step = 4
	key(' ')
	if got := patternSteps(s, "g"); got != "16: kick 0,4" {
		t.Fatalf("space: want a hit at step 4, got %q", got)
	}
	key('u')
	if got := patternSteps(s, "g"); got != "16: kick 0" {
		t.Fatalf("u in the grid: want the hit's add and sets undone at once, got %q:\n%s", got, strings.Join(a.log.lines, "\n"))
	}
	a.closeEditor()
	a.cursorTo("g")
	key('d')
	key('d')
	key('u')
	if !s.pats.has("g") {
		t.Fatalf("u in the browser: want g back:\n%s", strings.Join(a.log.lines, "\n"))
	}
}

// waitFor polls an engine-side condition. Transport and slot commands travel
// the mixer's queue and take effect on its tick — asserting engine state
// synchronously after an exec races that tick (reliably so under -race).
// Registry state is the exception: RegisterPattern is mutex-synchronous and
// may be asserted directly. A loaded run drops ticks, and a verb under the race
// detector on a busy runner takes many seconds, so the deadline is wide.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
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
// outside the key lands on the nearest degree, the lower on a tie, with an
// accidental for the semitone left.
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
	type at struct{ pos, deg, oct, acc int }
	var got []at
	for _, e := range d.Track[0].Event {
		got = append(got, at{e.Pos, e.Deg, e.Oct, e.Acc})
	}
	if want := []at{{0, 0, 1, 0}, {4, 2, 1, 0}, {4, 4, 1, 0}, {8, 1, 1, 1}}; !slices.Equal(got, want) {
		t.Fatalf("take events %v, want %v (F#3 as F sharp)", got, want)
	}
	if !strings.Contains(out.String(), "1 note(s) outside E2 phrygian carry an accidental") {
		t.Fatalf("take must say what carries an accidental:\n%s", out.String())
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

// TestATrackMovesBetweenPatterns: cp and mix take tracks by their paths into a
// new pattern or onto another's tracks, and del of several deletes what each
// path named before any delete; in the editor y and Y yank tracks, p pastes them
// into the pattern open, the browser's p makes a new pattern of them, and a
// paste refuses a yanked track that has moved since.
func TestATrackMovesBetweenPatterns(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new pattern src
set src.steps 32
take src lead 0:60 4:64
take src pad 0:48:8
new pattern dst
cp src.track.1 src.track.2 lead_pad
mix src.track.1 src.track.2 dst
! cp src.track.9 nine
! mix src.track.1 nowhere
! mix src.track.1 src.track.1 dst
`)
	instrs := func(name string) string {
		d, _ := s.pats.get(name)
		in := []string{strconv.Itoa(d.Steps)}
		for _, t := range d.Track {
			in = append(in, t.Instr)
		}
		return strings.Join(in, " ")
	}
	if got := instrs("lead_pad"); got != "32 lead pad" {
		t.Fatalf("cp of two tracks: want a 32-step pattern of lead and pad, got %q", got)
	}
	if got := instrs("dst"); got != "32 kick lead pad" || instrs("src") != "32 kick lead pad" {
		t.Fatalf("mix of two tracks: want dst's kick, then src's lead and pad, src untouched; got %q", got)
	}
	a := newTUIApp(s, NewLogBuffer(50))
	key := func(r rune) { a.editorKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}) }
	a.openEditor("src", focusGrid)
	a.ed.trk = 1
	key('y')
	a.ed.trk = 2
	key('Y')
	a.openEditor("lead_pad", focusGrid)
	key('p')
	if got := instrs("lead_pad"); got != "32 lead pad lead pad" || a.ed.trk != 3 {
		t.Fatalf("y, Y then p: want src's lead and pad pasted after lead_pad's, got %q:\n%s", got, strings.Join(a.log.lines, "\n"))
	}
	a.closeEditor()
	a.w, a.h = 100, 24
	a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'p'})
	for _, r := range "two" {
		a.promptKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r})
	}
	a.promptKey(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter})
	if got := instrs("two"); got != "32 lead pad" {
		t.Fatalf("the browser's p: want a new pattern of the yanked lead and pad, got %q", got)
	}
	runOrFatal(t, s, &out, "! del src.track.2 src.track.9\ndel src.track.1 src.track.2\n")
	if got := instrs("src"); got != "32 kick" {
		t.Fatalf("del src.track.1 src.track.2: want src's kick alone, got %q", got)
	}
	a.openEditor("dst", focusGrid)
	key('p')
	if got := instrs("dst"); got != "32 kick lead pad" || !strings.Contains(strings.Join(a.log.lines, "\n"), "changed since it was yanked") {
		t.Fatalf("p after the yanked tracks were deleted: want a refusal and dst as it was, got %q", got)
	}
}

// trackNotes is a track's notes as step:midi under the session's key
func trackNotes(s *Session, name string, n int) string {
	d, _ := s.pats.get(name)
	var notes []string
	for _, e := range d.Track[n].Event {
		notes = append(notes, fmt.Sprintf("%d:%d", e.Pos, audio.StepPitch(s.root, s.scale, e.Deg, e.Oct, e.Acc)))
	}
	return strings.Join(notes, " ")
}

// TestSplitPartsATrackAtAPitch: split keeps a tonal track's notes from the pitch
// up and moves the rest to a new track of its voice right after it; a drum track,
// or a split leaving a side empty, is refused.
func TestSplitPartsATrackAtAPitch(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
key 60 major
new pattern p
take p lead 0:72 4:60 8:76 12:55
take p pad 0:48
split p.track.1 64
! split p.track.0 60
! split p.track.1 80
! split p.track.1 40
`)
	if got := patternSteps(s, "p"); got != "16: kick 0 lead 0,8 lead 4,12 pad 0" {
		t.Fatalf("split at E4: want lead's high notes kept and the low after it, before pad; got %q", got)
	}
	if got := trackNotes(s, "p", 2); got != "4:60 12:55" {
		t.Fatalf("the lower line: want 4:60 12:55, got %q", got)
	}
}

// TestShiftMovesEveryNoteOrNone: shift moves the tracks' notes by octave, by
// degree carried into the octave, by semitone spelt on the key, or by step rotated
// around the pattern; a drum track takes only step, and a note past a step's
// bounds refuses the whole shift.
func TestShiftMovesEveryNoteOrNone(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
key 60 major
new pattern p
take p lead 0:60 4:64 15:71
shift p.track.1 oct +1
shift p.track.1 deg +3
shift p.track.1 semi -1
shift p.track.0 p.track.1 step 2
! shift p.track.0 oct 1
`)
	// C5 E5 B5 up three degrees, F5 A5 E6, a semitone down E5 G#5 D#6, two steps on
	if got := trackNotes(s, "p", 1); got != "1:87 2:76 6:80" {
		t.Fatalf("lead after oct, deg, semi and step: want 1:87 2:76 6:80, got %q", got)
	}
	d, _ := s.pats.get("p")
	if d.Track[0].Event[0].Pos != 2 || d.Track[1].Event[2].Acc != 1 {
		t.Fatalf("want the kick moved to step 2 and G#5 spelt G with a sharp, got %+v", d.Track)
	}
	runOrFatal(t, s, &out, "new pattern q\ntake q lead 0:60\n! shift q.track.1 p.track.1 oct +7\n")
	if got := trackNotes(s, "q", 1) + " " + trackNotes(s, "p", 1); got != "0:60 1:87 2:76 6:80" {
		t.Fatalf("a shift one note cannot take: want both tracks as they were, got %q", got)
	}
}

// TestMixedTracksLoopAtOneLength: tracks of other lengths join a pattern at the
// least common multiple of the lengths, each track repeated to fill it; past 64
// steps the pattern keeps its length and a longer loop is cut, as the command
// says.
func TestMixedTracksLoopAtOneLength(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new pattern a
new pattern b
set b.steps 24
new pattern c
set c.steps 20
mix b.track.0 a
mix c.track.0 a
`)
	d, _ := s.pats.get("a")
	var got []string
	for _, tr := range d.Track {
		var pos []string
		for _, e := range tr.Event {
			pos = append(pos, strconv.Itoa(e.Pos))
		}
		got = append(got, strings.Join(pos, ","))
	}
	if d.Steps != 48 || strings.Join(got, " ") != "0,16,32 0,24 0,20,40" || !strings.Contains(out.String(), "cut at step 48") {
		t.Fatalf("want 48 steps, a's kick every 16, b's every 24, c's every 20 cut at 48; got %d steps, %v:\n%s", d.Steps, got, out.String())
	}
}

// patternSteps is a pattern's steps, then each track's voice and event steps
func patternSteps(s *Session, name string) string {
	d, _ := s.pats.get(name)
	parts := []string{strconv.Itoa(d.Steps) + ":"}
	for _, tr := range d.Track {
		var pos []string
		for _, e := range tr.Event {
			pos = append(pos, strconv.Itoa(e.Pos))
		}
		parts = append(parts, tr.Instr, strings.Join(pos, ","))
	}
	return strings.Join(parts, " ")
}

// TestTracksMergeIntoOne: mix into a track path joins the tracks' notes into
// that track, which keeps its voice, at a length each loops in; a note already
// at its step is kept once and counted, and a drum track merging into a tonal
// one, the reverse, or into itself is refused before anything changes.
func TestTracksMergeIntoOne(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new pattern m
set m.steps 32
take m lead 0:60 16:64
take m pad 0:60 8:67
new pattern u
take u pad 0:60 4:62
! mix m.track.0 u.track.1
! mix m.track.1 u.track.0
! mix u.track.1 u.track.1
`)
	if got := patternSteps(s, "u"); got != "16: kick 0 pad 0,4" {
		t.Fatalf("refused merges must leave u as it was, got %q", got)
	}
	runOrFatal(t, s, &out, "mix m.track.1 m.track.2 u.track.1")
	if got := patternSteps(s, "u"); got != "32: kick 0,16 pad 0,4,8,16,16,20" || !strings.Contains(out.String(), "2 note(s) already at their step kept once") {
		t.Fatalf("want u at 32 steps, its kick repeated, its pad holding m's lead and pad, two notes kept once; got %q:\n%s", got, out.String())
	}
}

// TestBarsMakeANewPattern: cp of bar paths makes a pattern of one pattern's bars
// in the order named, every track sliced at the bar lines, a short last bar kept
// short and a bar named twice played twice; past 64 steps, a bar past the end or
// bars of two patterns are refused.
func TestBarsMakeANewPattern(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, `
new pattern b
set b.steps 40
take b lead 0:60 17:62 33:64 38:65
new pattern other
cp b.bar.2 b.bar.0 b.bar.2 loop
! cp b.bar.0 b.bar.1 b.bar.0 b.bar.1 b.bar.2 long
! cp b.bar.3 three
! cp b.bar.0 other.bar.0 both
`)
	if got := patternSteps(s, "loop"); got != "32: kick 8 lead 1,6,8,25,30" {
		t.Fatalf("want bar 2's 8 steps, bar 0's 16, then bar 2 again, got %q", got)
	}
	if s.has("long") || s.has("three") || s.has("both") {
		t.Fatal("a refused cp must make no pattern")
	}
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

// trioScript puts a pattern of three tracks, a kick, a snare and a hihat, in slot 1
const trioScript = `
new pattern trio
new pattern part
set part.track.0.instr snare
mix part trio
set part.track.0.instr hihat
mix part trio
slot 1 trio
`

// TestMuteTrackAndSoloShapeTheSlotsMask: a slot's soloed tracks sound alone and its
// muted ones never, and where and stat name them once the slot plays the pattern.
func TestMuteTrackAndSoloShapeTheSlotsMask(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, trioScript+`
mute track 1 2 on
! mute track 1 3 on
! mute track 2 0 on
`)
	if m := s.tracks[1].mask(); m != ^uint32(1<<2) {
		t.Fatalf("muting track 2 left mask %b", m)
	}
	runOrFatal(t, s, &out, "solo track 1 0 on\nsolo track 1 2 on")
	if m := s.tracks[1].mask(); m != 1 {
		t.Fatalf("tracks 0 and 2 soloed, 2 muted, left mask %b", m)
	}
	waitFor(t, func() bool { return s.eng.SlotPattern(1) == audio.PatternIDByName("trio") }, "slot 1 never played trio")
	out.Reset()
	runOrFatal(t, s, &out, "where\nstat")
	for _, want := range []string{"slot1=trio (track 2 muted, tracks 0 2 soloed)", "slot 1 trio: track 2 muted, tracks 0 2 soloed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("want %q in:\n%s", want, out.String())
		}
	}
}

// TestTrackMutesGoWithTheSlotsPattern: the tracks muted in a slot sound again, as the
// log says, when an apply moves them or another pattern comes, and a track added
// after them moves none.
func TestTrackMutesGoWithTheSlotsPattern(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, trioScript+`
mute track 1 1 on
add trio.track
set trio.track.3.instr clap
apply trio
`)
	if s.tracks[1].muted != 1<<1 {
		t.Fatal("a track added after the muted one dropped its mute")
	}
	runOrFatal(t, s, &out, "del trio.track.0\napply trio")
	if s.tracks[1] != (trackMutes{}) || !strings.Contains(out.String(), "as the apply moved its tracks") {
		t.Fatalf("deleting a track before the muted one kept the mute:\n%s", out.String())
	}
	runOrFatal(t, s, &out, "mute track 1 0 on\nslot 1 part")
	if s.tracks[1] != (trackMutes{}) || !strings.Contains(out.String(), "as the slot changes pattern") {
		t.Fatalf("another pattern in the slot kept the mute:\n%s", out.String())
	}
}

// TestMixerAndGridMuteTheCursorsTrack: on a track row the mixer's m and space mute
// the track and s solos it, and the grid's m mutes its cursor's track, through the
// commands a script runs.
func TestMixerAndGridMuteTheCursorsTrack(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, trioScript)
	waitFor(t, func() bool { return s.eng.SlotPattern(1) == audio.PatternIDByName("trio") }, "slot 1 never played trio")
	a := newTUIApp(s, NewLogBuffer(50))
	a.term, a.w, a.h = frameTerm{}, 100, 24
	key := func(k terminal.Key, r rune) {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: k, Rune: r})
		a.render()
	}
	a.render()
	key(terminal.KeyTab, 0)
	for range 3 { // slot 0's title, slot 1's, its tracks 0 and 1
		key(terminal.KeyDown, 0)
	}
	key(terminal.KeyRune, 'm')
	key(terminal.KeyRune, 's')
	if s.eng.IsSlotMuted(1) || s.tracks[1] != (trackMutes{muted: 1 << 1, soloed: 1 << 1}) {
		t.Fatalf("m and s on track 1's row left %+v", s.tracks[1])
	}
	key(terminal.KeySpace, ' ')
	if s.tracks[1].muted != 0 {
		t.Fatal("space must let track 1 sound again")
	}
	key(terminal.KeyRune, 'e')
	key(terminal.KeyRune, 'm')
	if s.tracks[1].muted != 1 {
		t.Fatalf("m in trio's grid left mutes %b", s.tracks[1].muted)
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
rip %s fast 2 bpm 180 nodrums lead soft
! rip %s slow 2 bpm 10
! rip %s slow 2 lead sideways           # not a lead rule
rip %s quiet 4 nodrums
ls pattern loop*
! notes nothing
notes loop
compress quiet 5
song quiet score
validate all
`, filepath.Join(t.TempDir(), "missing.wav"), wav, wav, wav, wav, wav, wav))
	if !strings.Contains(out.String(), "note(s) at") || !strings.Contains(out.String(), "bar 1 step") || !strings.Contains(out.String(), "support 0.") || !strings.Contains(out.String(), ", bright ") || !strings.Contains(out.String(), "brightness unmeasured on ") {
		t.Errorf("notes listed no transcript with support and brightness:\n%s", out.String())
	}
	// bpm <n> reads the recording at that tempo, the bar line decoded
	if fast := s.rips["fast"]; fast == nil || fast.rip.BPM != 180 {
		t.Errorf("rip at 180 bpm: %+v", fast)
	}
	// a rip without a kit is compared without one, so the render's attacks
	// cannot grade the song on drums it has none of; score prints both grades
	if !strings.Contains(out.String(), "drums 1.00") || !strings.Contains(out.String(), "quiet sounds ") {
		t.Errorf("the kitless song's score:\n%s", out.String())
	}
	if !s.pats.has("loop_beat_1") || !s.pats.isDirty("loop_beat_1") {
		t.Fatalf("no dirty loop_beat_1 in the document:\n%s", out.String())
	}
	if s.pats.has("quiet_beat_1") || !(s.pats.has("quiet_bass_1") || s.pats.has("quiet_melody_1")) {
		t.Errorf("nodrums wrote a beat %v and notes %v:\n%s", s.pats.has("quiet_beat_1"), s.pats.has("quiet_bass_1") || s.pats.has("quiet_melody_1"), out.String())
	}
	if s.bpm < bpm-3 || s.bpm > bpm+3 {
		t.Errorf("tempo %d, want about %d", s.bpm, bpm)
	}
	if !strings.Contains(out.String(), "ripped") {
		t.Errorf("no report:\n%s", out.String())
	}
}

// TestRipReadsAMIDIFileAsAScore: a .mid given to rip is its transcript, nothing
// heard: patterns, the shipped voices under the rip's names and the tempo, and
// compress, song, notes, agree and truth work from it as from a recording's
// rip, while what hears a recording refuses a score, which has none.
func TestRipReadsAMIDIFileAsAScore(t *testing.T) {
	const bpm = 100
	step := 60.0 / bpm / audio.StepsPerBeat
	var notes []audio.RipNote
	var hits []audio.RipHit
	for i := range 2 * audio.StepsPerBar / 4 {
		at := float64(4*i) * step
		hits = append(hits, audio.RipHit{Start: at, Drum: audio.InstrKick, Vel: 1})
		notes = append(notes, audio.RipNote{Start: at, End: at + 2*step, MIDI: 64 + 2*(i%4), Vel: 0.8})
		if i%2 == 0 {
			notes = append(notes, audio.RipNote{Start: at, End: at + 6*step, MIDI: 40, Vel: 0.9, Bass: true})
		}
	}
	notes = append(notes, audio.RipNote{Start: 0, End: step, MIDI: 110, Vel: 0.5}) // over the rip's range
	var b bytes.Buffer
	if err := audio.WriteMIDI(&b, notes, hits, bpm); err != nil {
		t.Fatal(err)
	}
	mid := filepath.Join(t.TempDir(), "score.mid")
	os.WriteFile(mid, b.Bytes(), 0o644)
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, fmt.Sprintf(`
! rip %s other bpm 120                 # a score's tempo is its own
rip %s score
! load %s                              # a score is ripped, not loaded
notes score
compress score 5
song score
! fit score                            # no recording to fit to
! song score score
agree score score
truth score %s
validate all
`, mid, mid, mid, mid))
	rp := s.rips["score"]
	if rp == nil || rp.x != nil || s.bpm != bpm || !strings.Contains(out.String(), "1 note(s) outside E1 to C7 dropped") {
		t.Fatalf("the score as a rip at %d bpm:\n%s", s.bpm, out.String())
	}
	for _, v := range []string{"score_melody_voice", "score_bass_voice"} {
		if !s.voices.has(v) {
			t.Errorf("no voice %s:\n%s", v, out.String())
		}
	}
	if !s.pats.has("score_beat_1") || !s.songs.has("score") || !strings.Contains(out.String(), "rips back at") || strings.Contains(out.String(), "support") || strings.Contains(out.String(), "heard by level") {
		t.Errorf("patterns, the song, its score, and nothing read in a recording:\n%s", out.String())
	}
	// the file's own notes are all heard but the one dropped
	if n := len(rp.rip.Notes); n != len(notes)-1 || !strings.Contains(out.String(), fmt.Sprintf("against %d note(s) given, transposed +0", n+1)) ||
		strings.Count(out.String(), "only given 0") != 2 || !strings.Contains(out.String(), "melody only given: D8@0.00\n") {
		t.Errorf("the score did not agree with its file:\n%s", out.String())
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

// TestACorpusGoesOnWhereItStopped: parts rendered apart and an export run again
// over them keep every loop on disk and write the rest, byte for byte, as one
// unbroken export does; only the last, whole export writes corpus.json.
func TestACorpusGoesOnWhereItStopped(t *testing.T) {
	dir := t.TempDir()
	one, parts := filepath.Join(dir, "one"), filepath.Join(dir, "parts")
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, fmt.Sprintf(`
builtin pattern
corpus %[1]s 5 1 aug gen nowav
corpus %[2]s 5 1 aug gen nowav part 0/2
corpus %[2]s 5 1 aug gen nowav part 1/2
`, one, parts))
	if _, err := os.Stat(filepath.Join(parts, "corpus.json")); err == nil {
		t.Error("a part wrote corpus.json")
	}
	kept := filepath.Join(parts, "loop_001.json")
	if err := os.WriteFile(kept, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(parts, "loop_003.json")); err != nil { // stopped before its label
		t.Fatal(err)
	}
	runOrFatal(t, s, &out, fmt.Sprintf("corpus %s 5 1 aug gen nowav\n", parts))
	files, err := os.ReadDir(one)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		want, _ := os.ReadFile(filepath.Join(one, f.Name()))
		got, _ := os.ReadFile(filepath.Join(parts, f.Name()))
		if f.Name() == "loop_001.json" {
			want = []byte("kept")
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from one unbroken export's", f.Name())
		}
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
	// two bars at 100 bpm: the shortest loop the rip takes, since every judge
	// of the refinement renders and hears the whole song
	buf, err := s.eng.RenderMusic(slots, 100, 2, audio.DefaultRootNote, audio.ScalePhrygian, nil, 1)
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
	if !ok || len(d.Bars) != 2 || d.BPM != s.bpm || !s.songs.isDirty("loop") {
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
		t.Errorf("%d dictionary pattern(s) for 2 bars of 3 parts:\n%s", dict, out.String())
	}
	if fi, err := os.Stat(song); err != nil || fi.Size() < 1000 {
		t.Errorf("the song did not render: %v", err)
	}
	runOrFatal(t, s, &out, "fit loop 1\n")
	if !strings.Contains(out.String(), "registered; the patterns naming it play it") {
		t.Errorf("fit replaced no voice:\n%s", out.String())
	}
	// the two witnesses and a truth: a rip agrees with itself on every note,
	// and read back from its own transcript, as text and as MIDI, hears every
	// note given, at every level the recording plays them
	out.Reset()
	runOrFatal(t, s, &out, "notes loop\n")
	truth, mid := filepath.Join(dir, "truth.txt"), filepath.Join(dir, "truth.mid")
	os.WriteFile(truth, out.Bytes(), 0o644)
	out.Reset()
	runOrFatal(t, s, &out, fmt.Sprintf("agree loop loop\n! agree loop nothing\ntruth loop %s\n! truth loop %s\n! notes loop %s\nnotes loop %s\ntruth loop %s\n", truth, wav, truth, mid, mid))
	rp := s.rips["loop"]
	n := len(rp.rip.Notes)
	if strings.Count(out.String(), fmt.Sprintf("against %d note(s) given, transposed +0", n)) != 2 || strings.Contains(out.String(), "only loop:") || strings.Contains(out.String(), "only given:") {
		t.Errorf("the rip did not agree with itself:\n%s", out.String())
	}
	levels := regexp.MustCompile(`(?m)^register .*; alone .*$`).FindAllString(out.String(), -1)
	if len(levels) != 2 {
		t.Errorf("truth gave no count by level for the register:\n%s", out.String())
	}
	for _, l := range levels {
		for _, m := range regexp.MustCompile(`(\d+)/(\d+)`).FindAllStringSubmatch(l, -1) {
			if m[1] != m[2] {
				t.Errorf("a note given went unheard by its level or company (%s): %s", m[0], l)
			}
		}
	}
	// one line of the song on its own: the instruments named after the file
	runOrFatal(t, s, &out, fmt.Sprintf("! song loop %s nothing\nsong loop %s kick\n", song, song))
	if fi, err := os.Stat(song); err != nil || fi.Size() < 1000 {
		t.Errorf("the kick alone did not render: %v", err)
	}
	// a refinement round: the voices and the steps each kept only when the song
	// sounds more like the recording, judged on the recording's own frames with
	// the rip-back reported beside it, and the song still valid after
	runOrFatal(t, s, &out, "! refine nothing\nrefine loop 1\nvalidate all\n")
	if !strings.Contains(out.String(), "like the recording (pitch") || !strings.Contains(out.String(), "before refining") || !strings.Contains(out.String(), "round 1: steps") {
		t.Errorf("refine reported no round:\n%s", out.String())
	}
	if err := audio.ValidateSong(d); err != nil {
		t.Error(err)
	}
	// a tonal floor no candidate can keep to leaves every voice in hand: the
	// refinement picks among voices that sound like the recording's notes
	var floors [3]tonalFloor
	for reg := range floors {
		floors[reg].targets = fitTargets(rp.x, rp.rate, lineNotes(rp.rip, reg), 2)
	}
	before := fmt.Sprintf("%p %p %p", rp.rip.MelodyVoice, rp.rip.UnderVoice, rp.rip.BassVoice)
	out.Reset()
	if _, err := s.refineVoices("loop", rp, d, 0, s.refineOpt, floors); err != nil {
		t.Fatal(err)
	}
	if after := fmt.Sprintf("%p %p %p", rp.rip.MelodyVoice, rp.rip.UnderVoice, rp.rip.BassVoice); after != before || !strings.Contains(out.String(), "voice stays") {
		t.Errorf("a voice left its floor:\n%s", out.String())
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
	// enter on a script runs it behind the screen
	if err := os.WriteFile(filepath.Join(dir, "make.slab"), []byte("new pattern from_script_file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key(terminal.KeyCtrlO, 0)
	a.files.cur = slices.Index(names(), "make.slab")
	key(terminal.KeyEnter, 0)
	for deadline := time.Now().Add(20 * time.Second); a.busy != "" && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		key(terminal.KeyNone, 0)
	}
	if a.files.open || !s.pats.has("from_script_file") {
		t.Fatalf("enter on a script: open %v, ran %v", a.files.open, s.pats.has("from_script_file"))
	}
	key(terminal.KeyCtrlO, 0)
	a.files.cur = slices.Index(names(), "loop.wav")
	key(terminal.KeyEnter, 0)
	if a.mode != modePrompt || a.editField.Value() != "loop" {
		t.Fatalf("enter on a recording: mode %d, name %q", a.mode, a.editField.Value())
	}
	key(terminal.KeyEnter, 0)
	if a.files.open || a.busy == "" {
		t.Fatalf("the rip did not start behind the screen: open %v, busy %q", a.files.open, a.busy)
	}
	waitFor(t, func() bool { key(terminal.KeyNone, 0); return a.busy == "" }, "the rip never ended")
	if !s.pats.has("loop_beat_1") || a.selName() != "loop_melody_1" && a.selName() != "loop_beat_1" {
		t.Fatalf("the recording was not ripped, or the browser is not on it (%q):\n%s", a.selName(), out.String())
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

// TestCorpusAugmentsAndLabelsTheVoices: an augmented corpus plays each loop's
// tonal tracks through voices of random genes near the registered ones and
// labels the events with those genes, the registered voices are as they were
// afterwards, and the audio is not the clean render.
func TestCorpusAugmentsAndLabelsTheVoices(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	before := audio.VoiceDefByName("lead").Genes()
	clean, err := s.corpus(tuneOptions{loops: 2, bars: 1, seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	aug, err := s.corpus(tuneOptions{loops: 2, bars: 1, seed: 3, aug: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(audio.VoiceDefByName("lead").Genes(), before) {
		t.Error("the registered lead was not put back")
	}
	changed, differs := 0, 0
	for i := range aug {
		if aug[i].slots != clean[i].slots {
			t.Fatalf("loop %d drew other patterns under aug", i)
		}
		for name, v := range aug[i].voices {
			if !reflect.DeepEqual(v.Genes(), clean[i].voices[name].Genes()) {
				changed++
			}
		}
		var d float64
		for k := range min(len(aug[i].audio), len(clean[i].audio)) {
			d += math.Abs(aug[i].audio[k] - clean[i].audio[k])
		}
		if d > 0 {
			differs++
		}
	}
	if changed == 0 || differs != len(aug) {
		t.Errorf("%d voice(s) changed, %d of %d loop(s) differ from the clean render", changed, differs, len(aug))
	}
	// an augmented loop sounds off A440 by up to a quarter tone, a clean one at it
	tuned := 0
	for i := range aug {
		if aug[i].tuning < -50 || aug[i].tuning > 50 || clean[i].tuning != 0 {
			t.Errorf("loop %d tuned %+.1f cents augmented, %+.1f clean", i, aug[i].tuning, clean[i].tuning)
		}
		if aug[i].tuning != 0 {
			tuned++
		}
	}
	if tuned == 0 || s.eng.Tuning() != 0 {
		t.Errorf("%d loop(s) tuned off A440, the engine left at %+.0f", tuned, s.eng.Tuning())
	}
	// the tempos the loops cycle through are the verb's to set
	if tempos, err := parseTempos("90-100"); err != nil || len(tempos) != 6 || tempos[0] != 90 || tempos[5] != 100 {
		t.Errorf("tempos 90-100: %v %v", tempos, err)
	}
	if _, err := parseTempos("40,100"); err == nil {
		t.Error("a tempo under the sequencer's range was accepted")
	}
	slow, err := s.corpus(tuneOptions{loops: 2, bars: 1, seed: 3, tempos: []int{80}})
	if err != nil || len(slow) != 2 || slow[0].bpm != 80 || slow[1].bpm != 80 {
		t.Errorf("a corpus at 80 bpm: %v, %d loop(s)", err, len(slow))
	}
	dir := t.TempDir()
	runOrFatal(t, s, &out, "corpus "+dir+" 1 1 aug tempos 100,120\n")
	data, err := os.ReadFile(filepath.Join(dir, "loop_000.json"))
	if err != nil || !strings.Contains(string(data), "\"voice\":[") || !strings.Contains(string(data), "\"tuning\":") {
		t.Errorf("the labels carry no voice genes or tuning: %v\n%s", err, data)
	}
	if !strings.Contains(out.String(), "random genes") {
		t.Errorf("corpus did not say it augmented:\n%s", out.String())
	}
}

// TestBenchGradesTheFrontEnds: bench renders fresh loops, clean and augmented,
// and grades the analysis on both
func TestBenchGradesTheFrontEnds(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "bench 1 2\n")
	if !strings.Contains(out.String(), "analysis on 1 clean loop(s)") || !strings.Contains(out.String(), "analysis on 1 augmented loop(s)") {
		t.Errorf("bench graded:\n%s", out.String())
	}
}

// TestBenchVerboseListsEveryLoop: bench ... verbose lists a line per loop it
// rendered, clean and augmented, and then the three worst by total
func TestBenchVerboseListsEveryLoop(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	const loops = 2
	runOrFatal(t, s, &out, fmt.Sprintf("bench %d 2 verbose\n", loops))
	var listed, worst int
	for _, l := range strings.Split(out.String(), "\n") {
		switch {
		case strings.HasPrefix(l, "loop "):
			listed++
		case strings.HasPrefix(l, "worst "):
			worst++
		}
	}
	if listed != 2*loops || worst != 3 {
		t.Errorf("%d loop(s) listed and %d worst, want %d and 3:\n%s", listed, worst, 2*loops, out.String())
	}
}

// TestRunScriptVerb: run executes a script's lines in the session in hand, stops
// at a failing line with its number, and a quit in it ends the script alone
func TestRunScriptVerb(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.slab")
	bad := filepath.Join(dir, "bad.slab")
	os.WriteFile(good, []byte("new pattern from_script\nquit\nnew pattern never\n"), 0o644)
	os.WriteFile(bad, []byte("new pattern twice\nnew pattern twice\n"), 0o644)
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "run "+good+"\n")
	if !s.pats.has("from_script") || s.pats.has("never") {
		t.Errorf("run made %v and %v", s.pats.has("from_script"), s.pats.has("never"))
	}
	if err := Execute(s, "run "+bad); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("a failing line gave %v", err)
	}
	// a script's relative paths are its directory's, wherever soundlab runs:
	// the recipe writes a document beside itself and a nested run loads it
	beside := filepath.Join(dir, "beside.slab")
	nested := filepath.Join(dir, "nested.slab")
	os.WriteFile(beside, []byte("new pattern kept\nsave pattern kept.toml\nrun nested.slab\n"), 0o644)
	os.WriteFile(nested, []byte("load pattern kept.toml\n"), 0o644)
	runOrFatal(t, s, &out, "run "+beside+"\n")
	if _, err := os.Stat(filepath.Join(dir, "kept.toml")); err != nil || !s.pats.has("kept") || s.base != "" {
		t.Errorf("the recipe's file %v, pattern kept %v, base %q after", err, s.pats.has("kept"), s.base)
	}
}

// TestLongVerbsRunBehindTheScreen: a long verb typed in the TUI runs on its own
// goroutine under a working box, keys wait for it, and its output and its end
// reach the log and the screen
func TestLongVerbsRunBehindTheScreen(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	lb := NewLogBuffer(50)
	s.SetOut(lb)
	a := newTUIApp(s, lb)
	a.term, a.w, a.h = frameTerm{}, 100, 40
	a.render()
	a.exec("bench 1 2")
	if a.busy == "" {
		t.Fatal("bench did not go behind the screen")
	}
	a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'n'})
	if a.mode != modeNormal {
		t.Error("a key opened a prompt while busy")
	}
	a.render()
	waitFor(t, func() bool {
		a.handle(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
		return a.busy == ""
	}, "bench never ended")
	lines := strings.Join(lb.Lines(), "\n")
	if !strings.Contains(lines, "analysis on 1 clean loop(s)") || !strings.Contains(lines, "keys wait") {
		t.Errorf("the log after:\n%s", lines)
	}
}

// TestCorpusGeneratesPartsTheBankNeverHad: gen draws a beat, a melody or a bass
// line of its own for some loops, each valid, labelled with its notes, and
// different from the bank's draw; the aug property, the same music with and
// without, holds with gen on both sides
func TestCorpusGeneratesPartsTheBankNeverHad(t *testing.T) {
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	plain, err := s.corpus(tuneOptions{loops: 6, bars: 1, seed: 5})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := s.corpus(tuneOptions{loops: 6, bars: 1, seed: 5, gen: true})
	if err != nil {
		t.Fatal(err)
	}
	both, err := s.corpus(tuneOptions{loops: 6, bars: 1, seed: 5, gen: true, aug: true})
	if err != nil {
		t.Fatal(err)
	}
	changed, notes := 0, 0
	accompanied := 0
	for i := range gen {
		if gen[i].slots[2] != audio.PatternSilence {
			accompanied++
		}
		if gen[i].slots != plain[i].slots {
			changed++
		}
		if gen[i].slots != both[i].slots {
			t.Errorf("loop %d: gen drew %v alone and %v with aug", i, gen[i].slots, both[i].slots)
		}
		for _, b := range gen[i].want.Bars {
			notes += len(b.Melody) + len(b.Bass)
		}
	}
	if changed == 0 || notes == 0 {
		t.Errorf("%d loop(s) changed by gen, %d note(s) labelled", changed, notes)
	}
	if accompanied == 0 {
		t.Errorf("no loop got an accompaniment in the free slot")
	}
	for _, n := range []string{"corpus_beat", "corpus_melody", "corpus_bass"} {
		if id := audio.PatternIDByName(n); id != audio.PatternSilence {
			if p := audio.GetPattern(id); p == nil || audio.ValidatePattern(p) != nil {
				t.Errorf("%s does not validate", n)
			}
		}
	}
	runOrFatal(t, s, &out, "corpus "+t.TempDir()+" 1 1 gen aug\n")
	if !strings.Contains(out.String(), "generated beats") {
		t.Errorf("corpus did not say it generated:\n%s", out.String())
	}
}

// TestLoadTellsAFileByWhatItIs: load and merge take a file alone and read it
// by its tables, run a .slab, and send a .wav to rip; run refuses a document
// with the same hint; play plays a .wav as it is.
func TestLoadTellsAFileByWhatItIs(t *testing.T) {
	dir := t.TempDir()
	pat := filepath.Join(dir, "pats.toml")
	os.WriteFile(pat, []byte("[[pattern]]\nname = \"by_tables\"\nsteps = 16\n[[pattern.track]]\ninstr = \"kick\"\nevent = [{ pos = 0, vel = 1.0 }]\n"), 0o644)
	slab := filepath.Join(dir, "make.slab")
	os.WriteFile(slab, []byte("new pattern by_script\n"), 0o644)
	wav := filepath.Join(dir, "clip.wav")
	writeWAV(t, wav, thumpLoop())
	var out bytes.Buffer
	s := newTestSession(t, &out)
	defer teardown(t, s)
	runOrFatal(t, s, &out, "load "+pat+"\nmerge "+slab+"\n! load "+wav+"\n! run "+pat+"\nplay "+wav+" 0.5\n")
	if !s.pats.has("by_tables") || !s.pats.has("by_script") {
		t.Errorf("by tables %v, by script %v:\n%s", s.pats.has("by_tables"), s.pats.has("by_script"), out.String())
	}
	if err := Execute(s, "load "+wav); err == nil || !strings.Contains(err.Error(), "rip <file.wav>") {
		t.Errorf("load of a recording: %v", err)
	}
	if err := Execute(s, "run "+pat); err == nil || !strings.Contains(err.Error(), "load or merge") {
		t.Errorf("run of a document: %v", err)
	}
	if !strings.Contains(out.String(), "playing "+wav) {
		t.Errorf("play of a recording:\n%s", out.String())
	}
}
