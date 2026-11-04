//go:build !vif_headless && !vif_noaudio && !wasm

package stapes

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/toml"
)

// errVoiceFile is the answer to a file verb on voices, which live in the sound document
var errVoiceFile = fmt.Errorf("voices are part of the sound document: name sound")

func cmdLoad(s *Session, a []string) error  { return s.loadDoc(a, true) }
func cmdMerge(s *Session, a []string) error { return s.loadDoc(a, false) }

// loadDoc reads load and merge's words, [sound|pattern] <file> [name]: a document
// of the kind, or of what its tables or extension say it is, and a name only for
// a MIDI file's patterns and song
func (s *Session) loadDoc(a []string, replace bool) error {
	k, kerr := kindArg(a[0])
	if kerr == nil {
		a = a[1:]
	}
	switch {
	case len(a) == 0:
		return errors.New("name a file")
	case isMIDI(a[0]) && kerr == nil && k != kindPattern:
		return fmt.Errorf("%s: a score loads as patterns and a song", a[0])
	case isMIDI(a[0]) && len(a) > 2:
		return fmt.Errorf("%s: one name, for its patterns and its song", a[0])
	case isMIDI(a[0]):
		return s.loadMIDI(a[0], a[1:], replace)
	case len(a) > 1 && kerr != nil:
		return kerr
	case len(a) > 1:
		return fmt.Errorf("%s: a name is for a MIDI file's patterns and song", a[1])
	case kerr != nil:
		return s.loadAny(a[0], replace)
	case k == kindSound:
		return s.loadSoundFile(s.path(a[0]), replace)
	case k == kindPattern:
		return s.loadPatternFile(s.path(a[0]), replace)
	}
	return errVoiceFile
}

// loadMIDI reads a MIDI file as patterns, a bar of a part each as compress makes
// them, and the song under name, the file's own made one when none is given, that
// plays them: in place of the pattern document, or into it, an earlier load's
// patterns of the name gone. The transport takes the song's tempo and key.
func (s *Session) loadMIDI(file string, named []string, replace bool) error {
	name := wordOf(file)
	if len(named) > 0 {
		name = named[0]
	}
	if err := badName(name); err != nil {
		return err
	}
	data, err := os.ReadFile(s.path(file))
	if err != nil {
		return err
	}
	m, err := audio.ReadMIDI(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	pats, song, said := m.Song(name)
	if len(pats) == 0 {
		return fmt.Errorf("%s: no notes to load", file)
	}
	byName := map[string]*audio.PatternDef{}
	for _, p := range pats {
		byName[p.Name] = p
		if s.sounds.has(p.Name) || s.voices.has(p.Name) || !replace && s.songs.has(p.Name) {
			return fmt.Errorf("%q: another entry has the name; load <file.mid> <name> names them otherwise", p.Name)
		}
	}
	if s.sounds.has(name) || s.voices.has(name) || !replace && s.pats.has(name) {
		return fmt.Errorf("%q: another entry has the name; load <file.mid> <name> names the song otherwise", name)
	}
	if replace {
		s.pats.replaceAll(pats, "", true) // no TOML behind it: save asks where
		s.songs.replaceAll([]*audio.SongDef{song}, "", true)
	} else {
		fresh := map[string]bool{}
		for _, p := range pats {
			fresh[p.Name] = true
		}
		for _, n := range slices.Clone(s.pats.order) {
			if dictionaryName(name, n) && !fresh[n] {
				s.pats.del(n)
			}
		}
		s.pats.mergeAll(pats, true)
		s.songs.put(song, true)
	}
	s.bpm, s.root, s.scale, s.tuning = song.BPM, song.Root, song.ScaleID(), 0
	s.eng.SetMusicBPM(s.bpm)
	s.eng.SetHarmony(s.root, s.scale, s.prog)
	s.eng.SetTuning(0)
	notes, hits := 0, 0
	for _, bar := range song.Bars {
		for _, n := range bar {
			if p := byName[n]; p != nil {
				for _, tr := range p.Track {
					if i, _ := audio.InstrumentByName(tr.Instr); i.IsDrum() {
						hits += len(tr.Event)
					} else {
						notes += len(tr.Event)
					}
				}
			}
		}
	}
	fmt.Fprintf(s.out, "read %s: %d note(s) and %d hit(s) in %d pattern(s); song %s plays them in %d bar(s) at %d bpm, %s; tempo and key set\n",
		file, notes, hits, len(pats), name, len(song.Bars), song.BPM, s.keyName())
	for _, l := range said {
		fmt.Fprintln(s.out, "  "+l)
	}
	return nil
}

// wordOf is a file's name as a name the command grammar addresses: lowercase
// letters, digits and underscores, rip when nothing of it is left
func wordOf(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '_'
	}, base)
	return cmp.Or(strings.Trim(name, "_"), "rip")
}

