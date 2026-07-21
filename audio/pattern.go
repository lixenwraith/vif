package audio

import (
	"sync"
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
	Instr       InstrumentType
	FollowChord bool
	Humanize    float64 // 0 = quantized; scales velocity jitter + micro-lag
	Events      []Step
}

// Pattern is the unified rhythm/melody pattern
type Pattern struct {
	ID     PatternID
	Name   string
	Steps  int
	Tracks []Track
}

var (
	patterns    = make(map[PatternID]*Pattern)
	patternName = make(map[string]PatternID)
	nextDynamic = PatternID(PatternDynamic)
	patternMu   sync.RWMutex
)

// Fill pattern IDs; written once in InitDefaultPatterns (main goroutine,
// pre-mixer — happens-before via mixer goroutine creation), read-only afterward
var fillIDs []PatternID

// InitDefaultPatterns registers built-in patterns; config file overrides by name
func InitDefaultPatterns() {
	// --- Rhythm (slot 0) ---
	RegisterPattern(&Pattern{
		ID: PatternBeatBasic, Name: "beat_basic", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: fourFloor(0.9)},
			{Instr: InstrHihat, Humanize: 0.5, Events: []Step{
				{Pos: 2, Vel: 0.4}, {Pos: 6, Vel: 0.4}, {Pos: 10, Vel: 0.4}, {Pos: 14, Vel: 0.4},
			}},
		},
	})

	drivingHats := []Step{
		{Pos: 0, Vel: 0.3}, {Pos: 2, Vel: 0.7}, {Pos: 4, Vel: 0.3}, {Pos: 6, Vel: 0.7},
		{Pos: 8, Vel: 0.3}, {Pos: 10, Vel: 0.7}, {Pos: 12, Vel: 0.3}, {Pos: 14, Vel: 0.7},
	}
	RegisterPattern(&Pattern{
		ID: PatternBeatDriving, Name: "beat_driving", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: fourFloor(1.0)},
			{Instr: InstrHihat, Humanize: 0.6, Events: drivingHats},
		},
	})

	// Driving + Euclidean syncopation (Elevated tier)
	RegisterPattern(&Pattern{
		ID: PatternBeatDrivingPlus, Name: "beat_driving_plus", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: fourFloor(1.0)},
			{Instr: InstrHihat, Humanize: 0.6, Events: drivingHats},
			{Instr: InstrHihat, Humanize: 0.7, Events: euclidEvents(EuclidMask(5, 16, 3), 16, 0.3, 0.45, 3, 0)},
			{Instr: InstrSnare, Humanize: 0.5, Events: euclidEvents(EuclidMask(3, 16, 2), 16, 0.2, 0.2, 0, 0.3)}, // ghosts
		},
	})

	RegisterPattern(&Pattern{
		ID: PatternBeatBreakdown, Name: "beat_breakdown", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 0.9}}},
			{Instr: InstrClap, Humanize: 0.3, Events: []Step{{Pos: 8, Vel: 0.6}}},
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
		ID: PatternBeatIntense, Name: "beat_intense", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: fourFloor(1.0)},
			{Instr: InstrHihat, Humanize: 0.6, Events: intenseHats},
			{Instr: InstrSnare, Humanize: 0.4, Events: []Step{
				{Pos: 4, Vel: 0.9}, {Pos: 12, Vel: 0.9},
				{Pos: 7, Vel: 0.35, Prob: 0.35}, {Pos: 15, Vel: 0.35, Prob: 0.35},
			}},
			{Instr: InstrClap, Humanize: 0.3, Events: []Step{{Pos: 12, Vel: 0.6}}},
		},
	})

	// Breakbeat
	RegisterPattern(&Pattern{
		ID: PatternBeatBreaks, Name: "beat_breaks", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: []Step{
				{Pos: 0, Vel: 0.95}, {Pos: 7, Vel: 0.7, Prob: 0.6}, {Pos: 10, Vel: 0.9},
			}},
			{Instr: InstrSnare, Humanize: 0.4, Events: []Step{
				{Pos: 4, Vel: 0.9}, {Pos: 12, Vel: 0.9}, {Pos: 14, Vel: 0.3, Prob: 0.4},
			}},
			{Instr: InstrHihat, Humanize: 0.7, Events: euclidEvents(EuclidMask(7, 16, 0), 16, 0.35, 0.5, 4, 0)},
		},
	})

	// Halftime
	RegisterPattern(&Pattern{
		ID: PatternBeatHalftime, Name: "beat_halftime", Steps: 16,
		Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 1.0}, {Pos: 3, Vel: 0.5, Prob: 0.4}}},
			{Instr: InstrSnare, Humanize: 0.3, Events: []Step{{Pos: 8, Vel: 0.95}}},
			{Instr: InstrClap, Humanize: 0.3, Events: []Step{{Pos: 8, Vel: 0.4}}},
			{Instr: InstrHihat, Humanize: 0.7, Events: euclidEvents(EuclidMask(4, 16, 2), 16, 0.3, 0.3, 0, 0)},
		},
	})

	// --- Melody (slot 1): unchanged registrations + Humanize ---
	RegisterPattern(&Pattern{
		ID: PatternMelodyHold, Name: "melody_bassline", Steps: 16,
		Tracks: []Track{
			{Instr: InstrBass, FollowChord: true, Humanize: 0.15, Events: rollingBass()},
		},
	})

	arp := make([]Step, 0, 8)
	degs := []int{0, 2, 4, 7}
	for i := 0; i < 8; i++ {
		arp = append(arp, Step{Pos: i * 2, Vel: 0.55, Deg: degs[i%4], Oct: 2, Dur: 1, Prob: 0.85})
	}
	RegisterPattern(&Pattern{
		ID: PatternMelodyArpUp, Name: "melody_bass_arp", Steps: 16,
		Tracks: []Track{
			{Instr: InstrBass, FollowChord: true, Humanize: 0.15, Events: rollingBass()},
			{Instr: InstrPiano, FollowChord: true, Humanize: 0.4, Events: arp},
		},
	})

	arpDown := make([]Step, 0, 8)
	for i := 0; i < 8; i++ {
		arpDown = append(arpDown, Step{Pos: i * 2, Vel: 0.55, Deg: degs[3-i%4], Oct: 2, Dur: 1, Prob: 0.85})
	}
	RegisterPattern(&Pattern{
		ID: PatternMelodyArpDown, Name: "melody_bass_arp_down", Steps: 16,
		Tracks: []Track{
			{Instr: InstrBass, FollowChord: true, Humanize: 0.15, Events: rollingBass()},
			{Instr: InstrPiano, FollowChord: true, Humanize: 0.4, Events: arpDown},
		},
	})

	RegisterPattern(&Pattern{
		ID: PatternMelodyChord, Name: "melody_full", Steps: 16,
		Tracks: []Track{
			{Instr: InstrBass, FollowChord: true, Humanize: 0.15, Events: rollingBass()},
			{Instr: InstrPiano, FollowChord: true, Humanize: 0.4, Events: arp},
			{Instr: InstrPad, FollowChord: true, Events: []Step{
				{Pos: 0, Vel: 0.35, Deg: 0, Oct: 1, Dur: 16},
				{Pos: 0, Vel: 0.3, Deg: 2, Oct: 1, Dur: 16},
				{Pos: 0, Vel: 0.3, Deg: 4, Oct: 1, Dur: 16},
			}},
		},
	})

	// Generative melody
	registerMelodyGen()

	// Slot-2 fill bank (dynamic IDs, seeded rng selects at runtime)
	roll := make([]Step, 16)
	for i := range roll {
		roll[i] = Step{Pos: i, Vel: 0.3 + 0.043*float64(i)}
	}
	fillIDs = fillIDs[:0]
	fillIDs = append(fillIDs,
		RegisterPattern(&Pattern{Name: "fill_snare_roll", Steps: 16, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 0.9}}},
			{Instr: InstrSnare, Humanize: 0.5, Events: roll},
		}}),
		RegisterPattern(&Pattern{Name: "fill_clap_build", Steps: 16, Tracks: []Track{
			{Instr: InstrClap, Humanize: 0.4, Events: euclidEvents(EuclidMask(11, 16, 0), 16, 0.5, 0.65, 4, 0)},
			{Instr: InstrSnare, Humanize: 0.3, Events: []Step{
				{Pos: 8, Vel: 0.6}, {Pos: 12, Vel: 0.75}, {Pos: 14, Vel: 0.85}, {Pos: 15, Vel: 0.95},
			}},
		}}),
		RegisterPattern(&Pattern{Name: "fill_kick_stutter", Steps: 16, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{
				{Pos: 0, Vel: 0.9}, {Pos: 8, Vel: 0.7}, {Pos: 10, Vel: 0.75},
				{Pos: 12, Vel: 0.8}, {Pos: 13, Vel: 0.85}, {Pos: 14, Vel: 0.9}, {Pos: 15, Vel: 0.95},
			}},
			{Instr: InstrHihat, Humanize: 0.6, Events: euclidEvents(EuclidMask(5, 16, 1), 16, 0.3, 0.3, 0, 0)},
		}}),
		RegisterPattern(&Pattern{Name: "fill_dropout", Steps: 16, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 0.9}}},
			{Instr: InstrSnare, Events: []Step{{Pos: 15, Vel: 0.9}}},
		}}),
	)
}

// RegisterPattern adds or overwrites a pattern; ID PatternSilence allocates
// a dynamic ID. Returns the effective ID
func RegisterPattern(p *Pattern) PatternID {
	patternMu.Lock()
	defer patternMu.Unlock()
	if p.ID == PatternSilence {
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
func GetPattern(id PatternID) *Pattern {
	patternMu.RLock()
	defer patternMu.RUnlock()
	return patterns[id]
}

// PatternIDByName resolves a registered name; PatternSilence if absent
func PatternIDByName(name string) PatternID {
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
