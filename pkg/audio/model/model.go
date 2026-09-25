// Package model contains the audio identifiers shared with the game protocol.
// It has no mixer, backend, filesystem, or synthesis dependencies.
package model

// SoundID identifies a registered sound. Zero always means silence.
type SoundID int32

const SoundNone SoundID = 0

// InstrumentType identifies a synthesis voice. Drums must precede tonal voices;
// IsDrum and the mixer drum arrays depend on that order.
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

// IsDrum reports whether the instrument uses pre-rendered drum variants.
func (i InstrumentType) IsDrum() bool { return i <= InstrClap }

// InstrumentByName resolves an instrument's canonical name.
func InstrumentByName(name string) (InstrumentType, bool) {
	for i, candidate := range instrumentNames {
		if name == candidate {
			return InstrumentType(i), true
		}
	}
	return 0, false
}

// PatternID identifies a registered pattern; names, not numeric IDs, persist.
// Only the patterns code owns have fixed IDs; authored ones are assigned at load.
type PatternID int32

const (
	PatternSilence PatternID = iota
	PatternMelodyGen
	PatternDynamic PatternID = 100
)

var patternNames = map[PatternID]string{
	PatternSilence:   "silence",
	PatternMelodyGen: "melody_gen",
}

func (p PatternID) String() string {
	if name, ok := patternNames[p]; ok {
		return name
	}
	return "dynamic"
}

// ScaleID selects a scale interval table. Phrygian is the zero-value default.
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

// Intensity selects a registered arrangement tier.
type Intensity int32

const (
	IntensityCalm Intensity = iota
	IntensityNormal
	IntensityElevated
	IntensityIntense
	IntensityPeak
	IntensityCount
)

var intensityNames = [...]string{"calm", "normal", "elevated", "intense", "peak"}

func (i Intensity) String() string {
	if i >= 0 && int(i) < len(intensityNames) {
		return intensityNames[i]
	}
	return "unknown"
}