func cmdBuiltin(s *Session, a []string) error {
	switch k, err := kindArg(a[0]); {
	case err != nil:
		return err
	case k == kindSound:
		return s.seedBuiltinSounds()
	case k == kindVoice:
		return s.seedBuiltinVoices()
	}
	return s.seedBuiltinPatterns()
}

func cmdSave(s *Session, a []string) error {
	k, err := kindArg(a[0])
	if err != nil {
		return err
	}
	file := ""
	if len(a) > 1 {
		file = s.path(a[1])
	}
	if isMIDI(file) {
		return fmt.Errorf("%s: a document saves as TOML; write <pattern|song> <file.mid> writes one as MIDI", a[1])
	}
	switch k {
	case kindSound:
		return s.saveSounds(file)
	case kindPattern:
		return s.savePatterns(file)
	}
	return errVoiceFile
}

func cmdExport(s *Session, a []string) error {
	if isMIDI(a[1]) {
		return fmt.Errorf("%s: export renders a sound to a WAV; write <pattern|song> <file.mid> writes notes", a[1])
	}
	d, ok := s.resolveSoundDef(a[0])
	if !ok {
		return fmt.Errorf("%q: no such sound in document or registry", a[0])
	}
	var buf []float64
	if len(a) > 2 {
		// RenderVariants assumes a validated spec (renderSound is total);
		// the document copy may not be, so gate it here. RenderPreview
		// validates internally.
		if err := audio.ValidateSound(d); err != nil {
			return err
		}
		vi, err := strconv.Atoi(a[2])
		if err != nil {
			return fmt.Errorf("variant %q: not an index", a[2])
		}
		sets := audio.RenderVariants(d, audio.SoundShape{})
		if vi < 0 || vi >= len(sets) {
			return fmt.Errorf("variant %d out of range, have %d", vi, len(sets))
		}
		buf = sets[vi]
	} else {
		var err error
		if buf, err = audio.RenderPreview(d, audio.SoundShape{}); err != nil {
			return err
		}
	}
	f, err := os.Create(s.path(a[1]))
	if err != nil {
		return err
	}
	werr := audio.WriteWAV(f, buf)
	cerr := f.Close() // matters for a file just written; defer would swallow it
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	fmt.Fprintf(s.out, "wrote %s: %d samples (%.2fs)\n",
		a[1], len(buf), float64(len(buf))/audio.AudioSampleRate)
	return nil
}

