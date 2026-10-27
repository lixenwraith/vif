package vmath

import (
	"math"
	"math/bits"
)

// DegToRad converts degrees to radians
func DegToRad(deg float64) float64 {
	return deg * math.Pi / 180.0
}

// RadToDeg converts radians to degrees
func RadToDeg(rad float64) float64 {
	return rad * 180.0 / math.Pi
}

// Lerp performs linear interpolation between a and b
// t is in [0.0, 1.0] where 0.0 returns a, 1.0 returns b
func Lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

// Clamp restricts a value v between lo and hi
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Abs returns the absolute value
func Abs(x float64) float64 {
	return math.Abs(x)
}

// Sign returns -1.0, 0.0, or 1.0
func Sign(x float64) float64 {
	if x < 0 {
		return -1.0
	}
	if x > 0 {
		return 1.0
	}
	return 0.0
}

// A rate in MicroRate units moves a cell a second, and at that rate CellSpan of
// rate times nanoseconds is one cell: the integer phase a scroll or a drift carries
const (
	MicroRate int64 = 1_000_000
	CellSpan        = MicroRate * 1_000_000_000
)

// Ramp is a rate easing linearly from From to To over Span units of time, Done of
// them past, that holds To from there: a scroll's pace as it changes. Integer
// throughout, so every platform covers the same distance.
type Ramp struct {
	From int64 `json:"from,omitempty"`
	To   int64 `json:"to"`
	Done int64 `json:"done,omitempty"`
	Span int64 `json:"span,omitempty"`
}

// EaseTo is a ramp from the rate r stands at to rate to over span, at once for none
func (r Ramp) EaseTo(to, span int64) Ramp {
	if span <= 0 {
		return Ramp{To: to}
	}
	return Ramp{From: r.Rate(), To: to, Span: span}
}

// Rate is the rate where the ramp stands
func (r Ramp) Rate() int64 {
	if r.Done >= r.Span {
		return r.To
	}
	return r.From + mulDiv(r.To-r.From, r.Done, r.Span)
}

// Cover is the distance the next dt covers, rate times time, and the ramp after it,
// which keeps To alone once it reaches its end
func (r Ramp) Cover(dt int64) (int64, Ramp) {
	dt = max(dt, 0)
	ease := min(dt, r.Span-r.Done)
	var dist int64
	if ease > 0 {
		from := r.Rate()
		r.Done += ease
		dist = (from + r.Rate()) * ease / 2
	}
	if r.Done >= r.Span {
		r = Ramp{To: r.To}
	}
	return dist + r.To*(dt-ease), r
}

// mulDiv is a·b/c truncated toward zero for 0 <= b <= c, exact where a·b overflows
func mulDiv(a, b, c int64) int64 {
	ua := uint64(a)
	if a < 0 {
		ua = -ua
	}
	hi, lo := bits.Mul64(ua, uint64(b))
	q, _ := bits.Div64(hi, lo, uint64(c))
	if a < 0 {
		return -int64(q)
	}
	return int64(q)
}
