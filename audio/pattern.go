package audio

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

// Step is one trigger within a track
type Step struct {
	Pos  int
	Vel  float64
	Deg  int     // scale degree (tonal only)
	Oct  int     // octave offset (tonal only)
	Dur  int     // steps (tonal only); <=0 = 1
	Prob float64 // 0 or 1 = always
}

// Track is one instrument lane within a pattern
type Track struct {
	Instr       core.InstrumentType
	FollowChord bool
	Events      []Step
}

// Pattern is the unified rhythm/melody pattern
type Pattern struct {
	ID     core.PatternID
	Name   string
	Steps  int
	Tracks []Track
}

var (
	patterns    = make(map[core.PatternID]*Pattern)
	patternName = make(map[string]core.PatternID)
	nextDynamic = core.PatternID(core.PatternDynamic)
	patternMu   sync.RWMutex
)

// RegisterPattern adds or overwrites a pattern; ID PatternSilence allocates
// a dynamic ID. Returns the effective ID
func RegisterPattern(p *Pattern) core.PatternID {
	patternMu.Lock()
	defer patternMu.Unlock()
	if p.ID == core.PatternSilence {
		if id, ok := patternName[p.Name]; ok {
			p.ID = id // name override replaces in place
		} else {
			p.ID = nextDynamic
			nextDynamic++
		}
	}
	patterns[p.ID] = p
	if p.Name != "" {
		patternName[p.Name] = p.ID
	}
	return p.ID
}

// GetPattern retrieves a pattern by ID
func GetPattern(id core.PatternID) *Pattern {
	patternMu.RLock()
	defer patternMu.RUnlock()
	return patterns[id]
}

// PatternIDByName resolves a registered name; PatternSilence if absent
func PatternIDByName(name string) core.PatternID {
	patternMu.RLock()
	defer patternMu.RUnlock()
	return patternName[name]
}

// fourFloor is a 4-on-floor kick lane helper
func fourFloor(vel float64) []Step {
	return []Step{{Pos: 0, Vel: vel}, {Pos: 4, Vel: vel}, {Pos: 8, Vel: vel}, {Pos: 12, Vel: vel}}
}

// rollingBass is the psytrance offbeat-16th bass lane: k-b-b-b per beat
func rollingBass() []Step {
	ev := make([]Step, 0, 12)
	for beat := 0; beat < 4; beat++ {
		base := beat * 4
		ev = append(ev,
			Step{Pos: base + 1, Vel: 0.85, Dur: 1},
			Step{Pos: base + 2, Vel: 0.7, Dur: 1},
			Step{Pos: base + 3, Vel: 0.7, Dur: 1},
		)
	}
	return ev
}

// InitDefaultPatterns registers built-in patterns; config file overrides by name
func InitDefaultPatterns() {
	// --- Rhythm (slot 0) ---
	RegisterPattern(&Pattern{
		ID: core.PatternBeatBasic, Name: "beat_basic", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrKick, Events: fourFloor(0.9)},
			{Instr: core.InstrHihat, Events: []Step{
				{Pos: 2, Vel: 0.4}, {Pos: 6, Vel: 0.4}, {Pos: 10, Vel: 0.4}, {Pos: 14, Vel: 0.4},
			}},
		},
	})

	RegisterPattern(&Pattern{
		ID: core.PatternBeatDriving, Name: "beat_driving", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrKick, Events: fourFloor(1.0)},
			{Instr: core.InstrHihat, Events: []Step{
				{Pos: 0, Vel: 0.3}, {Pos: 2, Vel: 0.7}, {Pos: 4, Vel: 0.3}, {Pos: 6, Vel: 0.7},
				{Pos: 8, Vel: 0.3}, {Pos: 10, Vel: 0.7}, {Pos: 12, Vel: 0.3}, {Pos: 14, Vel: 0.7},
			}},
		},
	})

	RegisterPattern(&Pattern{
		ID: core.PatternBeatBreakdown, Name: "beat_breakdown", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrKick, Events: []Step{{Pos: 0, Vel: 0.9}}},
			{Instr: core.InstrClap, Events: []Step{{Pos: 8, Vel: 0.6}}},
		},
	})

	intenseHats := make([]Step, 16)
	for i := range intenseHats {
		v := 0.3
		if i%2 == 0 {
			v = 0.55
		}
		intenseHats[i] = Step{Pos: i, Vel: v}
	}
	RegisterPattern(&Pattern{
		ID: core.PatternBeatIntense, Name: "beat_intense", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrKick, Events: fourFloor(1.0)},
			{Instr: core.InstrHihat, Events: intenseHats},
			{Instr: core.InstrSnare, Events: []Step{
				{Pos: 4, Vel: 0.9}, {Pos: 12, Vel: 0.9},
				{Pos: 7, Vel: 0.35, Prob: 0.35}, {Pos: 15, Vel: 0.35, Prob: 0.35},
			}},
			{Instr: core.InstrClap, Events: []Step{{Pos: 12, Vel: 0.6}}},
		},
	})

	// --- Melody (slot 1) ---
	RegisterPattern(&Pattern{
		ID: core.PatternMelodyHold, Name: "melody_bassline", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrBass, FollowChord: true, Events: rollingBass()},
		},
	})

	arp := make([]Step, 0, 8)
	degs := []int{0, 2, 4, 7}
	for i := 0; i < 8; i++ {
		arp = append(arp, Step{Pos: i * 2, Vel: 0.55, Deg: degs[i%4], Oct: 2, Dur: 1, Prob: 0.85})
	}
	RegisterPattern(&Pattern{
		ID: core.PatternMelodyArpUp, Name: "melody_bass_arp", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrBass, FollowChord: true, Events: rollingBass()},
			{Instr: core.InstrPiano, FollowChord: true, Events: arp},
		},
	})

	arpDown := make([]Step, 0, 8)
	for i := 0; i < 8; i++ {
		arpDown = append(arpDown, Step{Pos: i * 2, Vel: 0.55, Deg: degs[3-i%4], Oct: 2, Dur: 1, Prob: 0.85})
	}
	RegisterPattern(&Pattern{
		ID: core.PatternMelodyArpDown, Name: "melody_bass_arp_down", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrBass, FollowChord: true, Events: rollingBass()},
			{Instr: core.InstrPiano, FollowChord: true, Events: arpDown},
		},
	})

	RegisterPattern(&Pattern{
		ID: core.PatternMelodyChord, Name: "melody_full", Steps: 16,
		Tracks: []Track{
			{Instr: core.InstrBass, FollowChord: true, Events: rollingBass()},
			{Instr: core.InstrPiano, FollowChord: true, Events: arp},
			{Instr: core.InstrPad, FollowChord: true, Events: []Step{
				{Pos: 0, Vel: 0.35, Deg: 0, Oct: 1, Dur: 16},
				{Pos: 0, Vel: 0.3, Deg: 2, Oct: 1, Dur: 16},
				{Pos: 0, Vel: 0.3, Deg: 4, Oct: 1, Dur: 16},
			}},
		},
	})
}

