//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lixenwraith/vif/pkg/audio"
)

func cmdPlay(s *Session, a []string) error {
	d, ok := s.resolveSoundDef(a[0])
	if !ok {
		return fmt.Errorf("%q: no such sound in document or registry", a[0])
	}
	vol := 1.0
	if len(a) > 1 {
		v, err := strconv.ParseFloat(a[1], 64)
		if err != nil {
			return fmt.Errorf("vol %q: not a float", a[1])
		}
		vol = v
	}
	// RenderPreview is the per-keystroke path: one canonical take, validated
	// first, so an in-editor mutation that would produce non-finite samples
	// is reported rather than played.
	buf, err := audio.RenderPreview(d, audio.SoundShape{})
	if err != nil {
		return err
	}
	if !s.eng.PlayBuffer(buf, vol) {
		return fmt.Errorf("play %q: dropped (silent mode, muted, paused, or queue full)", a[0])
	}
	fmt.Fprintf(s.out, "playing %q (%.0fms)\n", a[0], float64(len(buf))*1000/audio.AudioSampleRate)
	return nil
}

func cmdHit(s *Session, a []string) error {
	switch a[0] {
	case "kick", "snare", "hihat", "clap":
		return cmdPlay(s, a) // drums are registry sounds; one audition path
	}
	return fmt.Errorf("want kick|snare|hihat|clap, got %q", a[0])
}

func cmdNote(s *Session, a []string) error {
	midi, err := strconv.Atoi(a[0])
	if err != nil {
		return fmt.Errorf("midi %q: not an int", a[0])
	}
	vel, steps := 0.8, 1
	instr := audio.InstrPiano
	if len(a) > 1 {
		if vel, err = strconv.ParseFloat(a[1], 64); err != nil {
			return fmt.Errorf("vel %q: not a float", a[1])
		}
	}
	if len(a) > 2 {
		if steps, err = strconv.Atoi(a[2]); err != nil {
			return fmt.Errorf("steps %q: not an int", a[2])
		}
	}
	if len(a) > 3 {
		// A voice edited but not applied would sound stale: apply it, visibly
		if s.voices.has(a[3]) && s.voices.isDirty(a[3]) {
			if err := s.applyName(a[3]); err != nil {
				return fmt.Errorf("auto-apply %q: %w", a[3], err)
			}
			fmt.Fprintf(s.out, "auto-applied dirty voice %q\n", a[3])
		}
		i, ok := audio.InstrumentByName(a[3])
		if !ok {
			return fmt.Errorf("unknown voice %q (apply it, or check spelling)", a[3])
		}
		if i.IsDrum() {
			return fmt.Errorf("%q is a drum; use hit", a[3])
		}
		instr = i
	}
	s.eng.TriggerMelodyNote(midi, vel, steps*audio.SamplesPerStep(s.bpm), instr)
	// Tonal voices live in the sequencer and only sound while Generate runs.
	if !s.eng.IsMusicPlaying() {
		fmt.Fprintln(s.out, "note queued, but the transport is stopped — `music start` to hear tonal voices")
	}
	return nil
}

func cmdSlot(s *Session, a []string) error {
	slot, err := strconv.Atoi(a[0])
	if err != nil || slot < 0 || slot >= audio.MusicSlots {
		return fmt.Errorf("slot %q: want 0 to %d", a[0], audio.MusicSlots-1)
	}
	fadeMS, quantize := 250, false // editor default, deliberately not game policy
	if len(a) > 2 {
		if fadeMS, err = strconv.Atoi(a[2]); err != nil {
			return fmt.Errorf("fade_ms %q: not an int", a[2])
		}
	}
	if len(a) > 3 {
		quantize = a[3] == "q" || a[3] == "quantize"
	}

	id := audio.PatternSilence
	if a[1] != "-" {
		// Silently playing a stale registration is worse than an implicit
		// apply the user can see.
		if s.pats.has(a[1]) && s.pats.isDirty(a[1]) {
			if err := s.applyName(a[1]); err != nil {
				return fmt.Errorf("auto-apply %q: %w", a[1], err)
			}
			fmt.Fprintf(s.out, "auto-applied dirty pattern %q\n", a[1])
		}
		if id = audio.PatternIDByName(a[1]); id == audio.PatternSilence {
			return fmt.Errorf("%q: not a registered pattern (apply it, or check spelling)", a[1])
		}
	}
	s.eng.SetPattern(slot, id, fadeMS*audio.AudioSampleRate/1000, quantize)
	s.slots[slot] = id
	fmt.Fprintf(s.out, "slot %d <- %s\n", slot, patName(id))
	return nil
}

func cmdMusic(s *Session, a []string) error {
	switch a[0] {
	case "start":
		s.eng.StartMusic()
	case "stop":
		s.eng.StopMusic()
	case "reset":
		s.eng.ResetMusic()
	default:
		return fmt.Errorf("want start|stop|reset, got %q", a[0])
	}
	return nil
}

func cmdBPM(s *Session, a []string) error {
	n, err := strconv.Atoi(a[0])
	if err != nil {
		return fmt.Errorf("bpm %q: not an int", a[0])
	}
	// Mirror the sequencer clamp so `note` durations match what will sound.
	if n < audio.MinBPM {
		n = audio.MinBPM
	} else if n > audio.MaxBPM {
		n = audio.MaxBPM
	}
	s.bpm = n
	s.eng.SetMusicBPM(n)
	return nil
}

func cmdSwing(s *Session, a []string) error {
	f, err := strconv.ParseFloat(a[0], 64)
	if err != nil {
		return fmt.Errorf("swing %q: not a float", a[0])
	}
	s.eng.SetMusicSwing(f)
	return nil
}

