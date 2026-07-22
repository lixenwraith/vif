package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/audio"
)

// Session owns the engine and the two working documents. Single-goroutine by
// construction — every mutation funnels through Execute — which is what lets
// document and registry pointers flow into RenderPreview without locks.
type Session struct {
	eng    *audio.AudioEngine
	sounds *doc[*audio.SoundDef]
	pats   *doc[*audio.PatternDef]
	out    io.Writer

	// bpm mirrors the last requested tempo so `note` can size durations in
	// steps. The sequencer clamps and bar-quantizes its own copy; one pending
	// change of skew does not matter for an audition length.
	bpm      int
	startErr error
}

func NewSession(backend string, masterVol float64, out io.Writer) (*Session, error) {
	cfg := audio.DefaultAudioConfig()
	cfg.Enabled = true
	cfg.MasterVolume = masterVol
	cfg.ForceBackend = backend

	eng, err := audio.NewAudioEngine(cfg)
	if err != nil {
		return nil, err
	}
	s := &Session{
		eng:    eng,
		sounds: newSoundDoc(),
		pats:   newPatternDoc(),
		out:    out,
		bpm:    audio.DefaultBPM,
	}
	// A failed Start latches silent mode but the engine stays valid, and
	// rendering is engine-independent: edit, validate and export all work
	// with no backend. Record the error and continue rather than abort.
	s.startErr = eng.Start()
	return s, nil
}

func (s *Session) Close() {
	s.eng.Stop()
	if !s.eng.Stopped() {
		fmt.Fprintln(s.out, "warning: mixer did not stop; a backend write is wedged")
	}
}

// --- documents ---

// loadSoundFile parses one document with includes resolved relative to the
// file's directory. DirFS confines every include to that subtree — a spec
// cannot reach ../ — which is LoadSoundsFS's capability model kept intact.
func (s *Session) loadSoundFile(file string, replace bool) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	dir, base := filepath.Dir(abs), filepath.Base(abs)
	defs, lerr := audio.LoadSoundsFS(os.DirFS(dir), base)
	if len(defs) == 0 && lerr != nil {
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
		s.sounds.replaceAll(defs, abs, true) // diverges from registry until apply
		s.sounds.include = head.Include
	} else {
		s.sounds.mergeAll(defs, true)
	}
	if lerr != nil {
		fmt.Fprintf(s.out, "loaded with errors (invalid entries dropped):\n%v\n", lerr)
	}
	fmt.Fprintf(s.out, "%d sound(s) loaded\n", len(defs))
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
	defs, err := audio.LoadPatternDefs(data)
	if err != nil {
		return err
	}
	if replace {
		abs, _ := filepath.Abs(file)
		s.pats.replaceAll(defs, abs, true)
	} else {
		s.pats.mergeAll(defs, true)
	}
	fmt.Fprintf(s.out, "%d pattern(s) loaded\n", len(defs))
	return nil
}

func (s *Session) seedBuiltinSounds() error {
	defs, err := audio.BuiltinSounds() // fresh parse of embedded TOML; no registry aliasing
	if err != nil {
		return err
	}
	s.sounds.replaceAll(defs, "", false) // registry holds identical content: clean
	s.sounds.include = nil
	fmt.Fprintf(s.out, "%d built-in sound(s) seeded\n", len(defs))
	return nil
}

// seedBuiltinPatterns snapshots the registry rather than calling
// BuiltinPatternDefs: that helper re-runs InitDefaultPatterns and carries a
// no-mixer precondition, while the engine already registered everything at
// Start. Def() allocates, so live mixer pointers are untouched.
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

func (s *Session) saveSounds(file string) error {
	if file == "" {
		file = s.sounds.src
	}
	if file == "" {
		return fmt.Errorf("no provenance path; use: save sound <file>")
	}
	// Name-shadow model: the root re-emits every sound in the document.
	// Includes still load first and are overridden per name, so reloading
	// reproduces this document exactly while the include set is preserved.
	data, err := audio.MarshalSoundsFile(s.sounds.include, s.sounds.all())
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	s.sounds.src = file
	fmt.Fprintf(s.out, "wrote %s (%d sounds", file, len(s.sounds.order))
	if len(s.sounds.include) > 0 {
		fmt.Fprintf(s.out, ", %d includes preserved; root overrides by name", len(s.sounds.include))
	}
	fmt.Fprintln(s.out, ")")
	return nil
}

func (s *Session) savePatterns(file string) error {
	if file == "" {
		file = s.pats.src
	}
	if file == "" {
		return fmt.Errorf("no provenance path; use: save pattern <file>")
	}
	data, err := audio.MarshalPatternDefs(s.pats.all())
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	s.pats.src = file
	fmt.Fprintf(s.out, "wrote %s (%d patterns)\n", file, len(s.pats.order))
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
	names := append(s.sounds.dirtyNames(), s.pats.dirtyNames()...)
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

func (s *Session) revertName(name string) error {
	if s.sounds.has(name) {
		reg := audio.SoundDefByName(name)
		if reg == nil {
			return fmt.Errorf("%q: never registered; nothing to revert to (del to drop)", name)
		}
		s.sounds.put(reg.Clone(), false)
		fmt.Fprintf(s.out, "reverted sound %q to registry state\n", name)
		return nil
	}
	if s.pats.has(name) {
		id := audio.PatternIDByName(name)
		p := audio.GetPattern(id)
		if id == audio.PatternSilence || p == nil {
			return fmt.Errorf("%q: never registered; nothing to revert to (del to drop)", name)
		}
		s.pats.put(p.Def(), false) // Def allocates; the live pointer stays untouched
		fmt.Fprintf(s.out, "reverted pattern %q to registry state\n", name)
		return nil
	}
	return fmt.Errorf("%q: not in any document", name)
}

func (s *Session) revertAll() error {
	names := append(s.sounds.dirtyNames(), s.pats.dirtyNames()...)
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

// docRoot maps a path head to its owning document entry. Sounds shadow
// patterns on a name collision; the collision is user-inflicted and mv fixes
// it.
func (s *Session) docRoot(name string) (root any, mark func(), err error) {
	if d, ok := s.sounds.get(name); ok {
		return d, func() { s.sounds.markDirty(name) }, nil
	}
	if d, ok := s.pats.get(name); ok {
		return d, func() { s.pats.markDirty(name) }, nil
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
