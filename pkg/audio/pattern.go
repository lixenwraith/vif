package audio

import (
	"fmt"
	"slices"
	"strings"
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
	Desc        string
	FollowChord bool
	Humanize    float64 // 0 = quantized; scales velocity jitter + micro-lag
	Events      []Step
}

// Pattern is the unified rhythm/melody pattern
type Pattern struct {
	ID     PatternID
	Name   string
	Desc   string
	Steps  int
	Tracks []Track
}

// Clone deep-copies a pattern. Mandatory before editing anything obtained from
// RegisteredPatterns or GetPattern: the mixer reads those structs directly from
// its goroutine, so in-place mutation is a data race. Register the clone; the
// swap is a pointer store under the registry lock.
func (p *Pattern) Clone() *Pattern {
	if p == nil {
		return nil
	}
	c := *p
	c.Tracks = slices.Clone(p.Tracks)
	for i := range c.Tracks {
		c.Tracks[i].Events = slices.Clone(p.Tracks[i].Events)
	}
	return &c
}

var (
	patterns    = make(map[PatternID]*Pattern)
	patternName = make(map[string]PatternID)
	nextDynamic = PatternID(PatternDynamic)
	patternMu   sync.RWMutex
)

// fillIDs is the slot-2 fill bank; written once by collectFills before the mixer
// exists (happens-before via its goroutine creation), read-only afterward.
var fillIDs []PatternID

// FillPrefix marks a pattern for the slot-2 fill bank.
const FillPrefix = "fill_"

// collectFills fixes the fill bank at Start: every registered pattern named
// FillPrefix*, in ID order. A fill defined after Start does not join it.
func collectFills() {
	fillIDs = fillIDs[:0]
	for _, p := range RegisteredPatterns() {
		if strings.HasPrefix(p.Name, FillPrefix) {
			fillIDs = append(fillIDs, p.ID)
		}
	}
}

// tierPools is each tier's rhythm and melody pool, resolved to IDs
type tierPools [IntensityCount][2][]PatternID

// resolveArrangements binds each tier's pattern names to IDs
func resolveArrangements(tiers [IntensityCount]Arrangement) (tierPools, error) {
	var out tierPools
	for t, a := range tiers {
		for slot, names := range [...][]string{a.Rhythm, a.Melody} {
			for _, n := range names {
				id := PatternIDByName(n)
				if id == PatternSilence {
					return out, fmt.Errorf("arrangement %s: unknown pattern %q", Intensity(t), n)
				}
				out[t][slot] = append(out[t][slot], id)
			}
		}
	}
	return out, nil
}

// RegisterPattern validates and adds or overwrites a pattern; ID
// PatternSilence allocates a dynamic ID. Returns the effective ID, or
// PatternSilence when validation fails — an unregistered ID resolves to nil in
// GetPattern and plays as silence, matching the rest of the degrade path.
// Callers that need the reason call ValidatePattern first; the TOML path
// already does, at load, with a specific error.
func RegisterPattern(p *Pattern) PatternID {
	if ValidatePattern(p) != nil {
		return PatternSilence
	}
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

// RegisteredPatterns snapshots the registry in ID order. Setup and editor path
// only — the mixer holds pattern pointers directly.
func RegisteredPatterns() []*Pattern {
	patternMu.RLock()
	defer patternMu.RUnlock()
	out := make([]*Pattern, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b *Pattern) int { return int(a.ID) - int(b.ID) })
	return out
}

// resetPatternRegistry clears the registry. fillIDs is written outside the
// mutex under collectFills' write-once contract: the caller guarantees no mixer.
func resetPatternRegistry() {
	patternMu.Lock()
	patterns = make(map[PatternID]*Pattern)
	patternName = make(map[string]PatternID)
	nextDynamic = PatternID(PatternDynamic)
	patternMu.Unlock()
	fillIDs = nil
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
