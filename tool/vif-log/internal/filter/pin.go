package filter

import "slices"

// PinSet is the pin buffer: record indices the user marked. It is owned by the
// app and outlives every filter change, which is the point — pins are
// assembled across successive filters. Not registered: it has no meaningful
// construction from a string argument.
type PinSet struct{ m map[int32]struct{} }

// NewPinSet returns an empty pin buffer.
func NewPinSet() *PinSet { return &PinSet{m: make(map[int32]struct{})} }

// Has reports whether record i is pinned.
func (p *PinSet) Has(i int32) bool { _, ok := p.m[i]; return ok }

// Toggle flips record i, returning its new state.
func (p *PinSet) Toggle(i int32) bool {
	if _, ok := p.m[i]; ok {
		delete(p.m, i)
		return false
	}
	p.m[i] = struct{}{}
	return true
}

// Clear empties the buffer.
func (p *PinSet) Clear() { clear(p.m) }

// Len returns the pin count.
func (p *PinSet) Len() int { return len(p.m) }

// Sorted returns the pinned record indices in file order.
func (p *PinSet) Sorted() []int32 {
	out := make([]int32, 0, len(p.m))
	for i := range p.m {
		out = append(out, i)
	}
	slices.Sort(out)
	return out
}

// Pinned restricts the view to the pin buffer.
type Pinned struct {
	Set *PinSet
	On  bool
}

// NewPinned wraps a pin buffer as a filter.
func NewPinned(s *PinSet) *Pinned { return &Pinned{Set: s} }

func (f *Pinned) Kind() string { return "pin" }
func (f *Pinned) Needs() Need  { return 0 }

func (f *Pinned) Match(c *Ctx) bool {
	return !f.On || f.Set.Has(int32(c.I))
}

func (f *Pinned) Label() string {
	if !f.On {
		return ""
	}
	return "pinned-only"
}
