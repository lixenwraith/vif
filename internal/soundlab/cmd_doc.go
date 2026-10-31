//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"cmp"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lixenwraith/vif/pkg/audio"
)

// errVoiceFile is the answer to a file verb on voices, which live in the sound document
var errVoiceFile = fmt.Errorf("voices are part of the sound document: name sound")

func cmdLoad(s *Session, a []string) error {
	switch k, err := kindArg(a[0]); {
	case err != nil:
		return err
	case k == kindSound:
		return s.loadSoundFile(a[1], true)
	case k == kindPattern:
		return s.loadPatternFile(a[1], true)
	}
	return errVoiceFile
}

func cmdMerge(s *Session, a []string) error {
	switch k, err := kindArg(a[0]); {
	case err != nil:
		return err
	case k == kindSound:
		return s.loadSoundFile(a[1], false)
	case k == kindPattern:
		return s.loadPatternFile(a[1], false)
	}
	return errVoiceFile
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
		file = a[1]
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
	f, err := os.Create(a[1])
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

// cmdRip reads a recording into the pattern document: a beat, a melody and a
// bass pattern per window of bars, dirty, under the names the rip gives them, and
// sets the transport's tempo and key to what it heard, so a slot plays the
// estimate at once. Notes off the key sit on its nearest degree.
func cmdRip(s *Session, a []string) error {
	if err := badName(a[1]); err != nil {
		return err
	}
	bars := audio.MaxPatternLen / audio.StepsPerBar
	if len(a) > 2 {
		n, err := strconv.Atoi(a[2])
		if err != nil || n < 1 || n > bars {
			return fmt.Errorf("bars %q: want 1 to %d", a[2], bars)
		}
		bars = n
	}
	f, err := os.Open(a[0])
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := s.ripper().WAV(f)
	if err != nil {
		return err
	}
	defs := r.Patterns(a[1], bars)
	if len(defs) == 0 {
		return fmt.Errorf("%s: nothing heard to write", a[0])
	}
	voices := r.Voices(a[1])
	for _, d := range defs {
		if s.has(d.Name) {
			return fmt.Errorf("%q: already exists", d.Name)
		}
	}
	for _, v := range voices {
		if s.has(v.Name) {
			return fmt.Errorf("%q: already exists", v.Name)
		}
	}
	// The voices heard register at once, so the patterns naming them play; the
	// patterns stay dirty until a slot or an apply takes them
	for _, v := range voices {
		if _, err := s.eng.DefineVoice(v.Clone()); err != nil {
			return fmt.Errorf("voice %q: %w", v.Name, err)
		}
		s.voices.put(v, false)
	}
	for _, d := range defs {
		s.pats.put(d, true)
	}
	s.bpm = r.BPM
	s.eng.SetMusicBPM(r.BPM)
	s.eng.SetHarmony(r.Root, r.Scale, r.Progression)
	s.root, s.scale, s.prog = r.Root, r.Scale, r.Progression
	var hits [audio.InstrClap]int
	notes, low := 0, 0
	for _, b := range r.Bars {
		for d := range b.Drums {
			hits[d] += len(b.Drums[d])
		}
		notes, low = notes+len(b.Melody), low+len(b.Bass)
	}
	fmt.Fprintf(s.out, "ripped %s: %d bpm, %s, %d bar(s) from %.2fs; kicks %d snares %d hats %d, melody notes %d, bass notes %d\n",
		a[0], r.BPM, s.keyName(), len(r.Bars), r.Downbeat, hits[audio.InstrKick], hits[audio.InstrSnare], hits[audio.InstrHihat], notes, low)
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	fmt.Fprintf(s.out, "%d pattern(s) added: %s; tempo and key set, slot them to hear the estimate\n", len(defs), strings.Join(names, " "))
	for _, v := range voices {
		fmt.Fprintf(s.out, "voice %s heard: %s, registered\n", v.Name, cmp.Or(v.Wave, "sine"))
	}
	return nil
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
	if s.slots == [audio.MusicSlots]audio.PatternID{} {
		return fmt.Errorf("every slot is silent; slot a pattern first")
	}
	buf, err := s.eng.RenderMusic(s.slots, s.bpm, bars, s.root, s.scale, s.prog, 1)
	if err != nil {
		return err
	}
	f, err := os.Create(a[0])
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

// cmdWrite exports one document entry as a standalone TOML document: an export,
// not a save, so dirty and modified stay as they are (save owns the baseline,
// see doc[T]). It reads the document alone, as show does, a sound shadowing a
// pattern of its name as in docRoot.
func cmdWrite(s *Session, a []string) error {
	name, file := a[0], a[1]
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
	return fmt.Errorf("%q: not in any document", name)
}
