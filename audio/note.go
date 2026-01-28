package audio

// NoteFrequencies contains precomputed frequencies for MIDI notes 0-127
// A4 (note 69) = 440Hz, equal temperament
var NoteFrequencies [128]float64

func init() {
	for i := range NoteFrequencies {
		NoteFrequencies[i] = 440.0 * pow2((float64(i)-69.0)/12.0)
	}
}

// pow2 computes 2^x using Taylor series
func pow2(x float64) float64 {
	ln2 := 0.693147180559945
	y := x * ln2
	sum := 1.0
	term := 1.0
	for i := 1; i < 20; i++ {
		term *= y / float64(i)
		sum += term
	}
	return sum
}

// NoteFreq returns frequency in Hz for MIDI note number
func NoteFreq(midi int) float64 {
	if midi < 0 || midi >= 128 {
		return 0
	}
	return NoteFrequencies[midi]
}