// cmdRip reads a recording into the pattern document as a beat, a melody and a
// bass pattern a window of bars, dirty, and sets the transport's tempo and key to
// what it heard, fitting a voice to a register the front end heard none for. A
// MIDI file is a rip already, its lines on the shipped voices its programs play
// on. The rip is kept under its name for fit, compress and song.
func cmdRip(s *Session, a []string) error {
	name := a[1]
	if err := badName(name); err != nil {
		return err
	}
	nodrums, tempo := false, 0
	for {
		n := len(a)
		switch {
		case n > 2 && a[n-1] == "nodrums":
			nodrums, a = true, a[:n-1]
			continue
		case n > 3 && (a[n-2] == "lead" || a[n-2] == "bpm") && isMIDI(a[0]):
			return fmt.Errorf("%s: a MIDI file's lines and tempo are its own; %s is for a recording", a[0], a[n-2])
		case n > 3 && a[n-2] == "lead":
			rule, ok := audio.ParseLeadRule(a[n-1])
			if !ok {
				return fmt.Errorf("lead %q: want busy, high, low, loud, soft, long or short", a[n-1])
			}
			s.lead, a = rule, a[:n-2]
			continue
		case n > 3 && a[n-2] == "bpm":
			v, err := strconv.Atoi(a[n-1])
			if err != nil || v < audio.MinBPM || v > audio.MaxBPM {
				return fmt.Errorf("bpm %q: want %d to %d", a[n-1], audio.MinBPM, audio.MaxBPM)
			}
			tempo, a = v, a[:n-2]
			continue
		}
		break
	}
	bars := audio.MaxPatternLen / audio.StepsPerBar
	if len(a) > 2 {
		n, err := strconv.Atoi(a[2])
		if err != nil || n < 1 || n > bars {
			return fmt.Errorf("bars %q: want 1 to %d", a[2], bars)
		}
		bars = n
	}
	var rp *ripped
	var err error
	if isMIDI(a[0]) {
		rp, err = s.readScore(a[0])
	} else {
		rp, err = s.hear(a[0], tempo)
	}
	if err != nil {
		return err
	}
	r := rp.rip
	if nodrums {
		for b := range r.Bars {
			r.Bars[b].Drums = [audio.InstrClap][]audio.Step{}
		}
	}
	if len(r.Patterns(name, bars)) == 0 {
		return fmt.Errorf("%s: nothing heard to write", a[0])
	}
	// The names first, so a taken one costs no fit
	for _, d := range r.Patterns(name, bars) {
		if s.has(d.Name) {
			return fmt.Errorf("%q: already exists", d.Name)
		}
	}
	for _, vn := range []string{name + "_melody_voice", name + "_under_voice", name + "_bass_voice"} {
		if s.has(vn) {
			return fmt.Errorf("%q: already exists", vn)
		}
	}
	if rp.x != nil {
		s.fitVoices(rp, s.fit, false)
	}
	defs, voices := r.Patterns(name, bars), r.Voices(name)
	// The voices register at once, so the patterns naming them play; the patterns
	// stay dirty until a slot or an apply takes them
	for _, v := range voices {
		if _, err := s.eng.DefineVoice(v.Clone()); err != nil {
			return fmt.Errorf("voice %q: %w", v.Name, err)
		}
		s.voices.put(v, false)
	}
	for _, d := range defs {
		s.pats.put(d, true)
	}
	s.rips[name] = rp
	s.bpm = r.BPM
	s.eng.SetMusicBPM(r.BPM)
	s.eng.SetHarmony(r.Root, r.Scale, r.Progression)
	s.root, s.scale, s.prog = r.Root, r.Scale, r.Progression
	s.tuning = r.Tuning // the transport plays at the recording's pitch, as the song will
	s.eng.SetTuning(r.Tuning)
	var hits [audio.InstrClap]int
	notes, under, low := 0, 0, 0
	for _, b := range r.Bars {
		for d := range b.Drums {
			hits[d] += len(b.Drums[d])
		}
		notes, under, low = notes+len(b.Melody), under+len(b.Under), low+len(b.Bass)
	}
	from := fmt.Sprintf("from %.2fs", r.Downbeat)
	if r.Downbeat < 0 {
		from = fmt.Sprintf("the first a pickup bar from %.2fs before the recording", -r.Downbeat)
	}
	tuned := ""
	if r.Tuning <= -5 || r.Tuning >= 5 {
		tuned = fmt.Sprintf(" tuned %+.0f cents", r.Tuning)
	}
	fmt.Fprintf(s.out, "ripped %s: %d bpm, %s%s, %d bar(s) %s; kicks %d snares %d hats %d, melody notes %d and %d under, bass notes %d\n",
		a[0], r.BPM, s.keyName(), tuned, len(r.Bars), from, hits[audio.InstrKick], hits[audio.InstrSnare], hits[audio.InstrHihat], notes, under, low)
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	fmt.Fprintf(s.out, "%d pattern(s) added: %s; tempo and key set, slot them to hear the estimate\n", len(defs), strings.Join(names, " "))
	for _, v := range voices {
		fmt.Fprintf(s.out, "voice %s: %s, registered\n", v.Name, cmp.Or(v.Wave, "sine"))
	}
	if rp.x == nil {
		fmt.Fprintf(s.out, "compress %s matches its bars to a dictionary and makes the song %s\n", name, name)
		return nil
	}
	fmt.Fprintf(s.out, "compress %s matches its bars to a dictionary and makes the song %s; fit %s refines the voices\n", name, name, name)
	return nil
}

// hear rips a recording, at tempo when one is given
func (s *Session) hear(file string, tempo int) (*ripped, error) {
	f, err := os.Open(s.path(file))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	x, rate, err := audio.ReadWAV(f)
	if err != nil {
		return nil, err
	}
	ripper := s.ripper()
	if tempo > 0 { // the tempo the listener knows; the bar line is still decoded
		ripper.Grid = &audio.RipGrid{BPM: tempo, Downbeat: -1}
	}
	r, err := ripper.Samples(x, rate)
	if err != nil {
		return nil, err
	}
	return &ripped{rip: r, x: x, rate: rate, file: file}, nil
}

