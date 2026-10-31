package audio

import "math"

// fft is an in-place iterative radix-2 transform of re and im, whose length is a
// power of two.
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		ang := -2 * math.Pi / float64(size)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for start := 0; start < n; start += size {
			cr, ci := 1.0, 0.0
			for k := range half {
				a, b := start+k, start+k+half
				tr := re[b]*cr - im[b]*ci
				ti := re[b]*ci + im[b]*cr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
}

// spectrogram hands visit each frame's magnitude spectrum, n/2+1 bins, under a
// Hann window of n samples every hop; frames are centred, so frame t is the
// signal at sample t*hop and the first frame starts at zero.
func spectrogram(x []float64, n, hop int, visit func(frame int, mag []float64)) {
	window := make([]float64, n)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	re, im := make([]float64, n), make([]float64, n)
	mag := make([]float64, n/2+1)
	for t := 0; t*hop < len(x); t++ {
		start := t*hop - n/2
		for i := range n {
			if j := start + i; j >= 0 && j < len(x) {
				re[i] = x[j] * window[i]
			} else {
				re[i] = 0
			}
			im[i] = 0
		}
		fft(re, im)
		for k := range mag {
			mag[k] = math.Hypot(re[k], im[k])
		}
		visit(t, mag)
	}
}

// slidingMedian is the median of the last size values pushed, the values kept
// sorted so a push costs a shift of size elements.
type slidingMedian struct {
	ring   []float64
	sorted []float64
	pos, n int
}

func newSlidingMedian(size int) *slidingMedian {
	return &slidingMedian{ring: make([]float64, size), sorted: make([]float64, 0, size)}
}

// push adds v, dropping the oldest value once size are held, and returns the
// median of what is held, the upper middle of an even count.
func (m *slidingMedian) push(v float64) float64 {
	if m.n == len(m.ring) {
		old := m.ring[m.pos]
		i := lowerBound(m.sorted, old)
		copy(m.sorted[i:], m.sorted[i+1:])
		m.sorted = m.sorted[:len(m.sorted)-1]
	} else {
		m.n++
	}
	m.ring[m.pos] = v
	m.pos = (m.pos + 1) % len(m.ring)
	i := lowerBound(m.sorted, v)
	m.sorted = append(m.sorted, 0)
	copy(m.sorted[i+1:], m.sorted[i:])
	m.sorted[i] = v
	return m.sorted[len(m.sorted)/2]
}

func lowerBound(s []float64, v float64) int {
	lo, hi := 0, len(s)
	for lo < hi {
		mid := (lo + hi) / 2
		if s[mid] < v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// resample converts x from one rate to another by linear interpolation, through
// a windowed-sinc low-pass first when the rate falls, so the top of the band does
// not fold into it.
func resample(x []float64, from, to int) []float64 {
	if from == to {
		return x
	}
	if to < from {
		const taps = 31
		cutoff := 0.45 * float64(to) / float64(from) // of the source rate; 0.5 is its Nyquist
		h := make([]float64, taps)
		var sum float64
		for i := range h {
			t := float64(i - taps/2)
			w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(taps-1))
			if t == 0 {
				h[i] = 2 * cutoff * w
			} else {
				h[i] = math.Sin(2*math.Pi*cutoff*t) / (math.Pi * t) * w
			}
			sum += h[i]
		}
		y := make([]float64, len(x))
		for i := range x {
			var acc float64
			for k, c := range h {
				if j := i + k - taps/2; j >= 0 && j < len(x) {
					acc += c * x[j]
				}
			}
			y[i] = acc / sum
		}
		x = y
	}
	ratio := float64(from) / float64(to)
	n := int(float64(len(x)) / ratio)
	out := make([]float64, n)
	for i := range out {
		pos := float64(i) * ratio
		j := int(pos)
		frac := pos - float64(j)
		if j+1 < len(x) {
			out[i] = x[j]*(1-frac) + x[j+1]*frac
		} else if j < len(x) {
			out[i] = x[j]
		}
	}
	return out
}
