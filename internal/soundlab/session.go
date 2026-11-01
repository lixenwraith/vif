//go:build !vif_headless && !vif_noaudio && !wasm

// Package soundlab is the audio authoring environment over pkg/audio: a session
// holding the engine and the working documents, the verb table every front end
// dispatches through (Execute), the script runner and the full-screen TUI over any
// terminal.Terminal. cmd/soundlab is its terminal command; a host with its own
// screen and speakers runs it in-process, handing NewSession the speakers.
package soundlab

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/audio/ripnet"
)

// Session owns the engine and the working documents: the sounds and the voices,
// one file, and the patterns. Single-goroutine by construction, every mutation
// funnelling through Execute, which is what lets document and registry pointers
// flow into RenderPreview without locks.
type Session struct {
	eng    *audio.AudioEngine
	sounds *doc[*audio.SoundDef]
	voices *doc[*audio.VoiceDef]
	pats   *doc[*audio.PatternDef]
	out    io.Writer

	// bpm mirrors the last requested tempo so `note` can size durations in
	// steps. The sequencer clamps and beat-quantizes its own copy; one pending
	// change of skew does not matter for an audition length.
	bpm int
	// root, scale and prog mirror the harmony `key` set, which the mixer keeps to
	// itself: the editors name notes and place a played one on a degree by them
	root  int
	scale audio.ScaleID
	prog  []int
	// slots is what slot last asked of each sequencer slot; the engine reports
	// what sounds a mixer tick later, which render cannot wait for
	slots    [audio.MusicSlots]audio.PatternID
	startErr error
	files    Files
	// rip is what rip listens with, the root's audio/rip.toml when one holds it,
	// and ripSrc that file, where tune writes; frontEnd is the model the root's
	// audio/ripnet.bin holds, nil for the package's own analysis
	rip      audio.RipParams
	ripSrc   string
	frontEnd *ripnet.FrontEnd
	// rips keeps what rip heard of each recording, by the name it was ripped as,
	// with the samples, so fit and compress work on it without the file; songs
	// are the timelines compress makes and the pattern document carries
	rips  map[string]*ripped
	songs *doc[*audio.SongDef]
	// fit and compressOpt size the searches rip, fit and compress run
	fit         fitOptions
	compressOpt compressOptions
	refineOpt   refineOptions
	// song is the timeline being conducted through the live sequencer, nil when
	// none; ticked says a front end drives conduct, so song play returns at once
	song   *songPlay
	ticked bool
}

// Files is where the game reads its audio overrides and where an untitled save
// must land for the next run to read it first, resolved as vif resolves them by
// ResolveFiles, with the mixer period vif.toml sets
type Files struct {
	music, sounds             string // "" = the embedded bank
	rip                       string // "" = the built-in parameters
	model                     string // "" = no model: the analysis listens
	musicTarget, soundsTarget string // "" = no configuration root
	ripTarget                 string
	buffer                    time.Duration
	skipped                   error // vif.toml keys this build does not know
}

// ResolveFiles applies vif's -config-dir, -config-music and -config-sounds,
// and vif.toml's [paths] root and [audio] buffer_ms as vif does. Those are all it
// reads, so a key another vif build knows is skipped, never dropped as a run does.
func ResolveFiles(dir, music, sounds string) (Files, error) {
	if err := paths.CheckRoot(dir); err != nil {
		return Files{}, err
	}
	settings, _, err := paths.LoadSettings(dir)
	var f Files
	if errors.As(err, new(paths.UnknownKeys)) {
		f.skipped, err = err, nil
	}
	if err != nil {
		return Files{}, err
	}
	roots := paths.ConfigRoots(cmp.Or(dir, settings.Paths.Root))
	f.buffer = time.Duration(settings.Audio.BufferMs) * time.Millisecond
	if f.music, err = paths.ConfigFile(roots, music, paths.AudioDirName, paths.MusicConfigFile); err != nil {
		return f, fmt.Errorf("music config: %w", err)
	}
	if f.sounds, err = paths.ConfigFile(roots, sounds, paths.AudioDirName, paths.SoundConfigFile); err != nil {
		return f, fmt.Errorf("sound config: %w", err)
	}
	if f.rip, err = paths.ConfigFile(roots, "", paths.AudioDirName, paths.RipConfigFile); err != nil {
		return f, fmt.Errorf("rip config: %w", err)
	}
	if f.model, err = paths.ConfigFile(roots, "", paths.AudioDirName, paths.RipModelFile); err != nil {
		return f, fmt.Errorf("rip model: %w", err)
	}
	f.musicTarget, _ = paths.ConfigTarget(roots, music, paths.AudioDirName, paths.MusicConfigFile)
	f.soundsTarget, _ = paths.ConfigTarget(roots, sounds, paths.AudioDirName, paths.SoundConfigFile)
	f.ripTarget, _ = paths.ConfigTarget(roots, "", paths.AudioDirName, paths.RipConfigFile)
	return f, nil
}

