package audio

import (
	"math/rand/v2"
)

// motifBank: scale-degree seeds; rhythm derives from Euclidean onsets
// Transform mapping — displacement: mask rotation; inversion: bars 4–5;
// retrograde: bar 6; thinning: bar 7 (pre-fill)
var motifBank = [4][4]int{
	{0, 2, 4, 2},
	{0, -1, 0, 2},
	{4, 2, 1, 0},
	{0, 3, 2, 4},
}

// melodyGen rewrites the registered PatternMelodyGen lead track per bar
// Mixer-goroutine confined after Start; Events alias a fixed backing array
type melodyGen struct {
	pat     *Pattern
	leadBuf [32]Step
	motif   [4]int
	lastDeg int
}

// registerMelodyGen installs the generative pattern (called from InitDefaultPatterns)
func registerMelodyGen() {
	RegisterPattern(&Pattern{
		ID: PatternMelodyGen, Name: "melody_gen", Steps: 16,
		Tracks: []Track{
			{Instr: InstrBass, FollowChord: true, Humanize: 0.2, Events: rollingBass()},
			{Instr: InstrPiano, FollowChord: true, Humanize: 0.5, Events: nil},
		},
	})
}

func newMelodyGen() *melodyGen {
	return &melodyGen{pat: GetPattern(PatternMelodyGen)}
}

// regenerate produces the lead line for the given bar within the phrase
// Chord-tone targeting on strong beats (0, 8) via FollowChord degree sets;
// constrained interval-weighted walk fills between motif statements
// Chord offset applies at resolve time via the track's FollowChord flag
func (g *melodyGen) regenerate(phraseBar int, rng *rand.Rand) {
	if g.pat == nil || len(g.pat.Tracks) < 2 {
		return
	}
	if phraseBar == 0 {
		g.motif = motifBank[rng.IntN(len(motifBank))]
		g.lastDeg = g.motif[0]
	}

	k := 5 + rng.IntN(3) // onset density 5–7
	if phraseBar == PhraseBars-1 {
		k = 3 // thin out under the fill bar
	}

	// TODO: test difference
	// mask := EuclidMask(k, 16, rng.IntN(4))

	// fixed rotation with anchored strong beats. A fresh rng.IntN(4)
	// rotation every bar moved the onset grid under the chord-tone targeting
	// at pos 0 and 8, which then rarely had an onset to land on
	mask := EuclidMask(k, 16, 0) | 1<<0 | 1<<8

	lead := g.leadBuf[:0]
	mi := 0
	for pos := 0; pos < 16; pos++ {
		if mask&(1<<uint(pos)) == 0 {
			continue
		}
		var deg int
		if pos == 0 || pos == 8 {
			deg = nearestChordTone(g.lastDeg)
		} else {
			d := g.motif[mi%4]
			switch {
			case phraseBar == 6: // retrograde response tail
				d = g.motif[3-(mi%4)]
			case phraseBar >= PhraseBars/2: // inversion about motif root
				d = g.motif[0]*2 - d
			}
			if rng.Float64() < 0.3 {
				d = g.lastDeg + walkInterval(rng)
			}
			deg = clampDeg(d)
			mi++
		}
		lead = append(lead, Step{Pos: pos, Vel: 0.5 + 0.15*rng.Float64(), Deg: deg, Oct: 2, Dur: 1})
		g.lastDeg = deg
	}
	g.pat.Tracks[1].Events = lead
}

func nearestChordTone(deg int) int {
	best, bd := 0, 1<<30
	for _, ct := range [3]int{0, 2, 4} {
		d := deg - ct
		if d < 0 {
			d = -d
		}
		if d < bd {
			best, bd = ct, d
		}
	}
	return best
}

// walkInterval: steps favored over leaps
func walkInterval(rng *rand.Rand) int {
	r := rng.Float64()
	switch {
	case r < 0.35:
		return 1
	case r < 0.70:
		return -1
	case r < 0.85:
		return 2
	case r < 0.95:
		return -2
	default:
		return 3
	}
}

func clampDeg(d int) int {
	if d < -3 {
		return -3
	}
	if d > 9 {
		return 9
	}
	return d
}
