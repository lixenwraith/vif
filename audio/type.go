package audio

import (
	"errors"
	"strings"
	"sync"
)

// === Sound effects ===

// SoundType identifies a preset sound effect
type SoundType int32

const (
	SoundError SoundType = iota
	SoundBell
	SoundWhoosh
	SoundCoin
	SoundShield
	SoundZap
	SoundCrackle
	SoundMetalHit
	SoundExplosion
	SoundBullet
	SoundRing
	SoundTypeCount
)

// === Instruments ===

// InstrumentType identifies a synthesis voice
// Drums precede tonal instruments: IsDrum, the drumKit variant array and
// PatternPlayer's per-drum voice pairs all depend on that ordering
type InstrumentType int32

const (
	InstrKick InstrumentType = iota
	InstrSnare
	InstrHihat
	InstrClap
	InstrBass
	InstrPiano
	InstrPad
	InstrumentCount
)

var instrumentNames = [...]string{"kick", "snare", "hihat", "clap", "bass", "piano", "pad"}

func (i InstrumentType) String() string {
	if i >= 0 && int(i) < len(instrumentNames) {
		return instrumentNames[i]
	}
	return "unknown"
}

// IsDrum reports whether the instrument uses pre-rendered drum variants
func (i InstrumentType) IsDrum() bool { return i <= InstrClap }

// === Patterns ===

// PatternID identifies a registered pattern
// Ordering is free: music.toml overrides resolve by name, not ID
type PatternID int32

const (
	PatternSilence PatternID = iota
	PatternBeatBasic
	PatternBeatDriving
	PatternBeatDrivingPlus
	PatternBeatBreaks
	PatternBeatHalftime
	PatternBeatBreakdown
	PatternBeatIntense
	PatternMelodyHold
	PatternMelodyArpUp
	PatternMelodyArpDown
	PatternMelodyChord
	PatternMelodyGen
	// PatternDynamic marks the start of runtime-registered IDs
	PatternDynamic PatternID = 100
)

// patternNames is the canonical name table
// keys match the InitDefaultPatterns registration names, which is what
// music.toml overrides resolve against (core returned "melody_hold" for a
// pattern registered as "melody_bassline")
var patternNames = map[PatternID]string{
	PatternSilence:         "silence",
	PatternBeatBasic:       "beat_basic",
	PatternBeatDriving:     "beat_driving",
	PatternBeatDrivingPlus: "beat_driving_plus",
	PatternBeatBreaks:      "beat_breaks",
	PatternBeatHalftime:    "beat_halftime",
	PatternBeatBreakdown:   "beat_breakdown",
	PatternBeatIntense:     "beat_intense",
	PatternMelodyHold:      "melody_bassline",
	PatternMelodyArpUp:     "melody_bass_arp",
	PatternMelodyArpDown:   "melody_bass_arp_down",
	PatternMelodyChord:     "melody_full",
	PatternMelodyGen:       "melody_gen",
}

// String returns the canonical pattern name (music.toml override key)
func (p PatternID) String() string {
	if n, ok := patternNames[p]; ok {
		return n
	}
	return "dynamic"
}

// === Harmony ===

// ScaleID selects a scale interval table in harmony
// Phrygian is index 0: engine default and zero value of newHarmony
type ScaleID int32

const (
	ScalePhrygian ScaleID = iota
	ScaleMinor
	ScaleHarmonicMinor
	ScaleDorian
	ScaleMinorPent
	ScaleMajor
	ScaleCount
)

// === Polyphony ===

// VoiceStealStrategy selects the polyphony-exhaustion policy
type VoiceStealStrategy int32

const (
	StealNone     VoiceStealStrategy = iota // reject the new note
	StealLowest                             // lowest envelope level (deepest into decay)
	StealQuietest                           // lowest envelope × velocity
	StealSameNote
)

// === Backends ===

// BackendType identifies the output backend
type BackendType int

const (
	BackendPulse BackendType = iota
	BackendPipeWire
	BackendALSA
	BackendSoX
	BackendFFplay
	BackendOSS
)

// BackendConfig describes a CLI audio backend
type BackendConfig struct {
	Type BackendType
	Name string
	Path string
	Args []string
}

// Sentinel errors
var (
	ErrNoAudioBackend = errors.New("no compatible audio backend found")
	ErrPipeClosed     = errors.New("audio pipe closed")
)

// tailBuffer is a bounded sink for backend stderr
// Writer: backend process; readers: probe diagnostics and telemetry
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailMax {
		t.buf = t.buf[len(t.buf)-stderrTailMax:]
	}
	t.mu.Unlock()
	return len(p), nil
}

// LastLine returns the final non-empty stderr line for compact diagnostics
func (t *tailBuffer) LastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.TrimRight(string(t.buf), "\n\r ")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}
