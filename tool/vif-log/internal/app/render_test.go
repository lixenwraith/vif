package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
)

// cells returns the columns' occupied extents, in placement order, so an
// overlap or a gutter that eats its neighbour shows up as a failing bound.
func extents(c colLayout) [][2]int {
	out := [][2]int{{c.pinX, 2}}
	if c.srcW > 0 {
		out = append(out, [2]int{c.srcX, c.srcW})
	}
	out = append(out, [2]int{c.tsX, c.tsW}, [2]int{c.lvlX, 1})
	if c.domW > 0 {
		out = append(out, [2]int{c.domX, c.domW})
	}
	if c.tickW > 0 {
		out = append(out, [2]int{c.tickX, c.tickW})
	}
	return append(out, [2]int{c.subX, c.subW}, [2]int{c.markX, 1},
		[2]int{c.msgX, c.msgW}, [2]int{c.fldX, 1})
}

func TestListColsNeverOverlap(t *testing.T) {
	for _, w := range []int{minW, 96, 120, 200} {
		for _, nsrc := range []int{1, 3} {
			for _, dom := range []bool{false, true} {
				c := listCols(w, nsrc, dom)
				prev := -1
				for _, e := range extents(c) {
					if e[0] < prev {
						t.Errorf("w=%d nsrc=%d dom=%v: column at %d overlaps the one ending at %d",
							w, nsrc, dom, e[0], prev)
					}
					prev = e[0] + e[1]
				}
				if c.fldX >= w {
					t.Errorf("w=%d nsrc=%d dom=%v: fields column starts past the pane",
						w, nsrc, dom)
				}
			}
		}
	}
}

func TestDomainGutterCostsNothingWithoutAJournal(t *testing.T) {
	// A diagnostic log must lay out exactly as it did before the journal
	// existed: the gutter is claimed only when a record can fill it.
	plain := listCols(120, 1, false)
	journal := listCols(120, 1, true)

	if plain.domW != 0 {
		t.Errorf("domW = %d without a journal, want 0", plain.domW)
	}
	if journal.domW != 1 {
		t.Errorf("domW = %d with a journal, want 1", journal.domW)
	}
	// Everything left of the gutter is untouched, so the eye finds time and
	// level in the same place in both files.
	if journal.tsX != plain.tsX || journal.lvlX != plain.lvlX {
		t.Error("the domain gutter moved a column to its left")
	}
	// Columns to its right shift by the gutter and its separator, and by
	// whatever the wider msg column takes for event names.
	if got, want := journal.subX-plain.subX, 2; got != want {
		t.Errorf("sub shifted by %d, want %d", got, want)
	}
	if journal.msgW <= plain.msgW {
		t.Errorf("journal msg column is %d wide, no wider than a log's %d",
			journal.msgW, plain.msgW)
	}
	if got, want := journal.fldX-plain.fldX, 2+journal.msgW-plain.msgW; got != want {
		t.Errorf("fields shifted by %d, want %d", got, want)
	}
}

func TestNarrowPaneKeepsTheJournalMsgColumnInBounds(t *testing.T) {
	// The journal's wider msg column must yield on a narrow pane like every
	// other column, or the fields column starts past the edge.
	for _, w := range []int{40, 59, 60, 75, 76, 95} {
		c := listCols(w, 1, true)
		if c.msgW > 24 {
			t.Errorf("w=%d: msgW = %d", w, c.msgW)
		}
		if w >= minW*62/100 && c.fldX >= w {
			t.Errorf("w=%d: fields column starts at %d, past the pane", w, c.fldX)
		}
	}
}

func TestSnapMarkDistinguishesAnchorsFromSnapshots(t *testing.T) {
	anchor := logfile.Meta{Flags: logfile.FlagAnchor}
	head := logfile.Meta{Flags: logfile.FlagSnapHead, Snap: 1}
	member := logfile.Meta{Snap: 1}

	if got := snapMark(anchor, true); got != '⚑' {
		t.Errorf("anchor mark = %q, want a flag", got)
	}
	if got := snapMark(head, true); got != '▶' {
		t.Errorf("collapsed head mark = %q", got)
	}
	if got := snapMark(head, false); got != '▼' {
		t.Errorf("expanded head mark = %q", got)
	}
	if got := snapMark(member, false); got != '·' {
		t.Errorf("member mark = %q", got)
	}
	if got := snapMark(logfile.Meta{}, false); got != 0 {
		t.Errorf("plain record mark = %q, want none", got)
	}
}
