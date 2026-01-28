package core

// InstrumentType identifies synthesizer presets
type InstrumentType int

const (
	InstrKick InstrumentType = iota
	InstrHihat
	InstrSnare
	InstrClap
	InstrBass
	InstrPiano
	InstrPad
	InstrumentCount
)

// PatternID identifies beat/melody patterns
type PatternID int

const (
	PatternSilence PatternID = iota
	// Beat patterns (loaded from config)
	PatternBeatBasic
	PatternBeatDriving
	PatternBeatBreakdown
	PatternBeatIntense
	// Melody patterns (loaded from config)
	PatternMelodyArpUp
	PatternMelodyArpDown
	PatternMelodyChord
	PatternMelodyHold
	// Dynamic patterns (generated at runtime)
	PatternDynamic = 100
	PatternCount   = PatternDynamic
)

// MusicIntensity drives adaptive music changes
type MusicIntensity int

const (
	IntensityCalm MusicIntensity = iota
	IntensityNormal
	IntensityElevated
	IntensityIntense
	IntensityPeak
)

func (i InstrumentType) String() string {
	names := [...]string{"kick", "hihat", "snare", "clap", "bass", "piano", "pad"}
	if int(i) < len(names) {
		return names[i]
	}
	return "unknown"
}

// IsDrum returns true for percussion instruments
func (i InstrumentType) IsDrum() bool {
	return i <= InstrClap
}

func (p PatternID) String() string {
	names := map[PatternID]string{
		PatternSilence:       "silence",
		PatternBeatBasic:     "beat_basic",
		PatternBeatDriving:   "beat_driving",
		PatternBeatBreakdown: "beat_breakdown",
		PatternBeatIntense:   "beat_intense",
		PatternMelodyArpUp:   "melody_arp_up",
		PatternMelodyArpDown: "melody_arp_down",
		PatternMelodyChord:   "melody_chord",
		PatternMelodyHold:    "melody_hold",
	}
	if name, ok := names[p]; ok {
		return name
	}
	return "dynamic"
}

func (m MusicIntensity) String() string {
	names := [...]string{"calm", "normal", "elevated", "intense", "peak"}
	if int(m) < len(names) {
		return names[m]
	}
	return "unknown"
}

// VoiceStealStrategy determines how to handle voice exhaustion
type VoiceStealStrategy int

const (
	StealOldest VoiceStealStrategy = iota
	StealQuietest
	StealSameNote
	StealNone // Reject new note if all voices busy
)