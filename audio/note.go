package audio

import "math"

// Semitone offsets within an octave (C = 0)
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

// MIDI octave numbering: C-1 = 0, C4 = 60 (middle C)
const (
	OctaveSub     = 1 // C1 = 24, ~32Hz
	OctaveBass    = 2 // C2 = 36, ~65Hz
	OctaveLow     = 3 // C3 = 48, ~131Hz
	OctaveMid     = 4 // C4 = 60, ~262Hz
	OctaveHigh    = 5 // C5 = 72, ~523Hz
	OctaveBright  = 6 // C6 = 84, ~1047Hz
	OctaveSparkle = 7 // C7 = 96, ~2093Hz — FIXED: was OctaveSparkel
)

// NoteFrequencies maps MIDI note numbers to Hz, equal temperament
// A4 (note 69) = 440Hz
var NoteFrequencies [128]float64

func init() {
	for i := range NoteFrequencies {
		NoteFrequencies[i] = 440.0 * math.Exp2((float64(i)-69.0)/12.0)
	}
}

// NoteFreq returns the frequency for a MIDI note, clamped to [0,127]
// clamps instead of returning 0. harmony.resolve already clamps, so
// only external TriggerMelodyNote callers are affected: an out-of-range note
// now sounds at the nearest valid pitch rather than a DC-silent voice that
// still holds a polyphony slot for its full envelope
func NoteFreq(midi int) float64 {
	if midi < 0 {
		midi = 0
	} else if midi > 127 {
		midi = 127
	}
	return NoteFrequencies[midi]
}

// MIDINote builds a MIDI note number from semitone (NoteC..NoteB) and octave
func MIDINote(semitone, octave int) int {
	return (octave+1)*12 + semitone
}
