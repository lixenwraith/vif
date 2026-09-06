package render

// Rect is a half-open rectangle of terminal cells: X0/Y0 are inclusive, X1/Y1
// exclusive. It carries no coordinate space of its own — the helper that
// produced it says whether the values are viewport or screen cells — and its
// zero value is empty, which is the identity every clip degrades to when two
// rectangles do not meet.
type Rect struct {
	X0, Y0, X1, Y1 int
}

// RectWH builds a rectangle from an origin and a size. A non-positive size
// yields an empty rectangle rather than an inverted one.
func RectWH(x, y, w, h int) Rect {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X0: x, Y0: y, X1: x + w, Y1: y + h}
}

// Empty reports whether the rectangle covers no cell
func (r Rect) Empty() bool { return r.X1 <= r.X0 || r.Y1 <= r.Y0 }

// Width returns the number of columns the rectangle covers
func (r Rect) Width() int { return max(r.X1-r.X0, 0) }

// Height returns the number of rows the rectangle covers
func (r Rect) Height() int { return max(r.Y1-r.Y0, 0) }

// Contains reports whether a coordinate falls inside the rectangle
func (r Rect) Contains(x, y int) bool {
	return x >= r.X0 && x < r.X1 && y >= r.Y0 && y < r.Y1
}

// Intersect returns the overlap of two rectangles, normalised to the zero Rect
// when they do not meet so a caller cannot end up with an inverted span
func (r Rect) Intersect(o Rect) Rect {
	out := Rect{
		X0: max(r.X0, o.X0),
		Y0: max(r.Y0, o.Y0),
		X1: min(r.X1, o.X1),
		Y1: min(r.Y1, o.Y1),
	}
	if out.Empty() {
		return Rect{}
	}
	return out
}

// Translate shifts the rectangle by an offset, which is how a viewport-space
// rectangle becomes a screen-space one. An empty rectangle stays empty.
func (r Rect) Translate(dx, dy int) Rect {
	if r.Empty() {
		return Rect{}
	}
	return Rect{X0: r.X0 + dx, Y0: r.Y0 + dy, X1: r.X1 + dx, Y1: r.Y1 + dy}
}
