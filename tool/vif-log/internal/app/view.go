package app

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/export"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/filter"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
)

// sortDir is the display order of the sort column.
type sortDir uint8

const (
	sortNone sortDir = iota
	sortAsc
	sortDesc
)

func (d sortDir) arrow() rune { return [...]rune{' ', '↑', '↓'}[d] }
func (d sortDir) String() string {
	return [...]string{"off", "asc", "desc"}[d]
}

// sortable reports whether a column's key lives in the index row. Sorting on
// fields would parse the whole view on every keystroke.
func sortable(c logfile.Column) bool {
	return c == logfile.ColTime || c == logfile.ColTick ||
		c == logfile.ColSub || c == logfile.ColMsg
}

// --- view and display order ------------------------------------------------

// sorted reports whether a current sorted order exists.
func (a *App) sorted() bool {
	return a.sortDir != sortNone && len(a.order) == len(a.view) && len(a.view) > 0
}

// rows returns the display order: the index-ordered view unless a sort is
// active and its result is current.
func (a *App) rows() []int32 {
	if a.sorted() {
		return a.order
	}
	return a.view
}

// rebuild restarts the filter pass, keeping the focused record on the same
// screen row so a filter change never scrolls the list.
func (a *App) rebuild() {
	anchor := a.cursorRec()
	row := min(max(a.cursor-a.scroll, 0), max(a.listH-1, 0))

	a.stack.Compile()
	a.view = a.view[:0]
	a.build = viewBuilder{busy: true}
	a.filterStep(firstPassBudget)
	if !a.build.busy {
		a.applySort() // the order is only meaningful over a complete pass
	}
	a.seek(anchor)

	a.scroll = a.cursor - row
	a.clamp()
}

// filterStep tests records until the deadline, appending survivors to the view.
func (a *App) filterStep(budget time.Duration) {
	metas := a.idx.Metas()
	n := len(metas)
	if a.build.next >= n {
		a.build.busy = false
		return
	}
	a.fctx.Bind(a.idx, a.frd)
	deadline := time.Now().Add(budget)
	i := a.build.next
	for i < n {
		end := min(i+budgetCheck, n)
		for ; i < end; i++ {
			a.fctx.Reset(i, metas[i])
			if a.stack.Match(&a.fctx) {
				a.view = append(a.view, int32(i))
			}
		}
		if time.Now().After(deadline) {
			break
		}
	}
	a.build.next = i
	a.build.busy = i < n
}

// applySort rebuilds the display order from index-resident keys.
func (a *App) applySort() {
	if a.sortDir == sortNone || !sortable(a.sortCol) {
		a.order = a.order[:0]
		return
	}
	a.order = append(a.order[:0], a.view...)
	metas := a.idx.Metas()
	key := a.sortKey()
	desc := a.sortDir == sortDesc
	slices.SortStableFunc(a.order, func(x, y int32) int {
		c := cmp.Compare(key(metas[x]), key(metas[y]))
		if desc {
			c = -c
		}
		if c != 0 {
			return c
		}
		return cmp.Compare(x, y) // ties keep chronological order
	})
}

func (a *App) sortKey() func(logfile.Meta) int64 {
	switch a.sortCol {
	case logfile.ColTick:
		return func(m logfile.Meta) int64 { return int64(m.Tick) }
	case logfile.ColSub:
		r := ranks(a.idx.Subs())
		return func(m logfile.Meta) int64 { return rankOf(r, int(m.Sub)) }
	case logfile.ColMsg:
		r := ranks(a.idx.Msgs())
		return func(m logfile.Meta) int64 { return rankOf(r, int(m.Msg)) }
	default:
		return func(m logfile.Meta) int64 { return m.TS }
	}
}

// ranks maps interned ids to alphabetical position so the sort compares ints.
func ranks(names []string) []int32 {
	ord := make([]int32, len(names))
	for i := range ord {
		ord[i] = int32(i)
	}
	slices.SortFunc(ord, func(x, y int32) int { return strings.Compare(names[x], names[y]) })
	out := make([]int32, len(names))
	for r, id := range ord {
		out[id] = int32(r)
	}
	return out
}

func rankOf(r []int32, id int) int64 {
	if id < 0 || id >= len(r) {
		return -1
	}
	return int64(r[id])
}

// cycleSort advances the sort on the focused column.
func (a *App) cycleSort() {
	if !sortable(a.col) {
		a.say(tui.ToastWarning, "sort: "+a.col.String()+" has no index key")
		return
	}
	if a.sortCol != a.col {
		a.sortCol, a.sortDir = a.col, sortNone
	}
	switch a.sortDir {
	case sortNone:
		a.sortDir = sortAsc
	case sortAsc:
		a.sortDir = sortDesc
	default:
		a.sortDir = sortNone
	}
	anchor := a.cursorRec()
	a.applySort()
	a.seek(anchor)
	a.clamp()
}

// --- cursor ----------------------------------------------------------------

func (a *App) cursorRec() int32 {
	rows := a.rows()
	if a.cursor < 0 || a.cursor >= len(rows) {
		return -1
	}
	return rows[a.cursor]
}