// readScore reads a MIDI file as a rip with no recording behind it, each line
// played by a copy of the shipped voice its program plays on, and says what of
// the file the rip could not keep: a meter, tempo changes, notes out of range, parts
func (s *Session) readScore(file string) (*ripped, error) {
	data, err := os.ReadFile(s.path(file))
	if err != nil {
		return nil, err
	}
	m, err := audio.ReadMIDI(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	r, dropped := m.Rip()
	var played []string
	for reg, at := range lineVoices(r) {
		if v := audio.VoiceDefByName(m.Voices[reg].String()); v != nil && len(lineNotes(r, reg)) > 0 {
			*at = v.Clone()
			played = append(played, ripLines[reg].word+" on the "+v.Name)
		}
	}
	said := []string{fmt.Sprintf("%d note(s) and %d hit(s) in %d/%d", len(m.Notes), len(m.Hits), m.Meter[0], m.Meter[1])}
	if len(played) > 0 {
		said[0] += ", " + strings.Join(played, ", ")
	}
	if m.Meter != [2]int{4, 4} {
		said[0] += ", read in bars of four beats from its first bar line"
	}
	if beat := math.Round(math.Log2(m.BPM / float64(r.BPM))); beat != 0 {
		said = append(said, fmt.Sprintf("its %.0f bpm read at %d, a beat %s", m.BPM, r.BPM, [...]string{"its sixteenth", "its eighth", "its quarter", "its half note", "its whole note"}[min(max(int(beat)+2, 0), 4)]))
	}
	if m.Changes > 0 {
		said = append(said, fmt.Sprintf("%d tempo change(s) after the start, played at the first tempo throughout", m.Changes))
	}
	if dropped > 0 {
		said = append(said, fmt.Sprintf("%d note(s) outside %s to %s dropped", dropped, noteName(audio.RipMinMIDI), noteName(audio.RipMaxMIDI-1)))
	}
	if m.Left > 0 {
		said = append(said, fmt.Sprintf("%d part(s) past the melody, under and bass left out", m.Left))
	}
	fmt.Fprintf(s.out, "read %s as a score: %s\n", file, strings.Join(said, "; "))
	return &ripped{rip: r, file: file}, nil
}

// cmdRender writes what the slots hold, played for bars bars at the transport's
// tempo and key with the engine's offline render, to a WAV: the estimate a rip
// made, or any arrangement, heard without a device.
func cmdRender(s *Session, a []string) error {
	bars := 4
	if len(a) > 1 {
		n, err := strconv.Atoi(a[1])
		if err != nil || n < 1 || n > 256 {
			return fmt.Errorf("bars %q: want 1 to 256", a[1])
		}
		bars = n
	}
	if isMIDI(a[0]) {
		return fmt.Errorf("%s: render writes a WAV; write <pattern|song> <file.mid> writes MIDI", a[0])
	}
	if s.slots == [audio.MusicSlots]audio.PatternID{} {
		return fmt.Errorf("every slot is silent; slot a pattern first")
	}
	buf, err := s.eng.RenderMusic(s.slots, s.bpm, bars, s.root, s.scale, s.prog, 1)
	if err != nil {
		return err
	}
	f, err := os.Create(s.path(a[0]))
	if err != nil {
		return err
	}
	werr := audio.WriteWAV(f, buf)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	fmt.Fprintf(s.out, "wrote %s: %d bar(s) at %d bpm, %s (%.1fs)\n", a[0], bars, s.bpm, s.keyName(), float64(len(buf))/audio.AudioSampleRate)
	return nil
}

// cmdWrite exports one document entry as a standalone TOML document, or a
// pattern or song as a MIDI file by a .mid name: an export, not a save, so dirty
// and modified stay as they are (save owns the baseline, see doc[T]). It reads the
// document alone, as show does, a sound shadowing a pattern of its name as in docRoot.
func cmdWrite(s *Session, a []string) error {
	name, file := a[0], s.path(a[1])
	if isMIDI(file) {
		return s.writeMIDI(name, file)
	}
	if d, ok := s.sounds.get(name); ok {
		data, err := audio.MarshalSounds([]*audio.SoundDef{d})
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "wrote %s (sound %q)\n", file, name)
		return nil
	}
	if d, ok := s.voices.get(name); ok {
		data, err := audio.MarshalVoices([]*audio.VoiceDef{d})
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "wrote %s (voice %q)\n", file, name)
		return nil
	}
	if d, ok := s.pats.get(name); ok {
		data, err := audio.MarshalPatternDefs([]*audio.PatternDef{d})
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "wrote %s (pattern %q)\n", file, name)
		return nil
	}
	if d, ok := s.songs.get(name); ok {
		data, err := audio.MarshalSongs([]*audio.SongDef{d})
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "wrote %s (song %q, its patterns not with it)\n", file, name)
		return nil
	}
	return fmt.Errorf("%q: not in any document", name)
}

