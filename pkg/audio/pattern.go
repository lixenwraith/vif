package audio

import (
	"slices"
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

// Pattern is the unified rhythm/melody pattern. Role, Groups and Tiers place it in
// the drawn arrangement: in its role's slot, in every group listed (all when none),
// at every tier set. A pattern without a role or a tier plays only when placed.
type Pattern struct {
	ID     PatternID
	Name   string
	Desc   string
	Steps  int
	Tracks []Track
	Role   Role
	Groups []string
	Tiers  uint8 // bit per Intensity
}

// Role is the slot a drawn pattern plays in
type Role uint8

const (
	RoleNone Role = iota
	RoleRhythm
	RoleMelody
	RoleFill
	roleCount
)

var roleNames = [roleCount]string{"", "rhythm", "melody", "fill"}

func (r Role) String() string {
	if r < roleCount {
		return roleNames[r]
	}
	return "invalid"
}

// slot is the sequencer slot a role plays in: rhythm 0, melody 1, fill 2
func (r Role) slot() int { return int(r) - 1 }

// Clone deep-copies a pattern. Mandatory before editing anything obtained from
// RegisteredPatterns or GetPattern: the mixer reads those structs directly from
// its goroutine, so in-place mutation is a data race. Register the clone; the
// swap is a pointer store under the registry lock.
func (p *Pattern) Clone() *Pattern {
	if p == nil {
		return nil
	}
	c := *p
	c.Groups = slices.Clone(p.Groups)
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

// arrangement is every group's pools by tier and role, built from the registry at
// Start and immutable afterward. A pattern changed or defined later keeps the place
// it had then, or has none.
type arrangement struct {
	groups []string
	pools  [][IntensityCount][roleCount - 1][]PatternID
}

// buildArrangement places every registered pattern. Groups keep the order their
// first member was registered in; a bank naming none is one unnamed group.
func buildArrangement() arrangement {
	pats := RegisteredPatterns()
	var a arrangement
	for _, p := range pats {
		for _, g := range p.Groups {
			if !slices.Contains(a.groups, g) {
				a.groups = append(a.groups, g)
			}
		}
	}
	if len(a.groups) == 0 {
		a.groups = []string{""}
	}
	a.pools = make([][IntensityCount][roleCount - 1][]PatternID, len(a.groups))
	for _, p := range pats {
		if p.Role == RoleNone {
			continue
		}
		for gi, g := range a.groups {
			if len(p.Groups) > 0 && !slices.Contains(p.Groups, g) {
				continue
			}
			for t := range IntensityCount {
				if p.Tiers&(1<<t) != 0 {
					a.pools[gi][t][p.Role.slot()] = append(a.pools[gi][t][p.Role.slot()], p.ID)
				}
			}
		}
	}
	return a
}

// pool is one group's members for a tier and role; nil outside the arrangement
func (a *arrangement) pool(group int, t Intensity, r Role) []PatternID {
	if group < 0 || group >= len(a.pools) || t < 0 || t >= IntensityCount || r == RoleNone {
		return nil
	}
	return a.pools[group][t][r.slot()]
}

// covers reports whether a group can draw both slots at a tier
func (a *arrangement) covers(group int, t Intensity) bool {
	return len(a.pool(group, t, RoleRhythm)) > 0 && len(a.pool(group, t, RoleMelody)) > 0
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

// resetPatternRegistry clears the registry; the caller guarantees no mixer.
func resetPatternRegistry() {
	patternMu.Lock()
	patterns = make(map[PatternID]*Pattern)
	patternName = make(map[string]PatternID)
	nextDynamic = PatternID(PatternDynamic)
	patternMu.Unlock()
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