func (a *App) meta(rec int32) (logfile.Meta, bool) {
	metas := a.idx.Metas()
	if rec < 0 || int(rec) >= len(metas) {
		return logfile.Meta{}, false
	}
	return metas[rec], true
}

// indexOf locates rec in the display order. Unsorted, a miss yields the
// insertion point — the nearest following record; sorted, it yields -1.
func (a *App) indexOf(rec int32) (int, bool) {
	if a.sorted() {
		i := slices.Index(a.order, rec)
		return i, i >= 0
	}
	return slices.BinarySearch(a.view, rec)
}

// seek places the cursor on rec, falling back to its snapshot head when rec
// was filtered out — the head survives collapse.
func (a *App) seek(rec int32) {
	rows := a.rows()
	if rec < 0 || len(rows) == 0 {
		a.cursor = 0
		return
	}
	i, found := a.indexOf(rec)
	if !found {
		if m, ok := a.meta(rec); ok {
			if s, ok := a.idx.SnapshotOf(m); ok {
				if j, ok := a.indexOf(int32(s.Head)); ok {
					i, found = j, true
				}
			}
		}
	}
	if !found && i < 0 {
		i = a.cursor
	}
	a.cursor = min(max(i, 0), len(rows)-1)
}

func (a *App) clamp() {
	n := len(a.rows())
	if n == 0 {
		a.cursor, a.scroll = 0, 0
		return
	}
	a.cursor = tui.ClampCursor(a.cursor, n)
	a.scroll = tui.ClampScroll(a.scroll, a.listH, n)
	a.scroll = tui.AdjustScroll(a.cursor, a.scroll, a.listH, n)
}

func (a *App) move(d int) {
	a.cursor += d
	a.dscroll = 0
	a.clamp()
}

// --- follow ----------------------------------------------------------------

// followKey identifies records that look the same as the focused one: the
// interned (sub, msg) pair plus the first string field, which is what varies
// within a pair — ev for event dispatch, service for service records.
type followKey struct {
	sub uint16
	msg uint32
	val string
}

func (a *App) followKeyOf(rec int32) (followKey, bool) {
	m, ok := a.meta(rec)
	if !ok {
		return followKey{}, false
	}
	k := followKey{sub: m.Sub, msg: m.Msg}
	if line, err := a.rd.Line(m); err == nil {
		a.rec.Parse(m, line)
		k.val = a.rec.FollowValue()
	}
	return k, true
}

// followJump moves to the next record sharing the focused record's key. The
// index pre-check means only candidate lines are read.
func (a *App) followJump(dir int) {
	rows := a.rows()
	cur := a.cursorRec()
	if cur < 0 {
		return
	}
	k, ok := a.followKeyOf(cur)
	if !ok {
		return
	}
	metas := a.idx.Metas()
	for i, n := a.cursor+dir, 0; i >= 0 && i < len(rows) && n < followScanCap; i, n = i+dir, n+1 {
		m := metas[rows[i]]
		if m.Sub != k.sub || m.Msg != k.msg {
			continue
		}
		if k.val != "" {
			line, err := a.rd.Line(m)
			if err != nil {
				continue
			}
			a.rec.Parse(m, line)
			if a.rec.FollowValue() != k.val {
				continue
			}
		}
		a.cursor = i
		a.dscroll = 0
		a.clamp()
		return
	}
	a.say(tui.ToastWarning, "no more "+a.followLabel(k))
}

func (a *App) followLabel(k followKey) string {
	s := logfile.Dash(a.idx.SubName(k.sub)) + "/" + logfile.Dash(a.idx.MsgName(k.msg))
	if k.val != "" {
		s += " " + k.val
	}
	return s
}

// --- snapshot, pins, column ------------------------------------------------

// toggleSnapshot expands or collapses the group under the cursor, anchoring on
// the head so the surrounding rows stay put.
func (a *App) toggleSnapshot() {
	m, ok := a.meta(a.cursorRec())
	if !ok || m.Snap == 0 {
		a.say(tui.ToastInfo, "not a stat snapshot")
		return
	}
	if s, ok := a.idx.SnapshotOf(m); ok {
		if i, found := a.indexOf(int32(s.Head)); found {
			a.cursor = i
		}
	}
	a.snap.ToggleGroup(m.Snap)
	a.rebuild()
}

func (a *App) togglePin() {
	rec := a.cursorRec()
	if rec < 0 {
		return
	}
	a.pins.Toggle(rec)
	if a.pinOnly.On {
		a.rebuild()
		return
	}
	a.move(1) // pinning a run should not need two keys per record
}

func (a *App) togglePinOnly() {
	if !a.pinOnly.On && a.pins.Len() == 0 {
		a.say(tui.ToastWarning, "no pinned records")
		return
	}
	a.pinOnly.On = !a.pinOnly.On
	a.rebuild()
}