// Options opens a session: the backend forced by name ("" detects, "null"
// discards) or the host's own speakers in its place, the sfx volume, the writer
// the session reports to, and the files vif would read (ResolveFiles)
type Options struct {
	Backend   string
	Sink      io.WriteCloser
	SfxVolume float64
	Out       io.Writer
	Files     Files
}

// NewSession starts the engine on the banks and overrides the game would load, and
// seeds each document from its override so a save writes back what the game reads.
func NewSession(o Options) (*Session, error) {
	files, out := o.Files, o.Out
	cfg := audio.DefaultAudioConfig()
	cfg.Enabled = true
	cfg.SfxVolume = o.SfxVolume
	cfg.ForceBackend = o.Backend
	cfg.Sink = o.Sink
	cfg.Buffer = files.buffer
	// The same banks the game registers, from the same embedded assets.
	bank, err := parameter.BuiltinSounds()
	if err != nil {
		return nil, err
	}
	cfg.BaseSounds, cfg.BaseVoices = bank.Sound, bank.Voice
	if cfg.BasePatterns, err = parameter.BuiltinPatterns(); err != nil {
		return nil, err
	}
	if files.music != "" {
		if cfg.PatternTOML, err = os.ReadFile(files.music); err != nil {
			return nil, err
		}
	}
	if p := files.sounds; p != "" {
		cfg.SoundFS, cfg.SoundFile = os.DirFS(filepath.Dir(p)), filepath.Base(p)
	}

	eng, err := audio.NewAudioEngine(cfg)
	if err != nil {
		return nil, err
	}
	s := &Session{
		eng:    eng,
		sounds: newSoundDoc(),
		voices: newVoiceDoc(),
		pats:   newPatternDoc(),
		out:    out,
		bpm:    audio.DefaultBPM,
		root:   audio.DefaultRootNote,
		scale:  audio.ScalePhrygian,
		files:  files,
		rip:    audio.DefaultRipParams(),
		rips:   map[string]*ripped{},
		songs:  newSongDoc(),
		fit:    defaultFit,
	}
	s.compressOpt, s.refineOpt = defaultCompress, defaultRefine
	if f := files.rip; f != "" {
		if data, err := os.ReadFile(f); err != nil {
			fmt.Fprintf(out, "rip params %s: %v (built-in used)\n", f, err)
		} else if p, err := audio.LoadRipParams(data); err != nil {
			fmt.Fprintf(out, "rip params %s: %v (built-in used)\n", f, err)
		} else {
			s.rip, s.ripSrc = p, f
			fmt.Fprintf(out, "rip params %s\n", f)
		}
	}
	if f := files.model; f != "" {
		if err := s.loadModel(f); err != nil {
			fmt.Fprintf(out, "rip model %s: %v (the analysis listens)\n", f, err)
		} else {
			fmt.Fprintf(out, "rip model %s (%d weights)\n", f, s.frontEnd.Model.Parameters())
		}
	}
	// A failed Start latches silent mode but the engine stays valid, and
	// rendering is engine-independent: edit, validate and export all work
	// with no backend. Record the error and continue rather than abort.
	s.startErr = eng.Start()
	// Editor policy: deterministic slots. The engine's auto-fill swaps slot 2
	// to a random fill once per phrase — game drama, editor confusion. Forced
	// off here; :fill on restores it for auditioning the fill bank.
	s.eng.SetAutoFill(false)
	if files.skipped != nil {
		fmt.Fprintf(out, "%v: skipped, soundlab reads only [paths] root and [audio] buffer_ms\n", files.skipped)
	}
	if err := eng.SpecError(); err != nil {
		fmt.Fprintf(out, "override errors, the game falls back the same way:\n%v\n", err)
	}
	s.adoptOverrides()
	return s, nil
}

