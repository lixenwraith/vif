package audio

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

var (
	beatPatterns   = make(map[core.PatternID]*BeatPattern)
	melodyPatterns = make(map[core.PatternID]*MelodyPattern)
	patternMu      sync.RWMutex
)

// RegisterBeatPattern adds a beat pattern to registry
func RegisterBeatPattern(p *BeatPattern) {
	patternMu.Lock()
	beatPatterns[p.ID] = p
	patternMu.Unlock()
}

// RegisterMelodyPattern adds a melody pattern to registry
func RegisterMelodyPattern(p *MelodyPattern) {
	patternMu.Lock()
	melodyPatterns[p.ID] = p
	patternMu.Unlock()
}

// GetBeatPattern retrieves beat pattern by ID
func GetBeatPattern(id core.PatternID) *BeatPattern {
	patternMu.RLock()
	defer patternMu.RUnlock()
	return beatPatterns[id]
}

// GetMelodyPattern retrieves melody pattern by ID
func GetMelodyPattern(id core.PatternID) *MelodyPattern {
	patternMu.RLock()
	defer patternMu.RUnlock()
	return melodyPatterns[id]
}

// InitDefaultPatterns registers built-in patterns
// Called at startup; config-loaded patterns override these
func InitDefaultPatterns() {
	// Basic 4-on-floor beat
	RegisterBeatPattern(&BeatPattern{
		ID:     core.PatternBeatBasic,
		Length: 16,
		Kick: []StepTrigger{
			{Step: 0, Velocity: 1.0},
			{Step: 4, Velocity: 1.0},
			{Step: 8, Velocity: 1.0},
			{Step: 12, Velocity: 1.0},
		},
		Hihat: []StepTrigger{
			{Step: 2, Velocity: 0.6},
			{Step: 6, Velocity: 0.6},
			{Step: 10, Velocity: 0.6},
			{Step: 14, Velocity: 0.6},
		},
		Snare: []StepTrigger{
			{Step: 4, Velocity: 0.9},
			{Step: 12, Velocity: 0.9},
		},
	})

	// Driving psytrance beat
	RegisterBeatPattern(&BeatPattern{
		ID:     core.PatternBeatDriving,
		Length: 16,
		Kick: []StepTrigger{
			{Step: 0, Velocity: 1.0},
			{Step: 4, Velocity: 1.0},
			{Step: 8, Velocity: 1.0},
			{Step: 12, Velocity: 1.0},
		},
		Hihat: []StepTrigger{
			{Step: 0, Velocity: 0.4},
			{Step: 2, Velocity: 0.7},
			{Step: 4, Velocity: 0.4},
			{Step: 6, Velocity: 0.7},
			{Step: 8, Velocity: 0.4},
			{Step: 10, Velocity: 0.7},
			{Step: 12, Velocity: 0.4},
			{Step: 14, Velocity: 0.7},
		},
	})

	// Simple arpeggio up
	RegisterMelodyPattern(&MelodyPattern{
		ID:         core.PatternMelodyArpUp,
		Length:     16,
		Instrument: core.InstrPiano,
		Notes: []NoteTrigger{
			{Step: 0, NoteOffset: 0, Velocity: 0.8, Duration: 2},
			{Step: 4, NoteOffset: 4, Velocity: 0.7, Duration: 2},   // Major 3rd
			{Step: 8, NoteOffset: 7, Velocity: 0.7, Duration: 2},   // 5th
			{Step: 12, NoteOffset: 12, Velocity: 0.6, Duration: 2}, // Octave
		},
	})

	// Arpeggio down
	RegisterMelodyPattern(&MelodyPattern{
		ID:         core.PatternMelodyArpDown,
		Length:     16,
		Instrument: core.InstrPiano,
		Notes: []NoteTrigger{
			{Step: 0, NoteOffset: 12, Velocity: 0.8, Duration: 2},
			{Step: 4, NoteOffset: 7, Velocity: 0.7, Duration: 2},
			{Step: 8, NoteOffset: 4, Velocity: 0.7, Duration: 2},
			{Step: 12, NoteOffset: 0, Velocity: 0.6, Duration: 2},
		},
	})
}