// writeMIDI writes a pattern or a song of the document as a Standard MIDI File: a
// pattern once through at the transport's tempo, key and chords, a song at its
// own, the transport's chords where it has none, as play hears it; a song's bars
// play the document's patterns, else the registry's. Prob and humanize are not
// written: every event sounds, on its step.
func (s *Session) writeMIDI(name, file string) error {
	if d, ok := s.songs.get(name); ok && !s.pats.has(name) {
		return s.writeSongMIDI(d, file)
	}
	p, ok := s.pats.get(name)
	switch {
	case s.sounds.has(name) || s.voices.has(name):
		return fmt.Errorf("%q: a sound or voice holds no notes; export <sound> <file.wav> renders a sound", name)
	case !ok:
		return fmt.Errorf("%q: no pattern or song in the document", name)
	}
	var b bytes.Buffer
	if err := audio.WritePatternMIDI(&b, p, s.bpm, s.root, s.scale, s.prog); err != nil {
		return err
	}
	if err := os.WriteFile(file, b.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "wrote %s (pattern %q, %d step(s) at %d bpm, %s)\n", file, name, p.Steps, s.bpm, s.keyName())
	return nil
}

// writeSongMIDI writes a song as writeMIDI does, for write and song alike
func (s *Session) writeSongMIDI(d *audio.SongDef, file string) error {
	if len(d.Chords) == 0 {
		c := *d
		c.Chords, d = s.prog, &c
	}
	var b bytes.Buffer
	if err := audio.WriteSongMIDI(&b, d, s.patternDef); err != nil {
		return err
	}
	if err := os.WriteFile(file, b.Bytes(), 0o644); err != nil {
		return err
	}
	tuned := ""
	if d.Tuning != 0 {
		tuned = fmt.Sprintf("; its %+.0f cents not written, MIDI is at A440", d.Tuning)
	}
	fmt.Fprintf(s.out, "wrote %s (song %q, %d bar(s) at %d bpm, %s %s%s)\n", file, d.Name, len(d.Bars), d.BPM, noteName(d.Root), d.Scale, tuned)
	return nil
}

// patternDef is a pattern as a song plays it: the document's, else the registry's
func (s *Session) patternDef(n string) *audio.PatternDef {
	if d, ok := s.pats.get(n); ok {
		return d
	}
	if id := audio.PatternIDByName(n); id != audio.PatternSilence {
		return audio.GetPattern(id).Def()
	}
	return nil
}

// loadAny loads a file by what it is: a .slab runs as a script, a .wav is for
// rip, and a .toml document goes to the sounds or the patterns by its tables,
// replacing the document or merged into it.
func (s *Session) loadAny(file string, replace bool) error {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".slab":
		return RunScriptFile(s, s.path(file))
	case ".wav":
		return fmt.Errorf("%s: a recording; rip <file.wav> <name> hears it", file)
	}
	kind, err := sniffDocument(s.path(file))
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if kind == "sound" {
		return s.loadSoundFile(s.path(file), replace)
	}
	return s.loadPatternFile(s.path(file), replace)
}

// sniffDocument says which document a TOML file is by its tables: patterns and
// songs make a pattern document, sounds, voices and includes a sound document
func sniffDocument(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var head struct {
		Pattern []map[string]any `toml:"pattern"`
		Song    []map[string]any `toml:"song"`
		Sound   []map[string]any `toml:"sound"`
		Voice   []map[string]any `toml:"voice"`
		Include []string         `toml:"include"`
	}
	if err := toml.Unmarshal(data, &head); err != nil {
		return "", err
	}
	sounds, pats := len(head.Sound)+len(head.Voice)+len(head.Include), len(head.Pattern)+len(head.Song)
	switch {
	case sounds > 0 && pats > 0:
		return "", errors.New("holds sounds and patterns both")
	case sounds > 0:
		return "sound", nil
	case pats > 0:
		return "pattern", nil
	}
	return "", errors.New("no [[pattern]], [[song]], [[sound]] or [[voice]] table")
}
