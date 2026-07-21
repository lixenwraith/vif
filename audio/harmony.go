package audio

var scaleTable = [ScaleCount][]int{
	ScaleMinor:         {0, 2, 3, 5, 7, 8, 10},
	ScaleHarmonicMinor: {0, 2, 3, 5, 7, 8, 11},
	ScalePhrygian:      {0, 1, 3, 5, 7, 8, 10},
	ScaleDorian:        {0, 2, 3, 5, 7, 9, 10},
	ScaleMinorPent:     {0, 3, 5, 7, 10},
	ScaleMajor:         {0, 2, 4, 5, 7, 9, 11},
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
		h.prog = prog
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
func (h *harmony) resolve(deg, oct int, followChord bool) int {
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
	midi := h.root + 12*(oct+octShift) + h.scale[idx]
	if midi < 0 {
		midi = 0
	} else if midi > 127 {
		midi = 127
	}
	return midi
}

func (h *harmony) reset() { h.chordIdx = 0 }