// adoptOverrides loads each override the engine registered as a clean document that
// saves to its target, which differs from the source whenever a lower root supplied
// it: the save then shadows it from the first root, as the game reads. One that
// fails to load loses its target, so an untitled save cannot overwrite it.
func (s *Session) adoptOverrides() {
	if f := s.files.sounds; f != "" {
		fmt.Fprintf(s.out, "sounds override %s, saving to %s\n", f, s.files.soundsTarget)
		if err := s.loadSoundFile(f, true); err != nil {
			fmt.Fprintf(s.out, "not adopted: %v\n", err)
			s.files.soundsTarget = ""
		} else {
			s.sounds.replaceAll(s.sounds.all(), s.files.soundsTarget, false)
			s.voices.replaceAll(s.voices.all(), s.files.soundsTarget, false)
		}
	}
	if f := s.files.music; f != "" {
		fmt.Fprintf(s.out, "music override %s, saving to %s\n", f, s.files.musicTarget)
		if err := s.loadPatternFile(f, true); err != nil {
			fmt.Fprintf(s.out, "not adopted: %v\n", err)
			s.files.musicTarget = ""
		} else {
			s.pats.replaceAll(s.pats.all(), s.files.musicTarget, false)
			s.songs.replaceAll(s.songs.all(), s.files.musicTarget, false)
		}
	}
}

func (s *Session) Close() {
	s.eng.Stop()
	if !s.eng.Stopped() {
		fmt.Fprintln(s.out, "warning: mixer did not stop; a backend write is wedged")
	}
}

// loadModel makes a weight file the front end rip hears with
func (s *Session) loadModel(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	fe, err := ripnet.Load(f)
	if err != nil {
		return err
	}
	s.frontEnd = fe
	return nil
}

// ripper is what rip listens with: the parameters, and the model when one loaded
func (s *Session) ripper() audio.Ripper {
	r := audio.Ripper{Params: s.rip}
	if s.frontEnd != nil {
		r.FrontEnd = s.frontEnd
	}
	return r
}

// StartError is why the engine runs silent, nil when a backend took the stream;
// editing, validation and export work either way.
func (s *Session) StartError() error { return s.startErr }

// SetOut redirects what the session says, as a front end does when its screen
// opens or closes.
func (s *Session) SetOut(w io.Writer) { s.out = w }

// --- documents ---

// loadSoundFile parses one document, its sounds and voices, with includes
// resolved relative to the file's directory. DirFS confines every include to that
// subtree, so a spec cannot reach ../, which is LoadSoundsFS's capability model
// kept intact.
func (s *Session) loadSoundFile(file string, replace bool) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	dir, base := filepath.Dir(abs), filepath.Base(abs)
	bank, lerr := audio.LoadSoundsFS(os.DirFS(dir), base)
	if bank == nil || (len(bank.Sound)+len(bank.Voice) == 0 && lerr != nil) {
		return lerr
	}
	// The root's include list must survive to save (MarshalSoundsFile), but
	// LoadSoundsFS flattens. Recover it with a lax decode: the decoder
	// ignores unknown keys, so this reads only what it names.
	var head struct {
		Include []string `toml:"include"`
	}
	if raw, rerr := os.ReadFile(abs); rerr == nil {
		_ = toml.Unmarshal(raw, &head)
	}
	if replace {
		s.sounds.replaceAll(bank.Sound, abs, true) // diverges from registry until apply
		s.voices.replaceAll(bank.Voice, abs, true)
		s.sounds.include = head.Include
	} else {
		s.sounds.mergeAll(bank.Sound, true)
		s.voices.mergeAll(bank.Voice, true)
	}
	if lerr != nil {
		fmt.Fprintf(s.out, "loaded with errors (invalid entries dropped):\n%v\n", lerr)
	}
	fmt.Fprintf(s.out, "%d sound(s) and %d voice(s) loaded\n", len(bank.Sound), len(bank.Voice))
	return nil
}

func (s *Session) loadPatternFile(file string, replace bool) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	// Authoring form on purpose: an invalid pattern stays editable in the
	// document and fails at validate/apply with a specific error, instead of
	// being dropped at load the way LoadPatternsTOML would.
	f, err := audio.LoadPatternFile(data)
	if err != nil {
		return err
	}
	if replace {
		abs, _ := filepath.Abs(file)
		s.pats.replaceAll(f.Pattern, abs, true)
		s.songs.replaceAll(f.Song, abs, true)
	} else {
		s.pats.mergeAll(f.Pattern, true)
		s.songs.mergeAll(f.Song, true)
	}
	fmt.Fprintf(s.out, "%d pattern(s) loaded", len(f.Pattern))
	if len(f.Song) > 0 {
		fmt.Fprintf(s.out, " and %d song(s)", len(f.Song))
	}
	fmt.Fprintln(s.out)
	return nil
}

