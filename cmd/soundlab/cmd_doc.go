package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/lixenwraith/vi-fighter/audio"
)

func cmdLoad(s *Session, a []string) error {
	snd, err := kindArg(a[0])
	if err != nil {
		return err
	}
	if snd {
		return s.loadSoundFile(a[1], true)
	}
	return s.loadPatternFile(a[1], true)
}

func cmdMerge(s *Session, a []string) error {
	snd, err := kindArg(a[0])
	if err != nil {
		return err
	}
	if snd {
		return s.loadSoundFile(a[1], false)
	}
	return s.loadPatternFile(a[1], false)
}

func cmdBuiltin(s *Session, a []string) error {
	snd, err := kindArg(a[0])
	if err != nil {
		return err
	}
	if snd {
		return s.seedBuiltinSounds()
	}
	return s.seedBuiltinPatterns()
}

func cmdSave(s *Session, a []string) error {
	snd, err := kindArg(a[0])
	if err != nil {
		return err
	}
	file := ""
	if len(a) > 1 {
		file = a[1]
	}
	if snd {
		return s.saveSounds(file)
	}
	return s.savePatterns(file)
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
		sets := audio.RenderVariants(d, audio.SFXParams{})
		if vi < 0 || vi >= len(sets) {
			return fmt.Errorf("variant %d out of range, have %d", vi, len(sets))
		}
		buf = sets[vi]
	} else {
		var err error
		if buf, err = audio.RenderPreview(d, audio.SFXParams{}); err != nil {
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

// cmdWrite exports one document entry as a standalone TOML document. It is
// an export, not a save: the working document still diverges from its
// provenance file, so dirty and modified are deliberately untouched — save
// owns the baseline (see doc[T]). Document-only resolution, matching show:
// what the browser displays is what writes, with the same sounds-shadow-
// patterns precedence as docRoot.
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