func cmdFill(s *Session, a []string) error {
	switch a[0] {
	case "on":
		s.eng.SetAutoFill(true)
	case "off":
		s.eng.SetAutoFill(false)
	default:
		return fmt.Errorf("want on|off, got %q", a[0])
	}
	return nil
}

// scaleNames are the scales `key` takes, by ScaleID
var scaleNames = [audio.ScaleCount]string{
	audio.ScalePhrygian:      "phrygian",
	audio.ScaleMinor:         "minor",
	audio.ScaleHarmonicMinor: "harmonic_minor",
	audio.ScaleDorian:        "dorian",
	audio.ScaleMinorPent:     "minor_pent",
	audio.ScaleMajor:         "major",
}

func scaleName(id audio.ScaleID) string {
	if id >= 0 && id < audio.ScaleCount {
		return scaleNames[id]
	}
	return "unknown"
}

func cmdKey(s *Session, a []string) error {
	root, err := strconv.Atoi(a[0])
	if err != nil {
		return fmt.Errorf("root %q: not a MIDI note number", a[0])
	}
	i := slices.Index(scaleNames[:], a[1])
	if i < 0 {
		return fmt.Errorf("unknown scale %q (%s)", a[1], strings.Join(scaleNames[:], " "))
	}
	sc := audio.ScaleID(i)
	var prog []int
	for _, p := range a[2:] {
		n, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("degree %q: not an int", p)
		}
		prog = append(prog, n)
	}
	s.eng.SetHarmony(root, sc, prog) // nil prog keeps the current progression
	if root > 0 && root < 128 {
		s.root = root // the engine keeps its root for any other
	}
	s.scale = sc
	if prog != nil {
		s.prog = prog
	}
	return nil
}

func cmdMute(s *Session, a []string) error {
	if a[0] == "slot" {
		return muteSlot(s, a[1:])
	}
	if len(a) > 1 {
		return fmt.Errorf("usage: %s", commands["mute"].usage)
	}
	var music, sfx bool
	switch a[0] {
	case "music":
		music = true
	case "sfx":
		sfx = true
	case "all":
		music, sfx = true, true
	case "none":
	default:
		return fmt.Errorf("want music|sfx|all|none, got %q", a[0])
	}
	s.eng.SetMusicMuted(music)
	s.eng.SetSfxMuted(sfx)
	return nil
}

// muteSlot sets one slot's mute; its pattern plays on in time under it
func muteSlot(s *Session, a []string) error {
	if len(a) != 2 {
		return fmt.Errorf("usage: %s", commands["mute"].usage)
	}
	slot, err := strconv.Atoi(a[0])
	if err != nil || slot < 0 || slot >= audio.MusicSlots {
		return fmt.Errorf("slot %q: want 0 to %d", a[0], audio.MusicSlots-1)
	}
	switch a[1] {
	case "on", "off":
		s.eng.SetSlotMuted(slot, a[1] == "on")
		return nil
	}
	return fmt.Errorf("want on|off, got %q", a[1])
}

func cmdVol(s *Session, a []string) error {
	f, err := strconv.ParseFloat(a[0], 64)
	if err != nil {
		return fmt.Errorf("vol %q: not a float", a[0])
	}
	s.eng.SetSfxVolume(f)
	return nil
}

func cmdWhere(s *Session, a []string) error {
	bar, step, running := s.eng.Transport()
	state := "stopped"
	if running {
		state = "running"
	}
	fmt.Fprintf(s.out, "bar %d step %d (%s)", bar, step, state)
	for i := range audio.MusicSlots {
		fmt.Fprintf(s.out, "  slot%d=%s", i, patName(s.eng.SlotPattern(i)))
		if s.eng.IsSlotMuted(i) {
			fmt.Fprint(s.out, " (muted)")
		}
	}
	fmt.Fprintln(s.out)
	return nil
}

func cmdStat(s *Session, a []string) error {
	be := s.eng.BackendName()
	if be == "" {
		be = "(none)"
	}
	played, dropped := s.eng.Stats()
	fmt.Fprintf(s.out, "backend %s  silent=%v  played=%d dropped=%d  bpm=%d\n",
		be, s.eng.IsSilent(), played, dropped, s.bpm)
	if s.startErr != nil {
		fmt.Fprintf(s.out, "start error: %v\n", s.startErr)
	}
	if err := s.eng.SpecError(); err != nil {
		fmt.Fprintf(s.out, "spec error: %v\n", err)
	}
	ear := "the analysis"
	if s.frontEnd != nil {
		ear = fmt.Sprintf("model %s (%d weights)", s.files.model, s.frontEnd.Model.Parameters())
	}
	fmt.Fprintf(s.out, "rip hears with %s; params %s\n", ear, cmp.Or(s.ripSrc, "built-in"))
	fmt.Fprintf(s.out, "sounds %d (dirty %v)  voices %d (dirty %v)  patterns %d (dirty %v)\n",
		len(s.sounds.order), s.sounds.dirtyNames(),
		len(s.voices.order), s.voices.dirtyNames(),
		len(s.pats.order), s.pats.dirtyNames())
	fmt.Fprintf(s.out, "unsaved to disk: sounds=%v patterns=%v\n",
		s.soundsModified(), s.pats.modified)
	return nil
}

func cmdWait(s *Session, a []string) error {
	ms, err := strconv.Atoi(a[0])
	if err != nil {
		return fmt.Errorf("wait %q: not an int", a[0])
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return nil
}