// seedBuiltinSounds seeds the sound document, voices included, from a fresh
// parse of the shipped bank, so nothing aliases the registry.
func (s *Session) seedBuiltinSounds() error {
	bank, err := parameter.BuiltinSounds()
	if err != nil {
		return err
	}
	s.sounds.replaceAll(bank.Sound, "", false) // registry holds identical content: clean
	s.voices.replaceAll(bank.Voice, "", false)
	s.sounds.include = nil
	fmt.Fprintf(s.out, "%d built-in sound(s) and %d voice(s) seeded\n", len(bank.Sound), len(bank.Voice))
	return nil
}

func (s *Session) seedBuiltinVoices() error {
	bank, err := parameter.BuiltinSounds()
	if err != nil {
		return err
	}
	s.voices.replaceAll(bank.Voice, s.sounds.src, false)
	fmt.Fprintf(s.out, "%d built-in voice(s) seeded\n", len(bank.Voice))
	return nil
}

// soundsModified reports the sound document, voices included, diverging from disk
func (s *Session) soundsModified() bool { return s.sounds.modified || s.voices.modified }

// patsModified reports the pattern document, songs included, diverging from disk
func (s *Session) patsModified() bool { return s.pats.modified || s.songs.modified }

// seedBuiltinPatterns snapshots the registry, which Start filled from the shipped
// bank plus the code-owned melody_gen. Def() allocates, so live mixer pointers are
// untouched.
func (s *Session) seedBuiltinPatterns() error {
	var defs []*audio.PatternDef
	for _, p := range audio.RegisteredPatterns() {
		if p == nil || p.Name == "" {
			continue // anonymous: cannot be reloaded or overridden
		}
		defs = append(defs, p.Def())
	}
	s.pats.replaceAll(defs, "", false)
	fmt.Fprintf(s.out, "%d registered pattern(s) seeded\n", len(defs))
	return nil
}

// saveFile picks where a save lands: the named file, else the document's own, else
// the file the next run with this session's roots reads first. The directory of an
// override the user root never held yet is created.
func saveFile(named, src, target string) (string, error) {
	file := cmp.Or(named, src, target)
	if file == "" {
		return "", errors.New("no save target; name a file")
	}
	return file, os.MkdirAll(filepath.Dir(file), 0o755)
}

func (s *Session) saveSounds(named string) error {
	file, err := saveFile(named, s.sounds.src, s.files.soundsTarget)
	if err != nil {
		return err
	}
	// Name-shadow model: the root re-emits every sound and voice in the document.
	// Includes still load first and are overridden per name, so reloading
	// reproduces this document exactly while the include set is preserved.
	data, err := audio.MarshalSoundsFile(&audio.SoundSpecFile{Include: s.sounds.include, Sound: s.sounds.all(), Voice: s.voices.all()})
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	s.sounds.src, s.voices.src = file, file
	fmt.Fprintf(s.out, "wrote %s (%d sounds, %d voices", file, len(s.sounds.order), len(s.voices.order))
	if len(s.sounds.include) > 0 {
		fmt.Fprintf(s.out, ", %d includes preserved; root overrides by name", len(s.sounds.include))
	}
	fmt.Fprintln(s.out, ")")
	s.sounds.modified, s.voices.modified = false, false
	return nil
}

func (s *Session) savePatterns(named string) error {
	file, err := saveFile(named, s.pats.src, s.files.musicTarget)
	if err != nil {
		return err
	}
	data, err := audio.MarshalPatternFile(&audio.PatternSpecFile{Pattern: s.pats.all(), Song: s.songs.all()})
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	s.pats.src, s.songs.src = file, file
	fmt.Fprintf(s.out, "wrote %s (%d patterns", file, len(s.pats.order))
	if n := len(s.songs.order); n > 0 {
		fmt.Fprintf(s.out, ", %d songs", n)
	}
	fmt.Fprintln(s.out, ")")
	s.pats.modified, s.songs.modified = false, false
	return nil
}

// --- unified validate / apply / revert ---

// validateName checks one entry regardless of kind. Patterns validate through
// Pattern(), so instrument-name conversion errors surface with the same verb
// as range errors.
func (s *Session) validateName(name string) error {
	if d, ok := s.sounds.get(name); ok {
		return audio.ValidateSound(d)
	}
	if d, ok := s.voices.get(name); ok {
		return audio.ValidateVoice(d)
	}
	if d, ok := s.pats.get(name); ok {
		_, err := d.Pattern()
		return err
	}
	return fmt.Errorf("%q: not in any document", name)
}

