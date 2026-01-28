package constant

import (
	"time"
)

// Tempo and Timing
const (
	DefaultBPM         = 140 // Psytrance range: 135-150
	MinBPM             = 80
	MaxBPM             = 180
	StepsPerBeat       = 4                          // 16th notes
	BeatsPerBar        = 4                          // 4/4 time
	StepsPerBar        = StepsPerBeat * BeatsPerBar // 16 steps
	MaxPatternLen      = 64                         // Max steps per pattern
	DefaultSwing       = 0.0                        // 0.0 = straight, 0.5 = max shuffle
	MaxPolyphony       = 8                          // Simultaneous melody voices
	VoiceStealNone     = 0
	VoiceStealOldest   = 1
	VoiceStealQuietest = 2
	VoiceStealSameNote = 3
)

// Timing calculations (runtime, depends on BPM)
func SamplesPerStep(bpm int) int {
	return AudioSampleRate * 60 / (bpm * StepsPerBeat)
}

func SamplesPerBar(bpm int) int {
	return SamplesPerStep(bpm) * StepsPerBar
}

// Transition timing
const (
	PatternTransitionDefault = 500 * time.Millisecond
	PatternTransitionFast    = 100 * time.Millisecond
	PatternTransitionSlow    = 2000 * time.Millisecond
)

// pow2 computes 2^x without math import in init
func pow2(x float64) float64 {
	// Taylor series approximation for 2^x, sufficient for note table
	ln2 := 0.693147180559945
	y := x * ln2
	sum := 1.0
	term := 1.0
	for i := 1; i < 20; i++ {
		term *= y / float64(i)
		sum += term
	}
	return sum
}

// Note names (semitone offset within octave)
const (
	NoteC  = 0
	NoteCs = 1
	NoteDb = 1
	NoteD  = 2
	NoteDs = 3
	NoteEb = 3
	NoteE  = 4
	NoteF  = 5
	NoteFs = 6
	NoteGb = 6
	NoteG  = 7
	NoteGs = 8
	NoteAb = 8
	NoteA  = 9
	NoteAs = 10
	NoteBb = 10
	NoteB  = 11
)

// Octave constants (MIDI octave numbering)
const (
	OctaveSub     = 1 // C1 = 24, ~32Hz
	OctaveBass    = 2 // C2 = 36, ~65Hz
	OctaveLow     = 3 // C3 = 48, ~131Hz
	OctaveMid     = 4 // C4 = 60, ~262Hz (Middle C)
	OctaveHigh    = 5 // C5 = 72, ~523Hz
	OctaveBright  = 6 // C6 = 84, ~1047Hz
	OctaveSparkel = 7 // C7 = 96, ~2093Hz
)

// MIDINote computes MIDI note number from note + octave
func MIDINote(note, octave int) int {
	return (octave+1)*12 + note // C-1 = 0, C4 = 60
}

// Instrument envelope defaults (seconds)
const (
	// Drums - sharp transients
	KickDecay  = 0.15
	HihatDecay = 0.08
	SnareDecay = 0.12
	ClapDecay  = 0.10

	// Tonal - ADSR
	BassAttack  = 0.005
	BassDecay   = 0.1
	BassSustain = 0.7 // Level, not time
	BassRelease = 0.15

	PianoAttack  = 0.002
	PianoDecay   = 0.3
	PianoSustain = 0.0 // Piano has no sustain, just decay
	PianoRelease = 0.5

	PadAttack  = 0.3
	PadDecay   = 0.2
	PadSustain = 0.8
	PadRelease = 1.0
)

// Sound effect dampening
const (
	RapidFireCooldown  = 30 * time.Millisecond  // Min gap between same sound
	RapidFireDecay     = 0.7                    // Volume multiplier per repeat
	RapidFireMinVolume = 0.2                    // Floor for repeated sounds
	MusicDuckAmount    = 0.6                    // Music volume when effects play
	MusicDuckRelease   = 100 * time.Millisecond // Duck recovery time
)