package position

import (
	"math/bits"
	"slices"
)

// Bits is a grid of bits, one a cell, each row starting on a word: row y is the
// words [y*Stride, (y+1)*Stride) and cell x bit x&63 of word x>>6, so an area is a
// few words a row and a row shifts a column in a word. Bits past a row's width
// stay clear.
type Bits struct {
	Words  []uint64
	Stride int
}

// Stride is the words a row of w cells takes
func Stride(w int) int { return (w + 63) / 64 }

// NewBits makes a clear grid of w×h bits
func NewBits(w, h int) Bits { return Bits{Words: make([]uint64, Stride(w)*h), Stride: Stride(w)} }

// sized returns b at stride×h words, clear, keeping its storage where it has the room
func (b Bits) sized(stride, h int) Bits {
	if cap(b.Words) < stride*h {
		return Bits{Words: make([]uint64, stride*h), Stride: stride}
	}
	b.Words, b.Stride = b.Words[:stride*h], stride
	clear(b.Words)
	return b
}

// Clone returns a grid sharing no storage with b
func (b Bits) Clone() Bits { return Bits{Words: slices.Clone(b.Words), Stride: b.Stride} }

// Rows is how many rows the grid holds
func (b Bits) Rows() int {
	if b.Stride == 0 {
		return 0
	}
	return len(b.Words) / b.Stride
}

// Row is row y's words
func (b Bits) Row(y int) []uint64 { return b.Words[y*b.Stride : y*b.Stride+b.Stride] }

// Bit reports cell (x, y), which the caller has bounded
func (b Bits) Bit(x, y int) bool { return b.Words[y*b.Stride+x>>6]>>(uint(x)&63)&1 != 0 }

// Set sets cell (x, y), which the caller has bounded
func (b Bits) Set(x, y int) { b.Words[y*b.Stride+x>>6] |= 1 << (uint(x) & 63) }

// Clear clears cell (x, y), which the caller has bounded
func (b Bits) Clear(x, y int) { b.Words[y*b.Stride+x>>6] &^= 1 << (uint(x) & 63) }

// Put writes cell (x, y), which the caller has bounded
func (b Bits) Put(x, y int, on bool) {
	if on {
		b.Set(x, y)
	} else {
		b.Clear(x, y)
	}
}

// Any reports whether a bit is set in [x0, x1) × [y0, y1), which the caller has
// bounded by the grid and found non-empty
func (b Bits) Any(x0, x1, y0, y1 int) bool {
	w0, w1 := x0>>6, (x1-1)>>6
	first := ^uint64(0) << (uint(x0) & 63)
	last := ^uint64(0) >> (63 - uint(x1-1)&63)
	for y := y0; y < y1; y++ {
		row := b.Words[y*b.Stride:]
		if w0 == w1 {
			if row[w0]&first&last != 0 {
				return true
			}
			continue
		}
		if row[w0]&first != 0 || row[w1]&last != 0 {
			return true
		}
		for w := w0 + 1; w < w1; w++ {
			if row[w] != 0 {
				return true
			}
		}
	}
	return false
}

// Each calls fn with every set cell of row y, in order
func (b Bits) Each(y int, fn func(x int)) {
	for i, word := range b.Row(y) {
		for word != 0 {
			fn(i*64 + bits.TrailingZeros64(word))
			word &= word - 1
		}
	}
}

// ShiftRow writes src's row moved d cells left into dst, so dst's bit x is src's
// bit x+d, d in (-64, 64); cells moved in from past either end are clear. dst may
// be src for d > 0.
func ShiftRow(dst, src []uint64, d int) {
	switch {
	case d == 0:
		copy(dst, src)
	case d > 0:
		for i := range dst {
			dst[i] = src[i] >> uint(d)
			if i+1 < len(src) {
				dst[i] |= src[i+1] << (64 - uint(d))
			}
		}
	default:
		e := uint(-d)
		for i := range dst {
			dst[i] = src[i] << e
			if i > 0 {
				dst[i] |= src[i-1] >> (64 - e)
			}
		}
	}
}

// Diff calls fn with the new state of every cell of w×rows that differs between
// prev and next, two grids of one stride and size
func Diff(prev, next Bits, w int, fn func(x, y int, set bool)) {
	for y := range next.Rows() {
		p, n := prev.Row(y), next.Row(y)
		for i := range n {
			for turned := p[i] ^ n[i]; turned != 0; turned &= turned - 1 {
				x := i*64 + bits.TrailingZeros64(turned)
				if x < w {
					fn(x, y, n[i]>>(uint(x)&63)&1 != 0)
				}
			}
		}
	}
}