func (s *Session) validateAll() error {
	fails := 0
	check := func(names []string) {
		for _, n := range names {
			if err := s.validateName(n); err != nil {
				fails++
				fmt.Fprintf(s.out, "  FAIL %-24s %v\n", n, err)
			} else {
				fmt.Fprintf(s.out, "  ok   %s\n", n)
			}
		}
	}
	check(s.sounds.order)
	check(s.voices.order)
	check(s.pats.order)
	if fails > 0 {
		return fmt.Errorf("%d invalid", fails)
	}
	return nil
}

func (s *Session) applyName(name string) error {
	if d, ok := s.sounds.get(name); ok {
		// Clone severs registry-document aliasing: DefineSound stores the
		// pointer it is given, and a later doc edit must not mutate the
		// registered spec behind the render cache.
		id, err := s.eng.DefineSound(d.Clone())
		if err != nil {
			return err
		}
		s.sounds.clearDirty(name)
		fmt.Fprintf(s.out, "applied sound %q (id %d)\n", name, id)
		return nil
	}
	if d, ok := s.voices.get(name); ok {
		id, err := s.eng.DefineVoice(d.Clone()) // the mixer reads what it is given
		if err != nil {
			return err
		}
		s.voices.clearDirty(name)
		fmt.Fprintf(s.out, "applied voice %q (instrument %d)\n", name, id)
		return nil
	}
	if d, ok := s.pats.get(name); ok {
		p, err := d.Pattern() // fresh render form each apply: no mixer aliasing
		if err != nil {
			return err
		}
		id, err := s.eng.DefinePattern(p)
		if err != nil {
			return err
		}
		s.pats.clearDirty(name)
		fmt.Fprintf(s.out, "applied pattern %q (id %d)\n", name, id)
		return nil
	}
	return fmt.Errorf("%q: not in any document", name)
}

// applyAll flushes dirty entries only; re-rendering clean sounds would burn
// full variant-set renders for nothing.
func (s *Session) applyAll() error {
	names := append(append(s.sounds.dirtyNames(), s.voices.dirtyNames()...), s.pats.dirtyNames()...)
	fails := 0
	for _, n := range names {
		if err := s.applyName(n); err != nil {
			fails++
			fmt.Fprintf(s.out, "  FAIL %-24s %v\n", n, err)
		}
	}
	fmt.Fprintf(s.out, "applied %d of %d dirty\n", len(names)-fails, len(names))
	if fails > 0 {
		return fmt.Errorf("%d failed", fails)
	}
	return nil
}

// sameCanonical reports whether two defs serialize to identical canonical TOML,
// the honest equality for revert: what matters is whether save would emit the same
// document (cmdShow leans on the same property). reflect.DeepEqual is the wrong
// predicate; it distinguishes representations that serialize, validate and render
// identically.
func sameCanonical[T any](marshal func([]T) ([]byte, error), a, b T) bool {
	x, ex := marshal([]T{a})
	y, ey := marshal([]T{b})
	return ex == nil && ey == nil && bytes.Equal(x, y)
}

// revertName restores one document entry from the live registry. That is the
// whole contract — registry-level, never disk-level; load restores a file.
// Corollary: revert after apply is the identity, because apply moved the
// edits into the registry. Report that case instead of claiming a restore.
func (s *Session) revertName(name string) error {
	if cur, ok := s.sounds.get(name); ok {
		reg := audio.SoundDefByName(name)
		if reg == nil {
			return fmt.Errorf("%q: never registered; nothing to revert to (del to drop)", name)
		}
		noop := sameCanonical(audio.MarshalSounds, cur, reg)
		s.sounds.put(reg.Clone(), false) // normalizes ownership, clears a stale '*'
		if noop {
			fmt.Fprintf(s.out, "%q matches registry — nothing to revert (apply committed these edits; load restores disk state)\n", name)
		} else {
			fmt.Fprintf(s.out, "reverted sound %q to registry state\n", name)
		}
		return nil
	}
	if cur, ok := s.voices.get(name); ok {
		reg := audio.VoiceDefByName(name)
		if reg == nil {
			return fmt.Errorf("%q: never registered; nothing to revert to (del to drop)", name)
		}
		noop := sameCanonical(audio.MarshalVoices, cur, reg)
		s.voices.put(reg.Clone(), false)
		if noop {
			fmt.Fprintf(s.out, "%q matches registry — nothing to revert (apply committed these edits; load restores disk state)\n", name)
		} else {
			fmt.Fprintf(s.out, "reverted voice %q to registry state\n", name)
		}
		return nil
	}
	if cur, ok := s.pats.get(name); ok {
		id := audio.PatternIDByName(name)
		p := audio.GetPattern(id)
		if id == audio.PatternSilence || p == nil {
			return fmt.Errorf("%q: never registered; nothing to revert to (del to drop)", name)
		}
		def := p.Def() // Def allocates; the live mixer pointer stays untouched
		noop := sameCanonical(audio.MarshalPatternDefs, cur, def)
		s.pats.put(def, false)
		if noop {
			fmt.Fprintf(s.out, "%q matches registry — nothing to revert (apply committed these edits; load restores disk state)\n", name)
		} else {
			fmt.Fprintf(s.out, "reverted pattern %q to registry state\n", name)
		}
		return nil
	}
	return fmt.Errorf("%q: not in any document", name)
}

