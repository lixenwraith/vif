package ui

import "github.com/lixenwraith/terminal/tui"

// Pane identifies a region of the frame.
type Pane uint8

const (
	PaneNone Pane = iota
	PaneHeader
	PaneList
	PaneDetail
	PaneStatus
	PaneFooter
)

// Dir is the split axis of a container node.
type Dir uint8

const (
	Vertical   Dir = iota // children stacked top to bottom
	Horizontal            // children placed left to right
)

// Node is either a leaf bound to a Pane or a container. Layout is data:
// changing pane count or arrangement edits this tree only.
type Node struct {
	Pane     Pane
	Dir      Dir
	Fixed    int // size along the parent axis, 0 = share by Weight
	Weight   float64
	Children []Node
}

// Layout is the root of the pane tree.
type Layout struct{ Root Node }

// DefaultLayout is the phase-1 layout: header, list|detail body, status, footer.
func DefaultLayout() Layout {
	return Layout{Root: Node{Dir: Vertical, Children: []Node{
		{Pane: PaneHeader, Fixed: 1},
		{Dir: Horizontal, Weight: 1, Children: []Node{
			{Pane: PaneList, Weight: 0.62},
			{Pane: PaneDetail, Weight: 0.38},
		}},
		{Pane: PaneStatus, Fixed: 1},
		{Pane: PaneFooter, Fixed: 1},
	}}}
}

// Resolve assigns a region to every leaf pane.
func (l Layout) Resolve(r tui.Region) map[Pane]tui.Region {
	out := make(map[Pane]tui.Region, 8)
	place(l.Root, r, out)
	return out
}

func place(n Node, r tui.Region, out map[Pane]tui.Region) {
	if len(n.Children) == 0 {
		if n.Pane != PaneNone {
			out[n.Pane] = r
		}
		return
	}
	total := r.H
	if n.Dir == Horizontal {
		total = r.W
	}
	sizes := share(n.Children, total)
	pos := 0
	for i, c := range n.Children {
		var sub tui.Region
		if n.Dir == Horizontal {
			sub = r.Sub(pos, 0, sizes[i], r.H)
		} else {
			sub = r.Sub(0, pos, r.W, sizes[i])
		}
		place(c, sub, out)
		pos += sizes[i]
	}
}

// share splits total between fixed and weighted children; the rounding
// remainder goes to the last weighted child.
func share(cs []Node, total int) []int {
	sizes := make([]int, len(cs))
	rest, sum := total, 0.0
	for i, c := range cs {
		if c.Fixed > 0 {
			s := min(c.Fixed, max(rest, 0))
			sizes[i], rest = s, rest-s
		} else {
			sum += c.Weight
		}
	}
	if sum <= 0 {
		sum = 1
	}
	lastW, acc := -1, 0
	for i, c := range cs {
		if c.Fixed > 0 {
			continue
		}
		s := int(float64(rest) * c.Weight / sum)
		sizes[i], acc, lastW = s, acc+s, i
	}
	if lastW >= 0 {
		sizes[lastW] += rest - acc
	}
	return sizes
}