// cycleDomain advances the three-state journal domain filter. Off a journal
// the filter has nothing to bite on, so say so rather than emptying the view.
func (a *App) cycleDomain() {
	if !a.idx.HasDomains() {
		a.say(tui.ToastInfo, "no journal records loaded")
		return
	}
	a.dom.Cycle()
	a.rebuild()
	if a.dom.Active() {
		a.say(tui.ToastInfo, "domain: "+a.dom.State.String())
	} else {
		a.say(tui.ToastInfo, "domain: both")
	}
}

func (a *App) clearPins() {
	n := a.pins.Len()
	a.pins.Clear()
	a.pinOnly.On = false
	a.rebuild()
	a.say(tui.ToastInfo, fmt.Sprintf("cleared %d pin(s)", n))
}

// cycleColumn moves the focus, re-running an active search in the new scope.
func (a *App) cycleColumn(d int) {
	a.col = a.col.Next(d)
	if a.find.Active() {
		_ = a.find.Set(a.find.Query, a.col)
		a.rebuild()
	}
}

// nextSnapshot jumps the cursor to the nearest landmark below: a stat snapshot
// head in a diagnostic log, a journal anchor in a capture.
func (a *App) nextSnapshot() { a.landmarkJump(1) }

// prevSnapshot jumps the cursor to the nearest landmark above.
func (a *App) prevSnapshot() { a.landmarkJump(-1) }

func (a *App) landmarkJump(dir int) {
	rows := a.rows()
	if len(rows) == 0 {
		return
	}
	metas := a.idx.Metas()
	for i := a.cursor + dir; i >= 0 && i < len(rows); i += dir {
		rec := rows[i]
		if int(rec) < len(metas) && metas[rec].Landmark() {
			a.cursor = i
			a.dscroll = 0
			a.clamp()
			return
		}
	}
	where := "below"
	if dir < 0 {
		where = "above"
	}
	a.say(tui.ToastWarning, "no more landmarks "+where)
}

// --- prompt: search and export ---------------------------------------------

type promptKind uint8

const (
	prFind promptKind = iota
	prFilter
	prExport
)

// prefix labels the prompt line and identifies the pending action.
func (k promptKind) prefix(col logfile.Column) string {
	switch k {
	case prExport:
		return "export to: "
	case prFilter:
		return "filter: "
	}
	return "/" + col.String() + " "
}

func (a *App) openPrompt(k promptKind, initial string) {
	a.promptKind = k
	a.prompt = tui.NewTextFieldState(initial)
	a.overlay = ovPrompt
}

func (a *App) openSearch() { a.openPrompt(prFind, a.find.Query) }

func (a *App) openExport() {
	if a.idx == nil {
		return
	}
	a.openPrompt(prExport, defaultExportName(a.title))
}

func (a *App) openFilter() { a.openPrompt(prFilter, "") }

func (a *App) commitPrompt() {
	a.overlay = ovNone
	switch a.promptKind {
	case prFind:
		if err := a.find.Set(a.prompt.Value(), a.col); err != nil {
			a.say(tui.ToastError, err.Error())
			return
		}
		a.rebuild()
	case prFilter:
		if err := a.applyFilterSpec(strings.TrimSpace(a.prompt.Value())); err != nil {
			a.say(tui.ToastError, err.Error())
			return
		}
		a.rebuild()
	case prExport:
		a.runExport(strings.TrimSpace(a.prompt.Value()))
	}
}

// clearState drops the active search and any dynamically added stack filters,
// leaving core persistent filters (level, snap, pin) intact.
func (a *App) clearState() {
	changed := false
	if a.find.Active() {
		_ = a.find.Set("", a.col)
		changed = true
	}

	var keep []filter.Entry
	for _, e := range a.stack.Entries {
		switch e.F.Kind() {
		case "level", "snap", "pin", "find", "dom":
			keep = append(keep, e)
		default:
			changed = true
		}
	}

	if changed {
		a.stack.Entries = keep
		a.rebuild()
	}
}

// exportSet returns the records to export: the pin buffer when it holds
// anything, otherwise the current result.
func (a *App) exportSet() ([]logfile.Meta, string) {
	src, what := a.rows(), "filtered"
	if a.pins.Len() > 0 {
		src, what = a.pins.Sorted(), "pinned"
	}
	metas := a.idx.Metas()
	out := make([]logfile.Meta, 0, len(src))
	for _, i := range src {
		if int(i) < len(metas) {
			out = append(out, metas[i])
		}
	}
	return out, what
}

func (a *App) runExport(path string) {
	if path == "" || a.idx == nil {
		return
	}
	if filepath.Ext(path) == "" {
		path += export.JSONL{}.Ext()
	}
	set, what := a.exportSet()
	if len(set) == 0 {
		a.say(tui.ToastWarning, "nothing to export")
		return
	}
	n, err := export.ToFile(path, export.JSONL{}, a.rd, set)
	if err != nil {
		a.say(tui.ToastError, err.Error())
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	a.say(tui.ToastSuccess, fmt.Sprintf("%d %s → %s", n, what, abs))
}

// defaultExportName is timestamped: exports are exclusive-create, so a fixed
// name would collide on the second export.
func defaultExportName(src string) string {
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	if base == "" || base == "." {
		base = "vif-log"
	}
	return base + "-" + time.Now().Format("150405") + ".jsonl"
}