func (s *Session) revertAll() error {
	names := append(append(s.sounds.dirtyNames(), s.voices.dirtyNames()...), s.pats.dirtyNames()...)
	for _, n := range names {
		if err := s.revertName(n); err != nil {
			fmt.Fprintf(s.out, "  skip %-24s %v\n", n, err)
		}
	}
	return nil
}

// --- resolution helpers ---

// resolveSoundDef prefers the document, then the live registry entry. The
// registry pointer is only ever read here (RenderPreview/RenderVariants are
// pure reads) and this session is the sole DefineSound caller, so no clone.
func (s *Session) resolveSoundDef(name string) (*audio.SoundDef, bool) {
	if d, ok := s.sounds.get(name); ok {
		return d, true
	}
	if d := audio.SoundDefByName(name); d != nil {
		return d, true
	}
	return nil, false
}

// docRoot maps a path head to its owning document entry. Sounds shadow voices
// and patterns on a name collision; the collision is user-inflicted and mv
// fixes it.
func (s *Session) docRoot(name string) (root any, mark func(), err error) {
	if d, ok := s.sounds.get(name); ok {
		return d, func() { s.sounds.markDirty(name) }, nil
	}
	if d, ok := s.voices.get(name); ok {
		return d, func() { s.voices.markDirty(name) }, nil
	}
	if d, ok := s.pats.get(name); ok {
		return d, func() { s.pats.markDirty(name) }, nil
	}
	if d, ok := s.songs.get(name); ok {
		return d, func() { s.songs.markDirty(name) }, nil
	}
	return nil, nil, fmt.Errorf("%q: not in document (new / load / builtin first)", name)
}

func patName(id audio.PatternID) string {
	if id == audio.PatternSilence {
		return "-"
	}
	for _, p := range audio.RegisteredPatterns() {
		if p.ID == id {
			if p.Name != "" {
				return p.Name
			}
			return fmt.Sprintf("anon#%d", id)
		}
	}
	return id.String()
}

func soundDur(d *audio.SoundDef) float64 {
	if d.Duration > 0 {
		return d.Duration
	}
	var m float64
	for i := range d.Layer {
		if e := d.Layer[i].Offset + d.Layer[i].Length; e > m {
			m = e
		}
	}
	return m
}

// pitch is the MIDI note a degree and octave above the root sound in the mirrored
// key, as the sequencer resolves a track that does not follow the chord
func (s *Session) pitch(deg, oct int) int {
	sc := audio.ScaleIntervals(s.scale)
	n := len(sc)
	oct += deg / n
	if deg %= n; deg < 0 {
		deg += n
		oct--
	}
	return min(max(s.root+12*oct+sc[deg], 0), 127)
}

// degreeOf is the degree and octave nearest midi in the mirrored key, the lower
// on a tie, and whether midi is exactly that degree
func (s *Session) degreeOf(midi int) (deg, oct int, exact bool) {
	deg, oct, acc := audio.PitchStep(s.root, s.scale, midi)
	return deg, oct, acc == 0
}

// keyName is the mirrored key as the header shows it, "E2 phrygian"
func (s *Session) keyName() string { return noteName(s.root) + " " + scaleName(s.scale) }

var pitchNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// noteName is a MIDI note in scientific pitch, middle C (60) being C4
func noteName(midi int) string {
	midi = min(max(midi, 0), 127)
	return pitchNames[midi%12] + strconv.Itoa(midi/12-1)
}
