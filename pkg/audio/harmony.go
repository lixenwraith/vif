package audio

import "slices"

var scaleTable = [ScaleCount][]int{
	ScaleMinor:         {0, 2, 3, 5, 7, 8, 10},
	ScaleHarmonicMinor: {0, 2, 3, 5, 7, 8, 11},
	ScalePhrygian:      {0, 1, 3, 5, 7, 8, 10},
	ScaleDorian:        {0, 2, 3, 5, 7, 9, 10},
	ScaleMinorPent:     {0, 3, 5, 7, 10},
	ScaleMajor:         {0, 2, 4, 5, 7, 9, 11},
}

// ScaleIntervals returns a scale's semitone offsets from its root, those pattern
// degrees resolve through; nil for an unknown scale
func ScaleIntervals(s ScaleID) []int {
	if s < 0 || s >= ScaleCount {
		return nil
	}
	return slices.Clone(scaleTable[s])
}

// harmony is the mixer-confined tonal context: key, scale, chord progression
type harmony struct {
	root     int   // MIDI root
	scale    []int // semitone offsets
	prog     []int // chord root degrees, one per bar, looping
	chordIdx int
}

func newHarmony() *harmony {
	return &harmony{
		root:  DefaultRootNote,
		scale: scaleTable[ScalePhrygian],
		prog:  []int{0, 0, 5, 6}, // i i VI VII
	}
}

// set applies partial updates: root<=0 keeps, scale out of range keeps, nil prog keeps
func (h *harmony) set(root int, scale ScaleID, prog []int) {
	if root > 0 && root < 128 {
		h.root = root
	}
	if scale >= 0 && scale < ScaleCount {
		h.scale = scaleTable[scale]
	}
	if len(prog) > 0 {
		// Own the slice — caller retains its copy across the command channel
		h.prog = append(h.prog[:0:0], prog...)
	}
	h.chordIdx = 0
}

func (h *harmony) advanceBar() {
	h.chordIdx = (h.chordIdx + 1) % len(h.prog)
}

func (h *harmony) chordRoot() int { return h.prog[h.chordIdx] }

// resolve maps a scale degree + octave offset to a clamped MIDI note
// followChord offsets the degree by the current chord root, so degree sets
// {0,2,4} become chord tones and Deg 0 tracks the progression
func (h *harmony) resolve(deg, oct, acc int, followChord bool) int {
	if followChord {
		deg += h.chordRoot()
	}
	n := len(h.scale)
	octShift := deg / n
	idx := deg % n
	if idx < 0 {
		idx += n
		octShift--
	}
	midi := h.root + 12*(oct+octShift) + h.scale[idx] + acc
	if midi < 0 {
		midi = 0
	} else if midi > 127 {
		midi = 127
	}
	return midi
}

func (h *harmony) reset() { h.chordIdx = 0 }

// StepPitch is the MIDI note a step's degree, octave and accidental sound over a
// key with no chord to follow, as the harmony resolves them.
func StepPitch(root int, scale ScaleID, deg, oct, acc int) int {
	h := harmony{root: root, scale: intervals(scale)}
	return h.resolve(deg, oct, acc, false)
}

// PitchStep is the step of a key that sounds midi: the nearest degree and its
// octave, the lower on a tie, with the semitones left over as the accidental.
func PitchStep(root int, scale ScaleID, midi int) (deg, oct, acc int) {
	sc := intervals(scale)
	deg, oct = degreeIn(midi, root, sc)
	return deg, oct, midi - (root + 12*oct + sc[deg])
}

// intervals is a scale's table, the default's for one unknown
func intervals(scale ScaleID) []int {
	if scale < 0 || scale >= ScaleCount {
		return scaleTable[ScalePhrygian]
	}
	return scaleTable[scale]
}

// degreeIn is the degree and octave of the scale nearest midi over root, the
// lower on a tie, as harmony.resolve reads them back.
func degreeIn(midi, root int, scale []int) (deg, oct int) {
	semis := midi - root
	oct = semis / 12
	if semis < 0 && semis%12 != 0 {
		oct--
	}
	within := semis - 12*oct
	best, bestD := 0, 12
	for i, iv := range scale {
		if d := max(within-iv, iv-within); d < bestD {
			best, bestD = i, d
		}
	}
	if 12-within < bestD {
		return 0, oct + 1
	}
	return best, oct
}
