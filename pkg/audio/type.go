package audio

import (
	"errors"
	"strings"
	"sync"

	"github.com/lixenwraith/vif/pkg/audio/model"
)

// === Instruments ===

type InstrumentType = model.InstrumentType

const (
	InstrKick       = model.InstrKick
	InstrSnare      = model.InstrSnare
	InstrHihat      = model.InstrHihat
	InstrClap       = model.InstrClap
	InstrBass       = model.InstrBass
	InstrPiano      = model.InstrPiano
	InstrPad        = model.InstrPad
	InstrumentCount = model.InstrumentCount
)

// InstrumentByName resolves a canonical instrument name — the InstrumentType
// String form, which is the TOML key space and is stable across enum
// reordering.
func InstrumentByName(s string) (InstrumentType, bool) {
	return model.InstrumentByName(s)
}

// === Patterns ===

// PatternID identifies a registered pattern
// Ordering is free: music.toml overrides resolve by name, not ID
type PatternID = model.PatternID

const (
	PatternSilence   = model.PatternSilence
	PatternMelodyGen = model.PatternMelodyGen
	PatternDynamic   = model.PatternDynamic
)

// === Harmony ===

// ScaleID selects a scale interval table in harmony
// Phrygian is index 0: engine default and zero value of newHarmony
type ScaleID = model.ScaleID

const (
	ScalePhrygian      = model.ScalePhrygian
	ScaleMinor         = model.ScaleMinor
	ScaleHarmonicMinor = model.ScaleHarmonicMinor
	ScaleDorian        = model.ScaleDorian
	ScaleMinorPent     = model.ScaleMinorPent
	ScaleMajor         = model.ScaleMajor
	ScaleCount         = model.ScaleCount
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
	BackendNull
	BackendWAV
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
